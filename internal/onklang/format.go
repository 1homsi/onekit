package onklang

import (
	"strconv"
	"strings"
)

// Format parses src and emits the canonical, deterministic .onk layout.
// Formatting is deliberately AST-backed so malformed input is rejected before
// a file can be rewritten. Both documentation and ordinary leading comments
// are retained at their declaration/member boundaries.
func Format(src string) ([]byte, error) {
	file, err := Parse(src)
	if err != nil {
		return nil, err
	}
	var out formatter
	out.file(file)
	return []byte(out.finish()), nil
}

type formatter struct {
	buf       strings.Builder
	indent    int
	endsBlank bool
}

func (f *formatter) line(value string) {
	f.buf.WriteString(strings.Repeat("  ", f.indent))
	f.buf.WriteString(value)
	f.buf.WriteByte('\n')
	f.endsBlank = false
}

func (f *formatter) blank() {
	if f.buf.Len() == 0 || f.endsBlank {
		return
	}
	f.buf.WriteByte('\n')
	f.endsBlank = true
}

// finish collapses any trailing blank lines into one and guarantees the
// output ends with a single newline (or is empty).
func (f *formatter) finish() string {
	out := strings.TrimRight(f.buf.String(), "\n")
	if out == "" {
		return ""
	}
	return out + "\n"
}

func (f *formatter) docs(doc string) {
	if doc == "" {
		return
	}
	for _, line := range strings.Split(doc, "\n") {
		if line == "" {
			f.line("///")
		} else {
			f.line("/// " + line)
		}
	}
}

func (f *formatter) comments(comments []string) {
	for _, comment := range comments {
		for _, line := range strings.Split(comment, "\n") {
			f.line(strings.TrimRight(line, "\r"))
		}
	}
}

func (f *formatter) file(file *File) {
	if file.Package != "" {
		f.comments(file.LeadingComments)
		f.line("package " + file.Package)
		f.blank()
	}
	for index, imp := range file.Imports {
		if index < len(file.ImportComments) {
			f.comments(file.ImportComments[index])
		}
		f.line("import " + strconv.Quote(imp))
	}
	if len(file.Imports) > 0 {
		f.blank()
	}
	declarations := file.Declarations
	if len(declarations) == 0 {
		for _, message := range file.Messages {
			declarations = append(declarations, message)
		}
		for _, enum := range file.Enums {
			declarations = append(declarations, enum)
		}
		for _, service := range file.Services {
			declarations = append(declarations, service)
		}
	}
	for i, declaration := range declarations {
		if i > 0 {
			f.blank()
		}
		switch declaration := declaration.(type) {
		case *MessageDecl:
			f.message(declaration)
		case *EnumDecl:
			f.enum(declaration)
		case *ServiceDecl:
			f.service(declaration)
		}
	}
	if len(file.TrailingComments) > 0 {
		f.blank()
		f.comments(file.TrailingComments)
	}
}

func (f *formatter) message(message *MessageDecl) {
	f.comments(message.LeadingComments)
	f.docs(message.Doc)
	f.line("message " + message.Name + formatDecorators(message.Decorators) + " {")
	f.indent++
	members := message.Members
	if len(members) == 0 {
		for _, field := range message.Fields {
			members = append(members, field)
		}
		for _, nested := range message.Nested {
			members = append(members, nested)
		}
		for _, nested := range message.NestedEn {
			members = append(members, nested)
		}
	}
	for i, member := range members {
		if i > 0 {
			f.blank()
		}
		switch member := member.(type) {
		case *FieldDecl:
			f.field(member)
		case *MessageDecl:
			f.message(member)
		case *EnumDecl:
			f.enum(member)
		}
	}
	f.indent--
	f.line("}")
}

func (f *formatter) field(field *FieldDecl) {
	f.comments(field.LeadingComments)
	f.docs(field.Doc)
	if field.Oneof != nil {
		f.comments(field.Oneof.LeadingComments)
		value := field.Name + ": oneof" + formatArgs(field.Oneof.Args) + " {"
		f.line(value)
		f.indent++
		for i, variant := range field.Oneof.Variants {
			if i > 0 {
				f.blank()
			}
			f.comments(variant.LeadingComments)
			f.line(variant.Name + ": " + formatType(variant.Type) + formatDecorators(variant.Decorators))
		}
		f.indent--
		f.line("}")
		return
	}
	value := field.Name + ": " + formatType(field.Type)
	if field.Optional {
		value += "?"
	} else if field.Repeated {
		value += "[]"
	}
	f.line(value + formatDecorators(field.Decorators))
}

func (f *formatter) enum(enum *EnumDecl) {
	f.comments(enum.LeadingComments)
	f.docs(enum.Doc)
	f.line("enum " + enum.Name + " {")
	f.indent++
	for i, value := range enum.Values {
		if i > 0 {
			f.blank()
		}
		f.comments(value.LeadingComments)
		f.docs(value.Doc)
		f.line(value.Name + formatDecorators(value.Decorators))
	}
	f.indent--
	f.line("}")
}

func (f *formatter) service(service *ServiceDecl) {
	f.comments(service.LeadingComments)
	f.docs(service.Doc)
	f.line("service " + service.Name + " {")
	f.indent++
	if service.BasePath != "" {
		f.comments(service.BasePathComments)
		f.line("base_path: " + strconv.Quote(service.BasePath))
	}
	if len(service.Headers) > 0 {
		f.comments(service.HeadersComments)
		f.headers(service.Headers)
	}
	if service.BasePath != "" && len(service.RPCs) > 0 {
		f.blank()
	}
	for i, rpc := range service.RPCs {
		if i > 0 {
			f.blank()
		}
		f.rpc(rpc)
	}
	f.indent--
	f.line("}")
}

func (f *formatter) rpc(rpc *RPCDecl) {
	f.comments(rpc.LeadingComments)
	f.docs(rpc.Doc)
	var value strings.Builder
	value.WriteString(rpc.Name)
	value.WriteByte('(')
	value.WriteString(rpc.RequestType)
	value.WriteString(") -> ")
	value.WriteString(rpc.ResponseType)
	for _, errType := range rpc.ErrorTypes {
		value.WriteString(" | ")
		value.WriteString(errType)
	}
	value.WriteString(formatDecorators(rpc.Decorators))
	if len(rpc.Headers) == 0 {
		f.line(value.String())
		return
	}
	f.line(value.String() + " {")
	f.indent++
	f.comments(rpc.HeadersComments)
	f.headers(rpc.Headers)
	f.indent--
	f.line("}")
}

func (f *formatter) headers(headers []HeaderDecl) {
	f.line("headers: {")
	f.indent++
	for i, header := range headers {
		if i > 0 {
			f.blank()
		}
		f.comments(header.LeadingComments)
		f.line(strconv.Quote(header.Name) + ": " + header.Type + formatDecorators(header.Decorators))
	}
	f.indent--
	f.line("}")
}

func formatType(typ *TypeRef) string {
	if typ == nil {
		return "unknown"
	}
	if typ.IsMap {
		return "map[" + typ.MapKey + ", " + formatType(typ.MapVal) + "]"
	}
	return typ.Name
}

func formatDecorators(decorators []Decorator) string {
	var out strings.Builder
	for _, decorator := range decorators {
		out.WriteByte(' ')
		out.WriteByte('@')
		out.WriteString(decorator.Name)
		out.WriteString(formatArgs(decorator.Args))
	}
	return out.String()
}

func formatArgs(args []Arg) string {
	if len(args) == 0 {
		return ""
	}
	values := make([]string, 0, len(args))
	for _, arg := range args {
		value := arg.Value
		if arg.Quoted {
			value = strconv.Quote(value)
		}
		if arg.Name != "" {
			value = arg.Name + ": " + value
		}
		values = append(values, value)
	}
	return "(" + strings.Join(values, ", ") + ")"
}
