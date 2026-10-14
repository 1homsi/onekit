package onkcompile

import (
	"fmt"
	"slices"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const httpDecorator = "http"

var (
	rawRequestKinds  = []string{onkir.RawBodyNone, onkir.RawBodyJSON, onkir.RawBodyMultipart, onkir.RawBodyStream}
	rawResponseKinds = []string{onkir.RawBodyStream, onkir.RawBodyJSON, onkir.RawResponseRedirect, onkir.RawResponseWebSocket}
)

func validateRawHTTPRPC(path string, rpc *onklang.RPCDecl, verb string) error {
	if !hasDecorator(rpc.Decorators, httpDecorator) {
		if verb == anyVerb {
			return &Error{Path: path, Line: rpc.Line, Msg: "@any needs @http: only a hand-written handler can serve every method"}
		}
		return nil
	}
	for _, name := range []string{"stream", wsDecorator, successDecorator} {
		if hasDecorator(rpc.Decorators, name) {
			return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@http cannot be combined with @%s: the handler owns the whole request and response", name)}
		}
	}
	decorator, _ := findDecorator(rpc.Decorators, httpDecorator)
	_, hasBody := findDecorator(rpc.Decorators, "body")
	request := onkir.RawBodyNone
	if isBodyBearingVerb(verb) {
		request = onkir.RawBodyStream
	}
	for _, arg := range decorator.Args {
		switch arg.Name {
		case "":
		case "request":
			if !slices.Contains(rawRequestKinds, arg.Value) {
				return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@http request must be one of %v, got %q", rawRequestKinds, arg.Value)}
			}
			request = arg.Value
		case "response":
			if !slices.Contains(rawResponseKinds, arg.Value) {
				return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@http response must be one of %v, got %q", rawResponseKinds, arg.Value)}
			}
		default:
			return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@http has no argument %q; use a content type, request: or response:", arg.Name)}
		}
	}
	if request != onkir.RawBodyNone && !isBodyBearingVerb(verb) {
		return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@http(request: %q) needs a verb with a body (@post, @put, @patch, @query or @any)", request)}
	}
	if request == onkir.RawBodyJSON && !hasBody {
		return &Error{Path: path, Line: rpc.Line, Msg: `@http(request: "json") needs @body("field") naming the request field that holds the JSON body`}
	}
	if request != onkir.RawBodyJSON && hasBody {
		return &Error{Path: path, Line: rpc.Line, Msg: `@body on an @http route needs @http(request: "json"); otherwise the handler reads the body itself`}
	}
	return nil
}

func validateRawHTTPContract(filePath string, method *onkir.Method) error {
	if !method.IsRawHTTP() {
		return nil
	}
	route, _ := method.Path()
	inPath := map[string]bool{}
	for _, name := range pathParameterNames(route) {
		inPath[name] = true
	}
	bodyName, _ := method.BodyField()
	for _, field := range method.Request.Fields {
		if inPath[field.Name] || field.HasDecorator("query") || field.Name == bodyName {
			continue
		}
		return &Error{Path: filePath, Msg: fmt.Sprintf("field %q of @http RPC %s is not sent: an @http route takes its request from the path, @query fields and a declared JSON @body, and the handler reads any other body itself", field.Name, method.Name)}
	}
	if method.RawResponseKind() != onkir.RawBodyJSON && len(method.Response.Fields) > 0 {
		return &Error{Path: filePath, Msg: fmt.Sprintf("response %s of @http RPC %s declares fields, but the handler writes the response itself; use an empty message, or @http(response: \"json\") for a typed JSON response", method.Response.Name, method.Name)}
	}
	return nil
}

func validateResponseDecorator(path string, rpc *onklang.RPCDecl, decorator onklang.Decorator) error {
	if decorator.Name == successDecorator {
		return validateSuccess(path, rpc, decorator)
	}
	positional := 0
	for _, arg := range decorator.Args {
		if arg.Name == "" {
			positional++
		}
	}
	if positional > 1 {
		return &Error{Path: path, Line: rpc.Line, Msg: "@http takes at most one content type, as in @http(\"application/javascript\")"}
	}
	return nil
}
