package gendart

import (
	"strings"
	"unicode"

	"github.com/1homsi/onekit/internal/onkir"
)

var dartReserved = map[string]bool{
	"assert": true, "await": true, "break": true, "case": true, "catch": true, "class": true, "const": true,
	"continue": true, "default": true, "do": true, "else": true, "enum": true, "extends": true, "false": true,
	"final": true, "finally": true, "for": true, "if": true, "in": true, "is": true, "new": true, "null": true,
	"rethrow": true, "return": true, "super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "var": true, "void": true, "while": true, "with": true, "yield": true,
	"tojson": true, "fromjson": true, "validate": true, "hashcode": true, "runtimetype": true,
	"tostring": true, "nosuchmethod": true,
}

var enumMembers = map[string]bool{"values": true, "index": true, "name": true, "wire": true, "fromwire": true}

var clientMembers = map[string]bool{
	"close": true, "baseurl": true, "headers": true, "timeout": true, "maxresponsebodybytes": true,
	"maxsselinebytes": true, "maxwsframebytes": true, "maxwsmessagebytes": true, "wspinginterval": true,
}

func words(s string) []string {
	var out []string
	var cur []rune
	runes := []rune(s)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
	}
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ':
			flush()
		case unicode.IsUpper(r) && len(cur) > 0 && (unicode.IsLower(cur[len(cur)-1]) || unicode.IsDigit(cur[len(cur)-1]) ||
			i+1 < len(runes) && unicode.IsLower(runes[i+1])):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

func PascalCase(s string) string {
	var b strings.Builder
	for _, w := range words(s) {
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}

func CamelCase(s string) string {
	ws := words(s)
	var b strings.Builder
	for i, w := range ws {
		r := []rune(strings.ToLower(w))
		if i > 0 {
			r[0] = unicode.ToUpper(r[0])
		}
		b.WriteString(string(r))
	}
	out := b.String()
	if out == "" {
		out = "value"
	}
	if unicode.IsDigit([]rune(out)[0]) {
		out = "v" + out
	}
	return out
}

func Ident(name string) string {
	id := CamelCase(name)
	if dartReserved[strings.ToLower(id)] {
		return id + "_"
	}
	return id
}

func MethodIdent(name string) string {
	id := Ident(name)
	if clientMembers[strings.ToLower(id)] {
		return id + "_"
	}
	return id
}

func MessageName(m *onkir.Message) string {
	var parts []string
	for cur := m; cur != nil; cur = cur.Parent {
		parts = append([]string{cur.Name}, parts...)
	}
	return strings.Join(parts, "")
}

func EnumName(e *onkir.Enum) string {
	if e.Parent != nil {
		return MessageName(e.Parent) + e.Name
	}
	return e.Name
}

func EnumValueName(v *onkir.EnumValue) string {
	id := Ident(v.Name)
	if enumMembers[strings.ToLower(id)] {
		return id + "_"
	}
	return id
}

func OneofTypeName(m *onkir.Message, f *onkir.Field) string {
	return MessageName(m) + PascalCase(f.Name)
}

func OneofVariantClassName(m *onkir.Message, f *onkir.Field, v *onkir.OneofVariant) string {
	return OneofTypeName(m, f) + PascalCase(v.Name)
}
