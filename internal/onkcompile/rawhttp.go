package onkcompile

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const httpDecorator = "http"

var rawHTTPUnsupportedTargets = []string{targetPythonClient, "dart-client", "swift-client", "rust-client", "rust-server", "ts-server"}

func validateRawHTTPRPC(path string, rpc *onklang.RPCDecl) error {
	if !hasDecorator(rpc.Decorators, httpDecorator) {
		return nil
	}
	for _, name := range []string{"stream", wsDecorator, "body", successDecorator} {
		if hasDecorator(rpc.Decorators, name) {
			return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@http cannot be combined with @%s: the handler owns the whole request and response", name)}
		}
	}
	return nil
}

func validateRawHTTPContract(filePath string, method *onkir.Method, options CompileOptions) error {
	if !method.IsRawHTTP() {
		return nil
	}
	if options.generates(rawHTTPUnsupportedTargets...) {
		return &Error{Path: filePath, Msg: fmt.Sprintf("@http RPC %s is supported by the go-server, go-client, ts-client and openapi targets only; remove the python, dart, swift, rust and ts-server targets", method.Name)}
	}
	route, _ := method.Path()
	inPath := map[string]bool{}
	for _, name := range pathParameterNames(route) {
		inPath[name] = true
	}
	for _, field := range method.Request.Fields {
		if inPath[field.Name] || field.HasDecorator("query") {
			continue
		}
		return &Error{Path: filePath, Msg: fmt.Sprintf("field %q of @http RPC %s is not sent: an @http route takes its request from the path and @query fields, and the handler reads the body itself", field.Name, method.Name)}
	}
	return nil
}

func validateResponseDecorator(path string, rpc *onklang.RPCDecl, decorator onklang.Decorator) error {
	if decorator.Name == successDecorator {
		return validateSuccess(path, rpc, decorator)
	}
	if len(decorator.Args) > 1 {
		return &Error{Path: path, Line: rpc.Line, Msg: "@http takes at most one content type, as in @http(\"application/javascript\")"}
	}
	return nil
}
