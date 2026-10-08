package gengo

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

//go:embed runtime/decodejson.go.tmpl
var decodeJSONRuntimeSource string

func writeDecodeJSONRuntime(p *Printer) {
	p.P(decodeJSONRuntimeSource)
}

type decodeGen struct {
	p       *Printer
	m       *onkir.Message
	fixups  []decodeFixup
	nextBit int
}

type decodeFixup struct {
	bit  int
	stmt []string
}

func writeDecodeJSONMethods(p *Printer, m *onkir.Message, c fieldCategories, exposeUnmarshal bool) {
	g := &decodeGen{p: p, m: m}
	g.writeDecode()
	p.P("func (m *", m.Name, ") DecodeJSON(data []byte) error {")
	p.P("d := onkDec{data: data}")
	p.P("m.onkDecode(&d)")
	p.P("if d.end() {")
	p.P("return nil")
	p.P("}")
	p.P("return m.onkDecodeSlow(data)")
	p.P("}")
	p.P()
	if c.needsUnmarshal() {
		writeCustomUnmarshalJSON(p, m, c, "onkDecodeSlow")
	} else {
		p.P("func (m *", m.Name, ") onkDecodeSlow(data []byte) error {")
		p.P("type alias ", m.Name)
		p.P("return json.Unmarshal(data, (*alias)(m))")
		p.P("}")
		p.P()
	}
	if exposeUnmarshal {
		p.P("func (m *", m.Name, ") UnmarshalJSON(data []byte) error {")
		p.P("return m.DecodeJSON(data)")
		p.P("}")
		p.P()
	}
}

func (g *decodeGen) writeDecode() {
	p, m := g.p, g.m
	p.P("func (m *", m.Name, ") onkDecode(d *onkDec) {")
	words := (len(m.Fields) + 63) / 64
	if words > 0 {
		p.P("var seen [", words, "]uint64")
	}
	p.P("if !d.null() {")
	p.P("if !d.open('{') {")
	p.P("return")
	p.P("}")
	p.P("for i := 0; d.next(i, '}'); i++ {")
	p.P("key := d.key()")
	p.P("if d.err != nil {")
	p.P("return")
	p.P("}")
	p.P("switch string(key) {")
	var keys []string
	for i, f := range m.Fields {
		keys = append(keys, strconv.Quote(f.Name))
		p.P("case ", strconv.Quote(f.Name), ":")
		p.P("if seen[", i/64, "]&(1<<", i%64, ") != 0 {")
		p.P("d.fail()")
		p.P("return")
		p.P("}")
		p.P("seen[", i/64, "] |= 1 << ", i%64)
		g.nextBit = i
		g.field(f)
	}
	p.P("default:")
	if len(keys) > 0 {
		p.P("if onkFoldKey(key, ", strings.Join(keys, ", "), ") {")
		p.P("d.fail()")
		p.P("return")
		p.P("}")
	}
	p.P("d.skip(0)")
	p.P("}")
	p.P("}")
	p.P("}")
	for _, fx := range g.fixups {
		p.P("if seen[", fx.bit/64, "]&(1<<", fx.bit%64, ") == 0 {")
		for _, s := range fx.stmt {
			p.P(s)
		}
		p.P("}")
	}
	p.P("}")
	p.P()
}

func (g *decodeGen) fixup(stmt ...string) {
	g.fixups = append(g.fixups, decodeFixup{bit: g.nextBit, stmt: stmt})
}

func (g *decodeGen) field(f *onkir.Field) {
	p := g.p
	target := "m." + GoFieldName(f)
	t := f.Type
	if f.Oneof != nil {
		g.oneof(f, target)
		return
	}
	kind := p.appendKindOf(t, f, false)
	switch {
	case f.Nullable:
		g.nullable(f, kind, target)
	case t.Kind == onkir.KindMap:
		g.mapField(f, target)
	case f.Repeated:
		g.repeated(f, kind, target)
	case kind == akMessage:
		p.P("if d.null() {")
		p.P(target, " = nil")
		p.P("continue")
		p.P("}")
		g.message(t, target)
	case f.Optional:
		g.optional(f, kind, target)
	default:
		g.plain(f, kind, target)
	}
}

func (g *decodeGen) nullable(f *onkir.Field, kind appendKind, target string) {
	p := g.p
	null := target + "Null"
	g.fixup(target+" = nil", null+" = false")
	p.P("if d.null() {")
	p.P(target, " = nil")
	p.P(null, " = true")
	p.P("continue")
	p.P("}")
	p.P(null, " = false")
	if kind == akMessage {
		g.message(f.Type, target)
		return
	}
	g.pointerValue(f, kind, target)
}

func (g *decodeGen) pointerValue(f *onkir.Field, kind appendKind, target string) {
	p := g.p
	p.P("var value ", p.GoFieldType(f.Type))
	g.value(kind, f.Type, f, "value", true, false)
	p.P(target, " = &value")
}

func (g *decodeGen) optional(f *onkir.Field, kind appendKind, target string) {
	p := g.p
	switch kind {
	case akIntString, akUintString, akBytesEncoded, akTimeEncoded, akEnumNumber:
		p.P("if d.null() {")
		p.P("continue")
		p.P("}")
	default:
		p.P("if d.null() {")
		p.P(target, " = nil")
		p.P("continue")
		p.P("}")
	}
	g.pointerValue(f, kind, target)
}

func (g *decodeGen) plain(f *onkir.Field, kind appendKind, target string) {
	p := g.p
	switch kind {
	case akEnumNumber:
		g.fixup(target + " = 0")
		p.P("if d.null() {")
		p.P(target, " = 0")
		p.P("continue")
		p.P("}")
	case akTimeEncoded:
		if timestampEncodingValue(f) != timestampEncodeDate {
			zero := target + " = time.Unix(0, 0).UTC()"
			if timestampEncodingValue(f) == timestampEncodeUnixMillis {
				zero = target + " = time.UnixMilli(0).UTC()"
			}
			g.fixup(zero)
			p.P("if d.null() {")
			p.P(zero)
			p.P("continue")
			p.P("}")
			break
		}
		p.P("if d.null() {")
		p.P("continue")
		p.P("}")
	case akBytes:
		p.P("if d.null() {")
		p.P(target, " = nil")
		p.P("continue")
		p.P("}")
	case akRaw, akEnum:
	default:
		p.P("if d.null() {")
		p.P("continue")
		p.P("}")
	}
	g.value(kind, f.Type, f, target, false, false)
}

func (g *decodeGen) repeated(f *onkir.Field, kind appendKind, target string) {
	p := g.p
	elem := p.repeatedItemType(f)
	p.P("if d.null() {")
	if kind == akIntString || kind == akUintString {
		p.P("continue")
	} else {
		p.P(target, " = nil")
		p.P("continue")
	}
	p.P("}")
	p.P("if !d.open('[') {")
	p.P("return")
	p.P("}")
	p.P(target, " = []", elem, "{}")
	p.P("for j := 0; d.next(j, ']'); j++ {")
	p.P("if cap(", target, ") == 0 {")
	p.P(target, " = make([]", elem, ", 0, 4)")
	p.P("}")
	switch kind {
	case akString, akBool, akInt, akUint, akFloat32, akFloat64, akBytes, akTime, akMessage:
		p.P("if d.null() {")
		p.P("var zero ", elem)
		p.P(target, " = append(", target, ", zero)")
		p.P("continue")
		p.P("}")
	}
	p.P("var item ", elem)
	if kind == akMessage {
		if f.ValueItems {
			g.messageInto(f.Type, "(&item)")
		} else {
			g.message(f.Type, "item")
		}
	} else {
		g.value(kind, f.Type, f, "item", true, false)
	}
	p.P(target, " = append(", target, ", item)")
	p.P("}")
}

func (g *decodeGen) mapField(f *onkir.Field, target string) {
	p := g.p
	t := f.Type
	if t.MapKey != onkir.ScalarString {
		p.P("d.delegate(&", target, ")")
		return
	}
	valueKind := p.appendKindOf(t.MapValue, nil, false)
	valueType := p.GoFieldType(t.MapValue)
	p.P("if d.null() {")
	p.P(target, " = nil")
	p.P("continue")
	p.P("}")
	p.P("if !d.open('{') {")
	p.P("return")
	p.P("}")
	p.P("if ", target, " == nil {")
	p.P(target, " = map[string]", valueType, "{}")
	p.P("}")
	p.P("for j := 0; d.next(j, '}'); j++ {")
	p.P("k := d.mapKey()")
	p.P("if d.err != nil {")
	p.P("return")
	p.P("}")
	switch valueKind {
	case akString, akBool, akInt, akUint, akFloat32, akFloat64, akBytes, akTime, akMessage:
		p.P("if d.null() {")
		p.P("var zero ", valueType)
		p.P(target, "[k] = zero")
		p.P("continue")
		p.P("}")
	}
	p.P("var item ", valueType)
	if valueKind == akMessage {
		g.message(t.MapValue, "item")
	} else {
		g.value(valueKind, t.MapValue, nil, "item", true, false)
	}
	p.P(target, "[k] = item")
	p.P("}")
}

func (g *decodeGen) message(t *onkir.Type, target string) {
	p := g.p
	typeName := p.MessageTypeName(t.Message)
	p.P("if ", target, " == nil {")
	p.P(target, " = new(", typeName, ")")
	p.P("}")
	g.messageInto(t, target)
}

func (g *decodeGen) messageInto(t *onkir.Type, target string) {
	p := g.p
	if p.hasAppend(t.Message) {
		p.P(target, ".onkDecode(d)")
		return
	}
	p.P("d.delegate(", target, ")")
}

func (g *decodeGen) oneof(f *onkir.Field, target string) {
	p, m := g.p, g.m
	disc := oneofDiscriminatorName(f)
	p.P("if d.null() {")
	p.P("continue")
	p.P("}")
	p.P("if !d.open('{') {")
	p.P("return")
	p.P("}")
	p.P("var tag string")
	names := []string{strconv.Quote(disc)}
	for i, variant := range f.Oneof.Variants {
		p.P("var v", i, " ", oneofWireFieldType(p, variant))
		names = append(names, strconv.Quote(variant.Name))
	}
	p.P("var oneofSeen uint64")
	p.P("for j := 0; d.next(j, '}'); j++ {")
	p.P("oneofKey := d.key()")
	p.P("if d.err != nil {")
	p.P("return")
	p.P("}")
	p.P("switch string(oneofKey) {")
	p.P("case ", strconv.Quote(disc), ":")
	p.P("if oneofSeen&1 != 0 {")
	p.P("d.fail()")
	p.P("return")
	p.P("}")
	p.P("oneofSeen |= 1")
	p.P("if !d.null() {")
	p.P("tag = d.str()")
	p.P("}")
	for i, variant := range f.Oneof.Variants {
		kind := p.appendKindOf(variant.Type, nil, true)
		p.P("case ", strconv.Quote(variant.Name), ":")
		p.P("if oneofSeen&(1<<", i+1, ") != 0 {")
		p.P("d.fail()")
		p.P("return")
		p.P("}")
		p.P("oneofSeen |= 1 << ", i+1)
		p.P("if d.null() {")
		p.P("v", i, " = nil")
		p.P("continue")
		p.P("}")
		slot := fmt.Sprintf("v%d", i)
		if kind == akMessage {
			g.message(variant.Type, slot)
			continue
		}
		p.P("var value ", p.GoFieldType(variant.Type))
		g.value(kind, variant.Type, nil, "value", true, true)
		p.P(slot, " = &value")
	}
	p.P("default:")
	p.P("if onkFoldKey(oneofKey, ", strings.Join(names, ", "), ") {")
	p.P("d.fail()")
	p.P("return")
	p.P("}")
	p.P("d.skip(0)")
	p.P("}")
	p.P("}")
	p.P("if d.err != nil {")
	p.P("return")
	p.P("}")
	p.P("switch tag {")
	for i, variant := range f.Oneof.Variants {
		kind := p.appendKindOf(variant.Type, nil, true)
		typeName := OneofVariantTypeName(m, f, variant)
		p.P("case ", strconv.Quote(variant.Tag()), ":")
		if kind == akMessage {
			p.P(target, " = &", typeName, "{", PascalCase(variant.Name), ": v", i, "}")
			continue
		}
		p.P("variant := &", typeName, "{}")
		p.P("if v", i, " != nil {")
		p.P("variant.", PascalCase(variant.Name), " = *v", i)
		p.P("}")
		p.P(target, " = variant")
	}
	p.P("}")
}

func bytesDecodeKind(f *onkir.Field) int {
	if f == nil {
		return 0
	}
	switch bytesEncodingValue(f) {
	case bytesEncodeBase64Raw:
		return 1
	case bytesEncodeBase64URL:
		return 2
	case bytesEncodeBase64URLRaw:
		return 3
	case bytesEncodeHex:
		return 4
	}
	return 0
}

func intBits(s onkir.ScalarKind) int {
	switch s {
	case onkir.ScalarInt32, onkir.ScalarUint32:
		return 32
	}
	return 64
}

func (g *decodeGen) value(kind appendKind, t *onkir.Type, f *onkir.Field, target string, strict, inOneof bool) {
	p := g.p
	goType := p.GoFieldType(t)
	switch kind {
	case akString:
		p.P(target, " = d.str()")
	case akBool:
		p.P(target, " = d.boolean()")
	case akInt:
		p.P(target, " = ", goType, "(d.int(", intBits(t.Scalar), "))")
	case akUint:
		p.P(target, " = ", goType, "(d.uint(", intBits(t.Scalar), "))")
	case akEnumNumber:
		p.P(target, " = ", goType, "(d.int(32))")
	case akFloat32:
		p.P(target, " = float32(d.float(32))")
	case akFloat64:
		p.P(target, " = d.float(64)")
	case akIntString:
		if strict {
			p.P(target, ", _ = d.intString(", inOneof, ", false)")
			return
		}
		p.P("if v, ok := d.intString(false, true); ok {")
		p.P(target, " = v")
		p.P("}")
	case akUintString:
		if strict {
			p.P(target, ", _ = d.uintString(", inOneof, ", false)")
			return
		}
		p.P("if v, ok := d.uintString(false, true); ok {")
		p.P(target, " = v")
		p.P("}")
	case akEnum:
		if p.isExternalEnum(t.Enum) {
			p.P("d.delegate(&", target, ")")
			return
		}
		p.P("switch string(d.name()) {")
		for _, v := range t.Enum.Values {
			p.P("case ", strconv.Quote(v.JSONName()), ":")
			p.P(target, " = ", t.Enum.Name+PascalCase(strings.ToLower(v.Name)))
		}
		p.P("default:")
		p.P("d.fail()")
		p.P("}")
	case akBytes:
		p.P(target, ", _ = d.bytes(0, false)")
	case akBytesEncoded:
		if strict {
			p.P(target, ", _ = d.bytes(", bytesDecodeKind(f), ", false)")
			return
		}
		p.P("if v, ok := d.bytes(", bytesDecodeKind(f), ", true); ok {")
		p.P(target, " = v")
		p.P("}")
	case akTime:
		p.P("if raw := d.raw(); d.err == nil {")
		p.P("if err := (&", target, ").UnmarshalJSON(raw); err != nil {")
		p.P("d.fail()")
		p.P("}")
		p.P("}")
	case akTimeEncoded:
		g.timeEncoded(f, target, strict)
	case akRaw:
		p.P("if raw := d.raw(); d.err == nil {")
		p.P(target, " = append(", target, "[:0], raw...)")
		p.P("}")
	case akMessage:
		g.messageInto(t, target)
	}
}

func (g *decodeGen) timeEncoded(f *onkir.Field, target string, strict bool) {
	p := g.p
	switch timestampEncodingValue(f) {
	case timestampEncodeUnixSeconds:
		p.P(target, " = time.Unix(d.int(64), 0).UTC()")
	case timestampEncodeUnixMillis:
		p.P(target, " = time.UnixMilli(d.int(64)).UTC()")
	case timestampEncodeDate:
		if !strict {
			p.P("if s := d.str(); s != \"\" {")
		} else {
			p.P("{")
			p.P("s := d.str()")
		}
		p.P("t, err := time.Parse(\"2006-01-02\", s)")
		p.P("if err != nil {")
		p.P("d.fail()")
		p.P("} else {")
		p.P(target, " = t")
		p.P("}")
		p.P("}")
	}
}
