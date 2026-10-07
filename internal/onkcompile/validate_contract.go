package onkcompile

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

func validateContract(pkg *onkir.Package, options CompileOptions) error {
	bodyRequests := bodyRequestMessages(pkg.Files)
	pathUses := requestPathNames(pkg.Files)
	fullNames := map[string]string{}
	for _, file := range pkg.Files {
		for _, message := range file.Messages {
			if err := validateFlattenCycle(file.Path, message, nil); err != nil {
				return err
			}
			if err := validateFlattenKeys(file.Path, message); err != nil {
				return err
			}
			if err := validateCompiledMessage(file.Path, message, fullNames, options); err != nil {
				return err
			}
		}
		for _, enum := range file.Enums {
			if file.Package != "" {
				if previous, exists := fullNames[enum.FullName()]; exists {
					return &Error{Path: file.Path, Msg: fmt.Sprintf("qualified declaration %q conflicts with %s", enum.FullName(), previous)}
				}
				fullNames[enum.FullName()] = file.Path
			}
		}
		for _, service := range file.Services {
			for _, method := range service.Methods {
				if err := validateMethodBindings(file.Path, method, options, bodyRequests, pathUses); err != nil {
					return err
				}
				if err := validateRawHTTPContract(file.Path, method, options); err != nil {
					return err
				}
			}
		}
		if err := validateSecuritySchemes(file); err != nil {
			return err
		}
	}
	return nil
}

func validateSecuritySchemes(file *onkir.File) error {
	seen := map[string]string{}
	check := func(header *onkir.Header) error {
		authType, ok := header.AuthType()
		if !ok {
			return nil
		}
		signature := authType
		if authType == "api_key" {
			signature += ":" + strings.ToLower(header.Name)
		}
		name := header.SecuritySchemeName()
		if previous, exists := seen[name]; exists && previous != signature {
			return &Error{Path: file.Path, Msg: fmt.Sprintf("auth scheme %q is declared as both %s and %s; give one header a different @auth_scheme_name", name, previous, signature)}
		}
		seen[name] = signature
		return nil
	}
	for _, service := range file.Services {
		for _, header := range service.Headers {
			if err := check(header); err != nil {
				return err
			}
		}
		for _, method := range service.Methods {
			for _, header := range method.Headers {
				if err := check(header); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func flattenedJSONKeys(message *onkir.Message, prefix string, keys map[string]string, owner string) (string, string) {
	for _, field := range message.Fields {
		if decorator, ok := field.Decorator("flatten"); ok && field.Type != nil && field.Type.Kind == onkir.KindMessage {
			childPrefix, _ := decorator.NamedArg("prefix")
			if key, previous := flattenedJSONKeys(field.Type.Message, prefix+childPrefix, keys, owner+"."+field.Name); key != "" {
				return key, previous
			}
			continue
		}
		key := prefix + field.Name
		if previous, exists := keys[key]; exists {
			return key, previous + " and " + owner + "." + field.Name
		}
		keys[key] = owner + "." + field.Name
	}
	return "", ""
}

func validateFlattenKeys(filePath string, message *onkir.Message) error {
	if key, owners := flattenedJSONKeys(message, "", map[string]string{}, message.Name); key != "" {
		return &Error{Path: filePath, Msg: fmt.Sprintf("JSON key %q is produced by both %s after @flatten; give the flattened field a distinct prefix", key, owners)}
	}
	for _, nested := range message.Nested {
		if err := validateFlattenKeys(filePath, nested); err != nil {
			return err
		}
	}
	return nil
}

func validateFlattenCycle(filePath string, message *onkir.Message, path []*onkir.Message) error {
	for _, seen := range path {
		if seen == message {
			names := make([]string, 0, len(path)+1)
			for _, m := range path {
				names = append(names, m.Name)
			}
			names = append(names, message.Name)
			return &Error{Path: filePath, Msg: "@flatten cycle: " + strings.Join(names, " -> ")}
		}
	}
	path = append(path, message)
	for _, field := range message.Fields {
		if !field.HasDecorator("flatten") || field.Type == nil || field.Type.Kind != onkir.KindMessage {
			continue
		}
		if err := validateFlattenCycle(filePath, field.Type.Message, path); err != nil {
			return err
		}
	}
	return nil
}

func validateCompiledMessage(filePath string, message *onkir.Message, fullNames map[string]string, options CompileOptions) error {
	if message.File != nil && message.File.Package != "" {
		if previous, exists := fullNames[message.FullName()]; exists {
			return &Error{Path: filePath, Msg: fmt.Sprintf("qualified declaration %q conflicts with %s", message.FullName(), previous)}
		}
		fullNames[message.FullName()] = filePath
	}
	for _, field := range message.Fields {
		if err := validateCompiledField(filePath, field, options); err != nil {
			return err
		}
	}
	for _, nested := range message.Nested {
		if err := validateCompiledMessage(filePath, nested, fullNames, options); err != nil {
			return err
		}
	}
	for _, enum := range message.NestedEnums {
		if enum.File != nil && enum.File.Package != "" {
			if previous, exists := fullNames[enum.FullName()]; exists {
				return &Error{Path: filePath, Msg: fmt.Sprintf("qualified declaration %q conflicts with %s", enum.FullName(), previous)}
			}
			fullNames[enum.FullName()] = filePath
		}
	}
	return nil
}

func validateCompiledField(filePath string, field *onkir.Field, options CompileOptions) error {
	if !options.AllowLegacyContracts && field.HasDecorator("required") && !field.Optional && !field.Repeated && field.Type != nil && field.Type.Kind == onkir.KindEnum {
		return &Error{Path: filePath, Msg: fmt.Sprintf(
			"@required on enum field %s.%s needs the ? marker; a non-optional enum always holds its first value", field.Message.FullName(), field.Name,
		)}
	}
	if field.Type != nil && field.Type.Kind == onkir.KindMap && field.Type.MapValue != nil &&
		field.Type.MapValue.Kind == onkir.KindMessage && isRootUnwrappedMessage(field.Type.MapValue.Message) {
		return &Error{Path: filePath, Msg: fmt.Sprintf(
			"@unwrap is not supported on map value message %s; use @unwrap only on a top-level request/response message",
			field.Type.MapValue.Message.FullName(),
		)}
	}
	decorator, hasEncode := field.Decorator("encode")
	if hasEncode {
		value, _ := decorator.Value()
		valid := false
		if field.Type != nil {
			switch field.Type.Kind {
			case onkir.KindEnum:
				valid = value == Int64EncodingNumber
			case onkir.KindScalar:
				switch field.Type.Scalar {
				case onkir.ScalarInt64, onkir.ScalarUint64:
					valid = value == Int64EncodingNumber
				case onkir.ScalarBytes:
					valid = value == "hex" || value == "base64" || value == "base64_raw" || value == "base64url" || value == "base64url_raw"
				case onkir.ScalarTimestamp:
					valid = value == "unix_seconds" || value == "unix_millis" || value == "date"
				default:
					valid = false
				}
			}
		}
		if !valid {
			return &Error{Path: filePath, Msg: fmt.Sprintf("@encode(%s) is not supported on field %s.%s", value, field.Message.FullName(), field.Name)}
		}
	}
	return nil
}

func isRootUnwrappedMessage(message *onkir.Message) bool {
	return message != nil && len(message.Fields) == 1 && message.Fields[0].HasDecorator("unwrap")
}

func validateMethodBindings(filePath string, method *onkir.Method, options CompileOptions, bodyRequests map[*onkir.Message]bool, pathUses map[*onkir.Message]map[string]bool) error {
	route, ok := method.WebSocketPath()
	if !ok {
		route, _ = method.Path()
	}
	seenPath := map[string]bool{}
	for _, name := range pathParameterNames(route) {
		if seenPath[name] {
			return &Error{Path: filePath, Msg: fmt.Sprintf("duplicate path parameter %q on RPC %s", name, method.Name)}
		}
		seenPath[name] = true
		field := methodField(method.Request, name)
		if field == nil || field.Type == nil || field.Type.Kind != onkir.KindScalar || field.Repeated {
			return &Error{Path: filePath, Msg: fmt.Sprintf("path parameter %q on RPC %s requires one non-repeated scalar request field", name, method.Name)}
		}
		if field.Optional {
			return &Error{Path: filePath, Msg: fmt.Sprintf("path parameter %q on RPC %s cannot be optional", name, method.Name)}
		}
		if onkir.IsWildcardParam(route, name) && field.Type.Scalar != onkir.ScalarString {
			return &Error{Path: filePath, Msg: fmt.Sprintf("wildcard path parameter %q on RPC %s requires a string request field", name, method.Name)}
		}
		if !isHTTPParameterScalar(field.Type.Scalar) {
			return &Error{Path: filePath, Msg: fmt.Sprintf("path parameter %q on RPC %s supports string, bool, integer, and float scalar fields", name, method.Name)}
		}
	}
	verb, _ := method.Verb()
	seenQuery := map[string]string{}
	for _, field := range method.Request.Fields {
		decorator, ok := field.Decorator("query")
		if !ok {
			continue
		}
		name, _ := decorator.Value()
		if name == "" {
			name = field.Name
		}
		if isBodyBearingVerb(verb) && options.generates(queryOnBodyUnsupportedTargets...) {
			return &Error{Path: filePath, Msg: fmt.Sprintf("@query field %q on body-bearing RPC %s is supported by the go, ts and openapi targets only; remove the python, dart, swift and rust targets or move the field into the body", field.Name, method.Name)}
		}
		if seenPath[field.Name] {
			return &Error{Path: filePath, Msg: fmt.Sprintf("request field %q cannot be both a path and query binding", field.Name)}
		}
		if previous, exists := seenQuery[name]; exists {
			return &Error{Path: filePath, Msg: fmt.Sprintf("query parameter %q is bound by both %s and %s", name, previous, field.Name)}
		}
		seenQuery[name] = field.Name
	}
	if bodyName, ok := method.BodyField(); ok {
		field := methodField(method.Request, bodyName)
		if field == nil {
			return &Error{Path: filePath, Msg: fmt.Sprintf("@body references unknown request field %q on RPC %s", bodyName, method.Name)}
		}
		if seenPath[field.Name] {
			return &Error{Path: filePath, Msg: fmt.Sprintf("request field %q cannot be both a path and body binding", field.Name)}
		}
		if _, query := field.Decorator("query"); query {
			return &Error{Path: filePath, Msg: fmt.Sprintf("request field %q cannot be both a query and body binding", field.Name)}
		}
	}
	if err := validateUnboundFields(filePath, method, verb, seenPath, options, bodyRequests, pathUses); err != nil {
		return err
	}
	if method.IsWebSocket() {
		if err := validateWSCorrelation(filePath, method); err != nil {
			return err
		}
	}
	return nil
}

func validateUnboundFields(filePath string, method *onkir.Method, verb string, pathFields map[string]bool, options CompileOptions, bodyRequests map[*onkir.Message]bool, pathUses map[*onkir.Message]map[string]bool) error {
	if method.IsWebSocket() {
		return nil
	}
	bodyName, hasBody := method.BodyField()
	if isBodyBearingVerb(verb) && !hasBody {
		return nil
	}
	allFields := !isBodyBearingVerb(verb) && !bodyRequests[method.Request]
	for _, field := range method.Request.Fields {
		if pathFields[field.Name] || field.HasDecorator("query") || hasBody && field.Name == bodyName {
			continue
		}
		if !field.HasDecorator("required") && (options.AllowLegacyContracts || !allFields || pathUses[method.Request][field.Name]) {
			continue
		}
		kind := "field"
		if field.HasDecorator("required") {
			kind = "@required field"
		}
		return &Error{Path: filePath, Msg: fmt.Sprintf(
			"%s %q on RPC %s is never sent: %s requests carry only path, @query and @body fields; add @query (a scalar field), put it in the route as {%s}, or use a verb with a body",
			kind, field.Name, method.Name, strings.ToUpper(verb), field.Name,
		)}
	}
	return nil
}

func isBodyBearingVerb(verb string) bool {
	return verb == postVerb || verb == putVerb || verb == patchVerb || verb == queryVerb
}

func methodField(message *onkir.Message, name string) *onkir.Field {
	for _, field := range message.Fields {
		if field.Name == name {
			return field
		}
	}
	return nil
}
