package gengo

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

// goReservedWords are Go keywords (plus predeclared literals that cannot be
// package identifiers); outputs ending in one get a "pkg_" prefix.
var goReservedWords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	"true": true, "false": true, "nil": true,
}

func GoPackageName(file *onkir.File) string {
	if file.Package == "" {
		return "generated"
	}
	name := strings.ToLower(file.Package[strings.LastIndex(file.Package, ".")+1:])
	if name == "" || name[0] >= '0' && name[0] <= '9' || goReservedWords[name] {
		return "pkg_" + name
	}
	return name
}

func needsTimeImport(file *onkir.File) bool {
	for _, m := range file.Messages {
		if messageUsesTime(m) {
			return true
		}
	}
	return false
}

func messageUsesTime(m *onkir.Message) bool {
	for _, f := range m.Fields {
		if f.Type != nil && typeUsesTime(f.Type) {
			return true
		}
	}
	for _, nested := range m.Nested {
		if messageUsesTime(nested) {
			return true
		}
	}
	return false
}

func typeUsesTime(t *onkir.Type) bool {
	switch t.Kind {
	case onkir.KindScalar:
		return t.Scalar == onkir.ScalarTimestamp
	case onkir.KindMap:
		return typeUsesTime(t.MapValue)
	case onkir.KindMessage, onkir.KindEnum:
		return false
	default:
		return false
	}
}

func hasOneofMessages(file *onkir.File) bool {
	for _, m := range file.Messages {
		if messageOrNestedHasOneof(m) {
			return true
		}
	}
	return false
}

func messageOrNestedHasOneof(m *onkir.Message) bool {
	if hasOneofField(m) {
		return true
	}
	for _, nested := range m.Nested {
		if messageOrNestedHasOneof(nested) {
			return true
		}
	}
	return false
}

func hasOneofField(m *onkir.Message) bool {
	return len(oneofFields(m)) > 0
}

func oneofFields(m *onkir.Message) []*onkir.Field {
	var fields []*onkir.Field
	for _, f := range m.Fields {
		if f.Oneof != nil {
			fields = append(fields, f)
		}
	}
	return fields
}

func hasErrorMessages(file *onkir.File) bool {
	for _, m := range file.Messages {
		if messageOrNestedHasError(m) {
			return true
		}
	}
	return false
}

func messageOrNestedHasError(m *onkir.Message) bool {
	if m.IsError() {
		return true
	}
	for _, nested := range m.Nested {
		if messageOrNestedHasError(nested) {
			return true
		}
	}
	return false
}

func fileHasFlattenFields(file *onkir.File) bool {
	var walk func(m *onkir.Message) bool
	walk = func(m *onkir.Message) bool {
		for _, f := range m.Fields {
			if _, ok := flattenPrefix(f); ok {
				return true
			}
		}
		for _, nested := range m.Nested {
			if walk(nested) {
				return true
			}
		}
		return false
	}
	for _, m := range file.Messages {
		if walk(m) {
			return true
		}
	}
	return false
}

type typesImports struct {
	time    bool
	fmt     bool
	json    bool
	hex     bool
	base64  bool
	strconv bool
	strings bool
	binary  bool
	errors  bool
	unsafe  bool
	io      bool
	math    bool
	utf8    bool
	sync    bool
	context bool
}

func (imp typesImports) any() bool {
	return imp.time || imp.fmt || imp.json || imp.hex || imp.base64 || imp.strconv || imp.strings || imp.binary || imp.errors || imp.unsafe || imp.io || imp.math || imp.utf8 || imp.sync || imp.context
}

func computeTypesImports(file *onkir.File) typesImports {
	hasEnums := len(file.Enums) > 0 || hasEnumsAnywhere(file)
	hasErrors := hasErrorMessages(file)
	enc := fileNeedsEncodingImports(file)
	return typesImports{
		time:    needsTimeImport(file),
		fmt:     hasErrors || hasEnums,
		json:    fileNeedsJSONHelpers(file) || hasEnums || fileUsesScalar(file, onkir.ScalarJSON),
		hex:     enc.hex,
		base64:  enc.base64,
		strconv: enc.strconv,
		strings: hasErrors || fileHasFlattenFields(file),
	}
}

func fileUsesScalar(file *onkir.File, scalar onkir.ScalarKind) bool {
	var typeUsesScalar func(*onkir.Type) bool
	typeUsesScalar = func(typ *onkir.Type) bool {
		if typ == nil {
			return false
		}
		switch typ.Kind {
		case onkir.KindScalar:
			return typ.Scalar == scalar
		case onkir.KindMap:
			return typeUsesScalar(typ.MapValue)
		default:
			return false
		}
	}
	var walk func(*onkir.Message) bool
	walk = func(message *onkir.Message) bool {
		for _, field := range message.Fields {
			if typeUsesScalar(field.Type) {
				return true
			}
		}
		for _, nested := range message.Nested {
			if walk(nested) {
				return true
			}
		}
		return false
	}
	for _, message := range file.Messages {
		if walk(message) {
			return true
		}
	}
	return false
}

func writeTypesImports(p *Printer, imp typesImports, externalRefs []PackageRef) {
	if !imp.any() && len(externalRefs) == 0 {
		return
	}
	p.P("import (")
	if imp.context {
		p.P(`"context"`)
	}
	if imp.binary {
		p.P(`"encoding/binary"`)
	}
	if imp.hex {
		p.P(`"encoding/hex"`)
	}
	if imp.json {
		p.P(`"encoding/json"`)
	}
	if imp.base64 {
		p.P(`"encoding/base64"`)
	}
	if imp.errors {
		p.P(`"errors"`)
	}
	if imp.fmt {
		p.P(`"fmt"`)
	}
	if imp.io {
		p.P(`"io"`)
	}
	if imp.math {
		p.P(`"math"`)
	}
	if imp.strconv {
		p.P(`"strconv"`)
	}
	if imp.strings {
		p.P(`"strings"`)
	}
	if imp.sync {
		p.P(`"sync"`)
	}
	if imp.time {
		p.P(`"time"`)
	}
	if imp.utf8 {
		p.P(`"unicode/utf8"`)
	}
	if imp.unsafe {
		p.P(`"unsafe"`)
	}
	for _, ref := range externalRefs {
		p.P(ref.Alias, " ", fmt.Sprintf("%q", ref.ImportPath))
	}
	p.P(")")
	p.P()
}

// GenerateTypes generates types.gen.go treating every message/enum type as
// local. Use GenerateTypesWithResolver for a multi-package project where some
// referenced types live in a different generated package.
func GenerateTypes(file *onkir.File) ([]byte, error) {
	return GenerateTypesWithResolver(file, nil)
}

func GenerateTypesWithResolver(file *onkir.File, resolver PackageResolver) ([]byte, error) {
	p := newPrinter(resolver)
	p.P("// Code generated by onek. DO NOT EDIT.")
	p.P("package ", GoPackageName(file))
	p.P()

	imp := computeTypesImports(file)
	hasWS := onkir.FileHasWSMethods(file)
	hasRaw := hasWS && fileHasWSRaw(p, file)
	imp.json = imp.json || hasWS
	imp.binary, imp.errors, imp.unsafe = hasRaw, hasRaw, hasRaw
	imp.io = hasWS
	imp.binary = imp.binary || hasWS
	if hasWS {
		imp.errors, imp.strconv, imp.strings, imp.base64, imp.math, imp.utf8, imp.sync = true, true, true, true, true, true, true
		imp.context, imp.time = true, true
	}
	writeTypesImports(p, imp, collectExternalRefs(file, resolver))

	for _, e := range file.Enums {
		writeEnum(p, e)
	}
	for _, m := range file.Messages {
		writeMessage(p, m)
	}
	if hasWS {
		writeWSCodecRuntime(p, hasRaw)
		writeWSJSONRuntime(p)
		for _, m := range fileMessagesDeep(file) {
			if fastJSONEligible(m) {
				writeFastJSONMethods(p, m)
			}
		}
	}
	if hasWS {
		for _, m := range fileMessagesDeep(file) {
			if onkir.MessageHasWSTimeout(m) {
				writeWSTimeoutMethod(p, m)
			}
		}
	}
	if hasRaw {
		for _, m := range fileMessagesDeep(file) {
			if onkir.MessageHasRaw(m, p.isExternal) {
				writeRawMethods(p, m)
			}
		}
	}

	return p.Format()
}

func hasEnumsAnywhere(file *onkir.File) bool {
	var walk func(m *onkir.Message) bool
	walk = func(m *onkir.Message) bool {
		if len(m.NestedEnums) > 0 {
			return true
		}
		for _, nested := range m.Nested {
			if walk(nested) {
				return true
			}
		}
		return false
	}
	for _, m := range file.Messages {
		if walk(m) {
			return true
		}
	}
	return false
}

func writeEnum(p *Printer, e *onkir.Enum) {
	p.P("type ", e.Name, " int32")
	p.P()
	p.P("const (")
	for i, v := range e.Values {
		constName := e.Name + PascalCase(strings.ToLower(v.Name))
		if i == 0 {
			p.P(constName, " ", e.Name, " = iota")
		} else {
			p.P(constName)
		}
	}
	p.P(")")
	p.P()

	p.P("func (v ", e.Name, ") String() string {")
	p.P("switch v {")
	for _, v := range e.Values {
		constName := e.Name + PascalCase(strings.ToLower(v.Name))
		p.P("case ", constName, ":")
		p.P("return ", fmt.Sprintf("%q", v.JSONName()))
	}
	p.P("default:")
	p.P(`return "unknown"`)
	p.P("}")
	p.P("}")
	p.P()

	p.P("func (v ", e.Name, ") IsValid() bool {")
	p.P("return v >= ", e.Name+PascalCase(strings.ToLower(e.Values[0].Name)), " && v <= ", e.Name+PascalCase(strings.ToLower(e.Values[len(e.Values)-1].Name)))
	p.P("}")
	p.P()

	p.P("func (v ", e.Name, ") MarshalJSON() ([]byte, error) {")
	p.P("if !v.IsValid() {")
	p.P("return nil, fmt.Errorf(", fmt.Sprintf("%q", e.Name+": invalid value %d"), ", int32(v))")
	p.P("}")
	p.P("return json.Marshal(v.String())")
	p.P("}")
	p.P()

	p.P("func (v *", e.Name, ") UnmarshalJSON(data []byte) error {")
	p.P("var s string")
	p.P("if err := json.Unmarshal(data, &s); err != nil {")
	p.P("return err")
	p.P("}")
	p.P("switch s {")
	for _, v := range e.Values {
		constName := e.Name + PascalCase(strings.ToLower(v.Name))
		p.P("case ", fmt.Sprintf("%q", v.JSONName()), ":")
		p.P("*v = ", constName)
	}
	p.P("default:")
	p.P("return fmt.Errorf(", fmt.Sprintf("%q", e.Name+": unknown value %q"), ", s)")
	p.P("}")
	p.P("return nil")
	p.P("}")
	p.P()
}

func writeMessage(p *Printer, m *onkir.Message) {
	p.P("type ", m.Name, " struct {")
	for _, f := range m.Fields {
		writeField(p, m, f)
	}
	p.P("}")
	p.P()

	if m.IsError() {
		writeErrorMethod(p, m)
	}
	if root := rootUnwrapField(m); root != nil {
		writeRootUnwrapJSON(p, m, root)
	} else if messageNeedsCustomJSON(m) {
		writeCustomJSONMethods(p, m)
	}

	writeFieldGetters(p, m)

	for _, f := range m.Fields {
		if f.Oneof != nil {
			writeOneof(p, m, f)
			writeOneofWireType(p, m, f)
		}
	}
	for _, nested := range m.Nested {
		writeMessage(p, nested)
	}
	for _, nested := range m.NestedEnums {
		writeEnum(p, nested)
	}
}

func oneofDiscriminatorName(f *onkir.Field) string {
	if disc, ok := f.Oneof.Discriminator(); ok && disc != "" {
		return disc
	}
	return "type"
}

// oneofWireName is the unexported struct a oneof field travels as on the
// wire: its discriminator tag plus one pointer per variant, all but one nil.
func oneofWireName(m *onkir.Message, f *onkir.Field) string {
	return "wire" + OneofInterfaceName(m, f)
}

// oneofWireFieldType is a variant's type inside the wire struct: always a
// pointer, so omitempty drops exactly the unset variants and never a set
// variant whose value happens to be its zero value.
func oneofWireFieldType(p *Printer, variant *onkir.OneofVariant) string {
	t := p.GoFieldType(variant.Type)
	if strings.HasPrefix(t, "*") {
		return t
	}
	return "*" + t
}

// writeOneofWireType emits the wire struct for one oneof field. A message's
// MarshalJSON/UnmarshalJSON carry it directly in their aux struct, so the
// payload is encoded and decoded in the same single encoding/json pass as the
// rest of the message. The earlier approach - a map[string]any marshaled into
// a RawMessage on the way out, and three json.Unmarshal calls over the same
// bytes (outer message, discriminator, variant) on the way in - rescanned and
// copied the payload at every level, making a large frame several times
// slower than the same payload in a plain struct.
func writeOneofWireType(p *Printer, m *onkir.Message, f *onkir.Field) {
	p.P("type ", oneofWireName(m, f), " struct {")
	p.P("Tag string `json:\"", oneofDiscriminatorName(f), "\"`")
	for _, variant := range f.Oneof.Variants {
		p.P("V", PascalCase(variant.Name), " ", oneofWireFieldType(p, variant), " `json:\"", variant.Name, ",omitempty\"`")
	}
	p.P("}")
	p.P()
}

// writeOneofMarshalField/writeOneofUnmarshalField emit the per-oneof-field
// logic inside a message's combined MarshalJSON/UnmarshalJSON (see
// jsonmapping_codegen.go): moving between the Go interface-typed field, which
// encoding/json can't handle alone, and its wire struct.
func writeOneofMarshalField(p *Printer, m *onkir.Message, f *onkir.Field) {
	goName := PascalCase(f.Name)
	p.P("switch v := m.", goName, ".(type) {")
	for _, variant := range f.Oneof.Variants {
		typeName := OneofVariantTypeName(m, f, variant)
		value := "v." + PascalCase(variant.Name)
		if !strings.HasPrefix(p.GoFieldType(variant.Type), "*") {
			value = "&" + value
		}
		p.P("case *", typeName, ":")
		p.P("aux.", goName, " = &", oneofWireName(m, f), "{Tag: ", fmt.Sprintf("%q", variant.Tag()), ", V", PascalCase(variant.Name), ": ", value, "}")
	}
	p.P("}")
}

func writeOneofUnmarshalField(p *Printer, m *onkir.Message, f *onkir.Field) {
	goName := PascalCase(f.Name)
	p.P("if w := aux.", goName, "; w != nil {")
	p.P("switch w.Tag {")
	for _, variant := range f.Oneof.Variants {
		typeName := OneofVariantTypeName(m, f, variant)
		field := "w.V" + PascalCase(variant.Name)
		p.P("case ", fmt.Sprintf("%q", variant.Tag()), ":")
		if strings.HasPrefix(p.GoFieldType(variant.Type), "*") {
			p.P("m.", goName, " = &", typeName, "{", PascalCase(variant.Name), ": ", field, "}")
			continue
		}
		p.P("variant := &", typeName, "{}")
		p.P("if ", field, " != nil { variant.", PascalCase(variant.Name), " = *", field, " }")
		p.P("m.", goName, " = variant")
	}
	p.P("}")
	p.P("}")
}

func writeErrorMethod(p *Printer, m *onkir.Message) {
	p.P("func (m *", m.Name, ") Error() string {")
	p.P(`parts := []string{"`, m.Name, `"}`)
	for _, f := range m.Fields {
		if f.Oneof != nil {
			continue
		}
		p.P(`parts = append(parts, fmt.Sprintf("`, f.Name, `=%v", m.`, PascalCase(f.Name), "))")
	}
	p.P(`return strings.Join(parts, " ")`)
	p.P("}")
	p.P()
}

func writeField(p *Printer, m *onkir.Message, f *onkir.Field) {
	goName := PascalCase(f.Name)
	if f.Oneof != nil {
		p.P(goName, " ", OneofInterfaceName(m, f), " `json:\"", f.Name, ",omitempty\"`")
		return
	}

	goType := p.GoFieldType(f.Type)
	if f.Repeated {
		goType = "[]" + goType
	} else if f.Optional && f.Type.Kind != onkir.KindMessage {
		goType = "*" + goType
	}

	p.P(goName, " ", goType, " `json:\"", f.Name, ",omitempty\"`")
}

// writeFieldGetters emits protoc-gen-go-style Get<Field>() accessor methods
// for every field on m. Consumers that were written against protoc-gen-go's
// generated code (the overwhelmingly common case for existing Go backends)
// call these instead of touching struct fields directly, and rely on them
// being nil-receiver-safe so chains like req.GetBusiness().GetName() work
// the same way they did with proto - GetX() on a nil *T returns the zero
// value instead of panicking.
func writeFieldGetters(p *Printer, m *onkir.Message) {
	for _, f := range m.Fields {
		if f.Oneof != nil {
			writeOneofGetters(p, m, f)
			continue
		}
		writeFieldGetter(p, m, f)
	}
}

func writeFieldGetter(p *Printer, m *onkir.Message, f *onkir.Field) {
	goName := PascalCase(f.Name)
	getterType := p.GoFieldType(f.Type)
	if f.Repeated {
		getterType = "[]" + getterType
	}
	dereference := !f.Repeated && f.Optional && f.Type.Kind != onkir.KindMessage

	p.P("func (x *", m.Name, ") Get", goName, "() ", getterType, " {")
	p.P("if x == nil {")
	p.P("var zero ", getterType)
	p.P("return zero")
	p.P("}")
	if dereference {
		p.P("if x.", goName, " == nil {")
		p.P("var zero ", getterType)
		p.P("return zero")
		p.P("}")
		p.P("return *x.", goName)
	} else {
		p.P("return x.", goName)
	}
	p.P("}")
	p.P()
}

// writeOneofGetters emits a nil-safe Get<Field>() returning the oneof's
// marker interface (mirroring protoc-gen-go's oneof wrapper getter), plus one
// Get<Variant>() per variant that type-asserts into that specific variant and
// returns its unwrapped value (the zero value if a different variant, or
// none, is set) - matching protoc-gen-go's oneof-variant getter convention.
func writeOneofGetters(p *Printer, m *onkir.Message, f *onkir.Field) {
	goName := PascalCase(f.Name)
	iface := OneofInterfaceName(m, f)

	p.P("func (x *", m.Name, ") Get", goName, "() ", iface, " {")
	p.P("if x == nil {")
	p.P("return nil")
	p.P("}")
	p.P("return x.", goName)
	p.P("}")
	p.P()

	for _, variant := range f.Oneof.Variants {
		variantGoName := PascalCase(variant.Name)
		typeName := OneofVariantTypeName(m, f, variant)
		variantType := p.GoFieldType(variant.Type)

		p.P("func (x *", m.Name, ") Get", variantGoName, "() ", variantType, " {")
		p.P("if v, ok := x.Get", goName, "().(*", typeName, "); ok {")
		p.P("return v.", variantGoName)
		p.P("}")
		p.P("var zero ", variantType)
		p.P("return zero")
		p.P("}")
		p.P()
	}
}

func writeOneof(p *Printer, m *onkir.Message, f *onkir.Field) {
	iface := OneofInterfaceName(m, f)
	marker := "is" + iface

	p.P("type ", iface, " interface {")
	p.P(marker, "()")
	p.P("}")
	p.P()

	for _, variant := range f.Oneof.Variants {
		typeName := OneofVariantTypeName(m, f, variant)
		p.P("type ", typeName, " struct {")
		p.P(PascalCase(variant.Name), " ", p.GoFieldType(variant.Type), " `json:\"", variant.Name, ",omitempty\"`")
		p.P("}")
		p.P()
		p.P("func (*", typeName, ") ", marker, "() {}")
		p.P()
	}
}
