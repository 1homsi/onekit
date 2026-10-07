package onkcompile

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func validateServiceDecl(filePath string, service *onklang.ServiceDecl, routeScope string, seenRoutes map[string]string, options CompileOptions) error {
	if err := validateDeclarationName(filePath, service.Line, service.Name, options); err != nil {
		return err
	}
	if err := validateHTTPPath(service.BasePath, true); err != nil {
		return &Error{Path: filePath, Line: service.Line, Msg: "invalid service base_path: " + err.Error()}
	}
	if strings.ContainsAny(service.BasePath, "{}") {
		return &Error{Path: filePath, Line: service.Line, Msg: "service base_path must not contain path parameters; declare them on each RPC route"}
	}
	if err := validateHeaders(filePath, service.Headers); err != nil {
		return err
	}
	seenMethods := map[string]string{}
	serviceHeaderNames := make(map[string]bool, len(service.Headers))
	for _, header := range service.Headers {
		serviceHeaderNames[strings.ToLower(header.Name)] = true
	}
	for _, rpc := range service.RPCs {
		if err := validateServiceRPC(filePath, rpc, service, routeScope, seenRoutes, seenMethods, serviceHeaderNames, options); err != nil {
			return err
		}
	}
	return nil
}

func validateServiceRPC(filePath string, rpc *onklang.RPCDecl, service *onklang.ServiceDecl, routeScope string, seenRoutes, seenMethods map[string]string, serviceHeaderNames map[string]bool, options CompileOptions) error {
	if !options.AllowLegacyContracts {
		if err := validateMemberName(filePath, rpc.Line, rpc.Name, options); err != nil {
			return err
		}
	}
	generated := generatedIdentifier(rpc.Name)
	if previous, exists := seenMethods[generated]; exists {
		return &Error{Path: filePath, Line: rpc.Line, Msg: fmt.Sprintf(
			"RPC name %q collides with %q after target-language name conversion", rpc.Name, previous,
		)}
	}
	seenMethods[generated] = rpc.Name
	verb, route, err := validateRPC(filePath, rpc, service.BasePath != "")
	if err != nil {
		return err
	}
	key := routeKey(verb, routeScope+route)
	if previous, exists := seenRoutes[key]; exists {
		return &Error{Path: filePath, Line: rpc.Line, Msg: fmt.Sprintf(
			"duplicate HTTP route %s (already declared by %s)", key, previous,
		)}
	}
	seenRoutes[key] = service.Name + "." + rpc.Name
	if err := validateHeaders(filePath, rpc.Headers); err != nil {
		return err
	}
	for _, header := range rpc.Headers {
		if serviceHeaderNames[strings.ToLower(header.Name)] {
			return &Error{Path: filePath, Line: header.Line, Msg: fmt.Sprintf(
				"RPC header %q conflicts with a service header", header.Name,
			)}
		}
	}
	return nil
}

func routeKey(verb, route string) string {
	if verb == wsTransport {
		verb = "GET"
	}
	return strings.ToUpper(verb) + " " + pathParameterPattern.ReplaceAllString(route, "{}")
}

func validateHeaders(path string, headers []onklang.HeaderDecl) error {
	seen := map[string]bool{}
	for _, header := range headers {
		key := strings.ToLower(header.Name)
		if key == "" {
			return &Error{Path: path, Line: header.Line, Msg: "header name must not be empty"}
		}
		if !validHTTPHeaderName(header.Name) {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("header name %q contains invalid HTTP token characters", header.Name)}
		}
		if seen[key] {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("duplicate header %q", header.Name)}
		}
		seen[key] = true
		if err := validateDecorators(path, header.Line, header.Decorators, headerDecorators); err != nil {
			return err
		}
		if kind, recognized := onkir.ParseScalarKind(header.Type); recognized && kind != onkir.ScalarString {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("header %q must use string type", header.Name)}
		}
		auth, hasAuth := findDecorator(header.Decorators, "auth")
		_, hasSchemeName := findDecorator(header.Decorators, "auth_scheme_name")
		if hasSchemeName && !hasAuth {
			return &Error{Path: path, Line: header.Line, Msg: "@auth_scheme_name requires @auth"}
		}
		if hasAuth && !hasDecorator(header.Decorators, "required") {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("@auth(%s) header must also be @required", auth.Args[0].Value)}
		}
		if hasAuth && (auth.Args[0].Value == "bearer" || auth.Args[0].Value == "basic") && !strings.EqualFold(header.Name, "Authorization") {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("@auth(%s) must be declared on the Authorization header; use @auth(api_key) for %q", auth.Args[0].Value, header.Name)}
		}
		if format, ok := findDecorator(header.Decorators, "format"); ok {
			value := format.Args[0].Value
			if value != "uuid" && value != "email" && value != "uri" {
				return &Error{Path: path, Line: header.Line, Msg: "header @format must be uuid, email, or uri"}
			}
		}
	}
	return nil
}

func validHTTPHeaderName(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return value != ""
}

func validateRPC(path string, rpc *onklang.RPCDecl, allowEmptyRoute bool) (string, string, error) {
	var verb, route string
	for _, decorator := range rpc.Decorators {
		if isHTTPVerb(decorator.Name) {
			if verb != "" {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "RPC must declare exactly one HTTP verb"}
			}
			if len(decorator.Args) != 1 || (decorator.Args[0].Value == "" && !allowEmptyRoute) {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@%s requires one non-empty route (an empty route is allowed only when the service sets base_path)", decorator.Name)}
			}
			verb, route = decorator.Name, decorator.Args[0].Value
			continue
		}
		switch decorator.Name {
		case wsDecorator:
			// Validation happens below once conflicting decorators are known;
			// record the route here.
			if len(decorator.Args) != 1 || decorator.Args[0].Value == "" {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws requires one non-empty route"}
			}
			route = decorator.Args[0].Value
		case "stream":
			if len(decorator.Args) != 0 {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@stream does not take arguments"}
			}
		case "body":
			if len(decorator.Args) > 1 {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@body accepts at most one argument"}
			}
		case "deprecated":
			if len(decorator.Args) > 1 {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@deprecated accepts at most one reason"}
			}
		case requiresDecorator, authorizeDecorator, metaDecorator:
			if err := validateAuthorization(path, rpc, decorator); err != nil {
				return "", "", err
			}
		case "max_body":
			if len(decorator.Args) != 1 {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@max_body takes one size such as 64MiB or a byte count"}
			}
			if _, ok := onkir.ParseByteSize(decorator.Args[0].Value); !ok {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@max_body size %q must be a positive byte count with an optional B, KiB, MiB or GiB suffix", decorator.Args[0].Value)}
			}
		default:
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("unknown RPC decorator @%s", decorator.Name)}
		}
	}
	if hasDecorator(rpc.Decorators, wsDecorator) {
		return validateWSRPC(path, rpc, verb, route)
	}
	if verb == "" {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "RPC must declare exactly one HTTP verb"}
	}
	if err := validateHTTPPath(route, allowEmptyRoute); err != nil {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "invalid RPC route: " + err.Error()}
	}
	if hasDecorator(rpc.Decorators, "max_body") && !isBodyBearingVerb(verb) {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@max_body requires a body-bearing HTTP verb"}
	}
	if body, ok := findDecorator(rpc.Decorators, "body"); ok {
		if !isBodyBearingVerb(verb) {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@body requires a body-bearing HTTP verb"}
		}
		if len(body.Args) != 1 || body.Args[0].Value == "" {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@body requires one non-empty request field name"}
		}
	}
	return verb, route, nil
}

// pathParameterPattern matches one `{name}` route parameter; hoisted so
// validateHTTPPath does not recompile it on every call.
var pathParameterPattern = regexp.MustCompile(`\{[^{}]+\}`)

func validateHTTPPath(value string, allowEmpty bool) error {
	if value == "" && allowEmpty {
		return nil
	}
	if !strings.HasPrefix(value, "/") {
		return errors.New("must start with /")
	}
	if strings.ContainsAny(value, "?#%") || strings.Contains(value, "//") {
		return errors.New("must not contain query strings, fragments, percent escapes, or empty segments")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			return errors.New("must not contain control, quote, or backslash characters")
		}
	}
	if open := strings.Count(value, "{"); open != strings.Count(value, "}") {
		return errors.New("contains unbalanced path parameter braces")
	}
	withoutParams := pathParameterPattern.ReplaceAllString(value, "x")
	if path.Clean(withoutParams) != withoutParams {
		return fmt.Errorf("must be a canonical literal URL path (got %s after substituting path parameters)", withoutParams)
	}
	for _, segment := range strings.Split(value, "/") {
		if strings.ContainsAny(segment, "{}") && !(strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && strings.Count(segment, "{") == 1) {
			return fmt.Errorf("path parameter must fill a whole segment (got %q)", segment)
		}
	}
	segments := strings.Split(value, "/")
	for i, segment := range segments {
		if strings.HasSuffix(segment, onkir.WildcardSuffix+"}") && i != len(segments)-1 {
			return fmt.Errorf("wildcard path parameter %q must be the last segment", segment)
		}
	}
	for _, name := range pathParameterNames(value) {
		if strings.ContainsAny(name, "{}") {
			return fmt.Errorf("path parameter %q must not contain nested braces", name)
		}
		if name == "" || generatedIdentifier(name) != strings.ToLower(strings.ReplaceAll(name, "_", "")) {
			return fmt.Errorf("contains invalid path parameter %q", name)
		}
	}
	return nil
}

func isHTTPVerb(name string) bool {
	switch name {
	case "get", postVerb, putVerb, "delete", patchVerb, queryVerb:
		return true
	default:
		return false
	}
}

func pathParameterNames(route string) []string {
	var names []string
	for start := strings.IndexByte(route, '{'); start >= 0; start = strings.IndexByte(route, '{') {
		route = route[start+1:]
		end := strings.IndexByte(route, '}')
		if end < 0 {
			break
		}
		names = append(names, strings.TrimSuffix(route[:end], onkir.WildcardSuffix))
		route = route[end+1:]
	}
	return names
}

func validateWSRPC(path string, rpc *onklang.RPCDecl, verb, route string) (string, string, error) {
	if verb != "" {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws cannot be combined with an HTTP verb; it replaces the transport binding"}
	}
	if hasDecorator(rpc.Decorators, "stream") {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws is already bidirectional and cannot be combined with @stream"}
	}
	if err := rejectAuthorizationOnWS(path, rpc); err != nil {
		return "", "", err
	}
	if hasDecorator(rpc.Decorators, "max_body") {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws does not support @max_body"}
	}
	if bodyName, ok := findDecorator(rpc.Decorators, "body"); ok {
		_ = bodyName
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws does not support @body binding; every non-path/non-query request field crosses as a message frame"}
	}
	if strings.Contains(route, onkir.WildcardSuffix+"}") {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws routes cannot use wildcard path parameters"}
	}
	if err := validateHTTPPath(route, false); err != nil {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "invalid @ws route: " + err.Error()}
	}
	return wsTransport, route, nil
}
