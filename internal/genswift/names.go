package genswift

import (
	"strings"
	"unicode"

	"github.com/1homsi/onekit/internal/onkir"
)

var swiftReserved = map[string]bool{
	"associatedtype": true, "class": true, "deinit": true, "enum": true, "extension": true, "fileprivate": true,
	"func": true, "import": true, "init": true, "inout": true, "internal": true, "let": true, "open": true,
	"operator": true, "private": true, "precedencegroup": true, "protocol": true, "public": true, "rethrows": true,
	"static": true, "struct": true, "subscript": true, "typealias": true, "var": true, "break": true, "case": true,
	"catch": true, "continue": true, "default": true, "defer": true, "do": true, "else": true, "fallthrough": true,
	"for": true, "guard": true, "if": true, "in": true, "repeat": true, "return": true, "throw": true,
	"switch": true, "where": true, "while": true, "any": true, "as": true, "await": true, "false": true,
	"is": true, "nil": true, "self": true, "super": true, "throws": true, "true": true, "try": true,
	"type": true, "json": true, "description": true, "debugdescription": true, "hashvalue": true, "hash": true,
	"tojson": true, "tojsonvalue": true, "validate": true, "ordinal": true, "rawvalue": true, "allcases": true,
	"fromjson": true, "fromwire": true,
}

var clientMembers = map[string]bool{
	"baseurl": true, "headers": true, "timeout": true, "session": true, "maxresponsebodybytes": true,
	"maxsselinebytes": true, "init": true,
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
	if swiftReserved[strings.ToLower(id)] {
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

func EnumCaseName(v *onkir.EnumValue) string {
	return Ident(v.Name)
}

func OneofTypeName(m *onkir.Message, f *onkir.Field) string {
	return MessageName(m) + PascalCase(f.Name)
}

func swiftString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

func DeclaredNames(file *onkir.File) []string {
	var names []string
	for _, e := range fileEnumsDeep(file) {
		names = append(names, EnumName(e))
	}
	for _, m := range fileMessagesDeep(file) {
		names = append(names, MessageName(m))
		for _, f := range m.Fields {
			if f.Oneof != nil {
				names = append(names, OneofTypeName(m, f))
			}
		}
	}
	if hasHTTPMethods(file) {
		for _, s := range file.Services {
			if serviceHasHTTP(s) {
				names = append(names, s.Name+"Client")
			}
		}
	}
	return names
}
