package onkcompile

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onklang"
)

const (
	metaDecorator      = "meta"
	guardDecorator     = "guard"
	requiresDecorator  = "requires"
	authorizeDecorator = "authorize"
	principalDecorator = "principal"
)

func validateRequires(path string, rpc *onklang.RPCDecl, decorator onklang.Decorator) error {
	if len(decorator.Args) == 0 {
		return &Error{Path: path, Line: rpc.Line, Msg: "@requires needs at least one scope"}
	}
	count := 0
	for _, other := range rpc.Decorators {
		if other.Name == requiresDecorator {
			count++
		}
	}
	if count > 1 {
		return &Error{Path: path, Line: rpc.Line, Msg: "declare every scope in a single @requires(...)"}
	}
	seen := map[string]bool{}
	for _, arg := range decorator.Args {
		if !validScopeName(arg.Value) {
			return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("invalid @requires scope %q: use letters, digits, and : _ . - /", arg.Value)}
		}
		if seen[arg.Value] {
			return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("duplicate @requires scope %q", arg.Value)}
		}
		seen[arg.Value] = true
	}
	return nil
}

func validScopeName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(":_.-/", r):
		default:
			return false
		}
	}
	return true
}

func validateAuthorization(path string, rpc *onklang.RPCDecl, decorator onklang.Decorator) error {
	switch decorator.Name {
	case requiresDecorator:
		return validateRequires(path, rpc, decorator)
	case metaDecorator:
		return validateMeta(path, rpc, decorator)
	case guardDecorator:
		return validateGuard(path, rpc, decorator)
	}
	if len(decorator.Args) != 2 {
		return &Error{Path: path, Line: rpc.Line, Column: decorator.Col, Msg: "@authorize expects an expression and a message"}
	}
	return nil
}

func rejectAuthorizationOnWS(path string, rpc *onklang.RPCDecl) error {
	for _, name := range []string{requiresDecorator, authorizeDecorator, metaDecorator, guardDecorator} {
		if hasDecorator(rpc.Decorators, name) {
			return &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@%s is not supported on @ws methods yet; authorize the upgrade request in middleware", name)}
		}
	}
	return nil
}

const (
	maxMetaKeyBytes   = 64
	maxMetaValueBytes = 200
)

func validateMeta(path string, rpc *onklang.RPCDecl, decorator onklang.Decorator) error {
	line, col := decorator.Line, decorator.Col
	if line == 0 {
		line, col = rpc.Line, rpc.Col
	}
	fail := func(format string, args ...any) error {
		return &Error{Path: path, Line: line, Column: col, Code: "invalid_meta", Msg: fmt.Sprintf(format, args...)}
	}
	if len(decorator.Args) != 2 {
		return fail("@meta expects a key and a value, as in @meta(\"audit\", \"app.update\")")
	}
	key, value := decorator.Args[0].Value, decorator.Args[1].Value
	if !validMetaKey(key) {
		return fail("invalid @meta key %q: start with a lower-case letter and use lower-case letters, digits and . _ -", key)
	}
	if value == "" || len(value) > maxMetaValueBytes {
		return fail("@meta value for %q must be 1 to %d bytes", key, maxMetaValueBytes)
	}
	for _, other := range rpc.Decorators {
		if other.Name == metaDecorator && len(other.Args) == 2 && other.Args[0].Value == key && (other.Line != decorator.Line || other.Col != decorator.Col) {
			return fail("duplicate @meta key %q", key)
		}
	}
	return nil
}

func validMetaKey(key string) bool {
	if key == "" || len(key) > maxMetaKeyBytes || key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

const maxGuardBytes = 200

func validateGuard(path string, rpc *onklang.RPCDecl, decorator onklang.Decorator) error {
	line, col := decorator.Line, decorator.Col
	if line == 0 {
		line, col = rpc.Line, rpc.Col
	}
	fail := func(format string, args ...any) error {
		return &Error{Path: path, Line: line, Column: col, Code: "invalid_guard", Msg: fmt.Sprintf(format, args...)}
	}
	if len(decorator.Args) == 0 {
		return fail("@guard needs at least one pattern, as in @guard(\"object/level/:id\")")
	}
	route := rpcRoute(rpc)
	params := map[string]bool{}
	for _, name := range pathParameterNames(route) {
		params[name] = true
	}
	for _, arg := range decorator.Args {
		pattern := arg.Value
		if pattern == "" || len(pattern) > maxGuardBytes {
			return fail("@guard patterns must be 1 to %d bytes", maxGuardBytes)
		}
		for _, segment := range strings.Split(pattern, "/") {
			name, ok := strings.CutPrefix(segment, ":")
			if !ok {
				continue
			}
			if !params[name] {
				return fail("@guard placeholder :%s in %q is not a path parameter of the route", name, pattern)
			}
		}
	}
	return nil
}

func rpcRoute(rpc *onklang.RPCDecl) string {
	for _, decorator := range rpc.Decorators {
		if isHTTPVerb(decorator.Name) && len(decorator.Args) == 1 {
			return decorator.Args[0].Value
		}
	}
	return ""
}
