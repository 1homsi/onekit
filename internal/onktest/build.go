package onktest

import (
	"strings"

	"github.com/1homsi/onekit/internal/onkschema"
)

// The builders below assemble schema documents directly. Tests use them for
// declarations no TypeSpec source can produce, such as a decorator argument
// of the wrong kind, which the compiler must still reject because any
// frontend's document may carry it.

// File declares a package with messages, enums and services.
func File(pkg string, decls ...any) *onkschema.File {
	file := &onkschema.File{Package: pkg}
	for _, d := range decls {
		switch d := d.(type) {
		case *onkschema.MessageDecl:
			file.Messages = append(file.Messages, d)
		case *onkschema.EnumDecl:
			file.Enums = append(file.Enums, d)
		case *onkschema.ServiceDecl:
			file.Services = append(file.Services, d)
		default:
			panic("onktest.File: unexpected declaration")
		}
	}
	return file
}

// Message declares a message from fields, nested messages and enums, and
// message decorators.
func Message(name string, members ...any) *onkschema.MessageDecl {
	m := &onkschema.MessageDecl{Name: name}
	for _, member := range members {
		switch member := member.(type) {
		case *onkschema.FieldDecl:
			m.Fields = append(m.Fields, member)
		case *onkschema.MessageDecl:
			m.Nested = append(m.Nested, member)
		case *onkschema.EnumDecl:
			m.NestedEn = append(m.NestedEn, member)
		case onkschema.Decorator:
			m.Decorators = append(m.Decorators, member)
		default:
			panic("onktest.Message: unexpected member")
		}
	}
	return m
}

// Field declares a field. The type is a scalar or a type name, with a
// trailing "[]" for repeated and "?" for optional, or "map<K, V>".
func Field(name, typ string, decorators ...onkschema.Decorator) *onkschema.FieldDecl {
	f := &onkschema.FieldDecl{Name: name, Decorators: decorators}
	if strings.HasSuffix(typ, "?") {
		f.Optional = true
		typ = strings.TrimSuffix(typ, "?")
	}
	if strings.HasSuffix(typ, "[]") {
		f.Repeated = true
		typ = strings.TrimSuffix(typ, "[]")
	}
	f.Type = Type(typ)
	return f
}

// Type is a type reference: a name, or "map<K, V>".
func Type(typ string) *onkschema.TypeRef {
	if inner, ok := strings.CutPrefix(typ, "map<"); ok {
		key, value, _ := strings.Cut(strings.TrimSuffix(inner, ">"), ",")
		return &onkschema.TypeRef{IsMap: true, MapKey: strings.TrimSpace(key), MapVal: Type(strings.TrimSpace(value))}
	}
	return &onkschema.TypeRef{Name: typ}
}

// Oneof declares a oneof field from variants and oneof arguments.
func Oneof(name string, args []onkschema.Arg, variants ...onkschema.OneofVariant) *onkschema.FieldDecl {
	return &onkschema.FieldDecl{Name: name, Oneof: &onkschema.OneofDecl{Args: args, Variants: variants}}
}

// Variant declares a oneof variant.
func Variant(name, typ string, decorators ...onkschema.Decorator) onkschema.OneofVariant {
	return onkschema.OneofVariant{Name: name, Type: Type(typ), Decorators: decorators}
}

// Enum declares an enum.
func Enum(name string, values ...string) *onkschema.EnumDecl {
	e := &onkschema.EnumDecl{Name: name}
	for _, v := range values {
		e.Values = append(e.Values, onkschema.EnumValueDecl{Name: v})
	}
	return e
}

// Service declares a service; an empty base path is left to directory
// inference.
func Service(name, basePath string, rpcs ...*onkschema.RPCDecl) *onkschema.ServiceDecl {
	return &onkschema.ServiceDecl{Name: name, BasePath: basePath, RPCs: rpcs}
}

// RPC declares a method; errors follow the response after a "|", as in
// "Item|NotFound".
func RPC(name, request, response string, decorators ...onkschema.Decorator) *onkschema.RPCDecl {
	parts := strings.Split(response, "|")
	rpc := &onkschema.RPCDecl{Name: name, RequestType: request, ResponseType: strings.TrimSpace(parts[0]), Decorators: decorators}
	for _, e := range parts[1:] {
		rpc.ErrorTypes = append(rpc.ErrorTypes, strings.TrimSpace(e))
	}
	return rpc
}

// Header declares a header contract.
func Header(name string, decorators ...onkschema.Decorator) onkschema.HeaderDecl {
	return onkschema.HeaderDecl{Name: name, Type: "string", Decorators: decorators}
}

// Dec is a decorator.
func Dec(name string, args ...onkschema.Arg) onkschema.Decorator {
	return onkschema.Decorator{Name: name, Args: args}
}

// Q is a quoted decorator argument; N an unquoted one.
func Q(value string) onkschema.Arg { return onkschema.Arg{Value: value, Quoted: true} }

// N is an unquoted decorator argument, such as a number or a keyword.
func N(value string) onkschema.Arg { return onkschema.Arg{Value: value} }

// Named gives an argument a name, as in discriminator: "kind".
func Named(name string, arg onkschema.Arg) onkschema.Arg {
	arg.Name = name
	return arg
}
