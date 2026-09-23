package gengo

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

type fastKind int

const (
	fkUnsupported fastKind = iota
	fkString
	fkBool
	fkInt
	fkUint
	fkIntString
	fkUintString
	fkFloat
	fkEnum
	fkBytes
	fkMessage
	fkDelegate
)

func typeFastKind(t *onkir.Type, f *onkir.Field, inOneof bool) (fastKind, int) {
	if t == nil {
		return fkUnsupported, 0
	}
	switch t.Kind {
	case onkir.KindMessage:
		return fkMessage, 0
	case onkir.KindMap:
		return fkDelegate, 0
	case onkir.KindEnum:
		if f != nil && needsEnumNumberEncoding(f) {
			return fkUnsupported, 0
		}
		return fkEnum, 0
	case onkir.KindScalar:
	default:
		return fkUnsupported, 0
	}
	switch t.Scalar {
	case onkir.ScalarString:
		return fkString, 0
	case onkir.ScalarBool:
		return fkBool, 0
	case onkir.ScalarInt32:
		return fkInt, 32
	case onkir.ScalarUint32:
		return fkUint, 32
	case onkir.ScalarFloat32:
		return fkFloat, 32
	case onkir.ScalarFloat64:
		return fkFloat, 64
	case onkir.ScalarInt64, onkir.ScalarUint64:
		unsigned := t.Scalar == onkir.ScalarUint64
		if !inOneof && f != nil && needsInt64StringEncoding(f) {
			if unsigned {
				return fkUintString, 64
			}
			return fkIntString, 64
		}
		if unsigned {
			return fkUint, 64
		}
		return fkInt, 64
	case onkir.ScalarBytes:
		if f != nil && bytesEncodingValue(f) != "" {
			return fkUnsupported, 0
		}
		return fkBytes, 0
	case onkir.ScalarTimestamp:
		if f != nil && timestampEncodingValue(f) != "" {
			return fkUnsupported, 0
		}
		return fkDelegate, 0
	case onkir.ScalarJSON:
		return fkDelegate, 0
	}
	return fkUnsupported, 0
}

func fastJSONEligible(m *onkir.Message) bool {
	if m == nil || rootUnwrapField(m) != nil || len(m.Fields) > 64 {
		return false
	}
	for _, f := range m.Fields {
		if _, ok := flattenPrefix(f); ok {
			return false
		}
		if v := emptyBehaviorValue(f); v != "" && v != emptyBehaviorPreserve {
			return false
		}
		if f.Oneof != nil {
			for _, v := range f.Oneof.Variants {
				if k, _ := typeFastKind(v.Type, nil, true); k == fkUnsupported {
					return false
				}
			}
			continue
		}
		if k, _ := typeFastKind(f.Type, f, false); k == fkUnsupported {
			return false
		}
	}
	return true
}

func (p *Printer) fastChild(m *onkir.Message) bool {
	return !p.isExternal(m) && fastJSONEligible(m)
}

func jsonKey(name string) string {
	return fmt.Sprintf("%q", fmt.Sprintf("%q:", name))
}

func writeFastJSONMethods(p *Printer, m *onkir.Message) {
	name := m.Name
	p.P("func (m *", name, ") wsResetJSON() { *m = ", name, "{} }")
	p.P()
	writeFastEncode(p, m, name)
	writeFastDecode(p, m, name)
}

func writeFastEncode(p *Printer, m *onkir.Message, name string) {
	p.P("func (m *", name, ") wsAppendJSON(b []byte) ([]byte, error) {")
	p.P(`if m == nil { return append(b, "null"...), nil }`)
	p.P("var err error")
	p.P("first := true")
	p.P("_ = first")
	p.P("b = append(b, '{')")
	for _, f := range m.Fields {
		field := "m." + PascalCase(f.Name)
		key := jsonKey(f.Name)
		if f.Oneof != nil {
			writeFastEncodeOneof(p, m, f, field, key)
			continue
		}
		kind, bits := typeFastKind(f.Type, f, false)
		switch {
		case f.Repeated && kind != fkDelegate:
			p.P("if len(", field, ") > 0 {")
			p.P("b = wsAppendKey(b, &first, ", key, ")")
			p.P("b = append(b, '[')")
			p.P("for i, item := range ", field, " {")
			p.P("if i > 0 { b = append(b, ',') }")
			writeFastEncodeValue(p, f.Type, kind, bits, "item", f)
			p.P("}")
			p.P("b = append(b, ']')")
			p.P("}")
		case kind == fkDelegate:
			p.P("if ", fastOmitDelegate(f, field), " {")
			p.P("b = wsAppendKey(b, &first, ", key, ")")
			p.P("if b, err = wsAppendStd(b, ", field, "); err != nil { return b, err }")
			p.P("}")
		case kind == fkMessage:
			p.P("if ", field, " != nil {")
			p.P("b = wsAppendKey(b, &first, ", key, ")")
			writeFastEncodeValue(p, f.Type, kind, bits, field, f)
			p.P("}")
		case f.Optional:
			p.P("if ", field, " != nil {")
			p.P("b = wsAppendKey(b, &first, ", key, ")")
			writeFastEncodeValue(p, f.Type, kind, bits, "(*"+field+")", f)
			p.P("}")
		case kind == fkIntString || kind == fkUintString:
			p.P("b = wsAppendKey(b, &first, ", key, ")")
			writeFastEncodeValue(p, f.Type, kind, bits, field, f)
		default:
			p.P("if ", fastNonZero(kind, field), " {")
			p.P("b = wsAppendKey(b, &first, ", key, ")")
			writeFastEncodeValue(p, f.Type, kind, bits, field, f)
			p.P("}")
		}
	}
	p.P("_ = err")
	p.P("return append(b, '}'), nil")
	p.P("}")
	p.P()
}

func fastNonZero(kind fastKind, expr string) string {
	switch kind {
	case fkString:
		return expr + ` != ""`
	case fkBool:
		return expr
	case fkBytes:
		return "len(" + expr + ") > 0"
	default:
		return expr + " != 0"
	}
}

func fastOmitDelegate(f *onkir.Field, expr string) string {
	if f.Repeated || f.Optional {
		if f.Repeated {
			return "len(" + expr + ") > 0"
		}
		return expr + " != nil"
	}
	if f.Type.Kind == onkir.KindMap || (f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarJSON) {
		return "len(" + expr + ") > 0"
	}
	return "true"
}

func writeFastEncodeValue(p *Printer, t *onkir.Type, kind fastKind, bits int, expr string, f *onkir.Field) {
	switch kind {
	case fkString:
		p.P("b = wsAppendString(b, ", expr, ")")
	case fkBool:
		p.P("b = strconv.AppendBool(b, ", expr, ")")
	case fkInt:
		p.P("b = strconv.AppendInt(b, int64(", expr, "), 10)")
	case fkUint:
		p.P("b = strconv.AppendUint(b, uint64(", expr, "), 10)")
	case fkIntString:
		p.P("b = append(b, '\"')")
		p.P("b = strconv.AppendInt(b, int64(", expr, "), 10)")
		p.P("b = append(b, '\"')")
	case fkUintString:
		p.P("b = append(b, '\"')")
		p.P("b = strconv.AppendUint(b, uint64(", expr, "), 10)")
		p.P("b = append(b, '\"')")
	case fkFloat:
		p.P("if b, err = wsAppendFloat(b, float64(", expr, "), ", bits, "); err != nil { return b, err }")
	case fkEnum:
		p.P("{")
		p.P("raw, err := ", expr, ".MarshalJSON()")
		p.P("if err != nil { return b, err }")
		p.P("b = append(b, raw...)")
		p.P("}")
	case fkBytes:
		p.P("b = wsAppendBytes(b, ", expr, ")")
	case fkMessage:
		if p.fastChild(t.Message) {
			p.P("if b, err = ", expr, ".wsAppendJSON(b); err != nil { return b, err }")
		} else {
			p.P("if b, err = wsAppendStd(b, ", expr, "); err != nil { return b, err }")
		}
	default:
		p.P("if b, err = wsAppendStd(b, ", expr, "); err != nil { return b, err }")
	}
	_ = f
}

func writeFastEncodeOneof(p *Printer, m *onkir.Message, f *onkir.Field, field, key string) {
	disc := oneofDiscriminatorName(f)
	p.P("if ", field, " != nil {")
	p.P("b = wsAppendKey(b, &first, ", key, ")")
	p.P("switch v := ", field, ".(type) {")
	for _, variant := range f.Oneof.Variants {
		vName := PascalCase(variant.Name)
		p.P("case *", OneofVariantTypeName(m, f, variant), ":")
		p.P("b = append(b, ", fmt.Sprintf("%q", fmt.Sprintf("{%q:%q", disc, variant.Tag())), "...)")
		kind, bits := typeFastKind(variant.Type, nil, true)
		value := "v." + vName
		if kind == fkMessage {
			p.P("if ", value, " != nil {")
			p.P("b = append(b, ", fmt.Sprintf("%q", fmt.Sprintf(",%q:", variant.Name)), "...)")
			writeFastEncodeValue(p, variant.Type, kind, bits, value, nil)
			p.P("}")
		} else {
			p.P("b = append(b, ", fmt.Sprintf("%q", fmt.Sprintf(",%q:", variant.Name)), "...)")
			writeFastEncodeValue(p, variant.Type, kind, bits, value, nil)
		}
		p.P("b = append(b, '}')")
	}
	p.P("default:")
	p.P(`b = append(b, "null"...)`)
	p.P("}")
	p.P("}")
}

func writeFastDecode(p *Printer, m *onkir.Message, name string) {
	p.P("func (m *", name, ") wsDecodeJSON(d *wsJSON) {")
	p.P("if !d.open('{') { return }")
	p.P("var seen uint64")
	p.P("for i := 0; d.next(i, '}'); i++ {")
	p.P("key := d.key()")
	p.P("if d.err != nil { return }")
	p.P("switch string(key) {")
	var keys []string
	for i, f := range m.Fields {
		keys = append(keys, fmt.Sprintf("%q", f.Name))
		p.P("case ", fmt.Sprintf("%q", f.Name), ":")
		p.P("if seen&(1<<", i, ") != 0 { d.fail(); return }")
		p.P("seen |= 1 << ", i)
		writeFastDecodeField(p, m, f)
	}
	p.P("default:")
	if len(keys) > 0 {
		p.P("if wsFoldKey(key, ", strings.Join(keys, ", "), ") { d.fail(); return }")
	}
	p.P("d.skip(0)")
	p.P("}")
	p.P("}")
	p.P("}")
	p.P()
}

func writeFastDecodeField(p *Printer, m *onkir.Message, f *onkir.Field) {
	field := "m." + PascalCase(f.Name)
	if f.Oneof != nil {
		writeFastDecodeOneof(p, m, f, field)
		return
	}
	kind, bits := typeFastKind(f.Type, f, false)
	switch {
	case kind == fkDelegate:
		p.P("d.delegate(&", field, ")")
	case kind == fkEnum && !f.Repeated && !f.Optional:
		p.P("if raw := d.raw(); d.err == nil {")
		p.P("if err := (&", field, ").UnmarshalJSON(raw); err != nil { d.fail() }")
		p.P("}")
	case f.Repeated:
		elem := p.GoFieldType(f.Type)
		p.P("if d.null() { ", field, " = nil; continue }")
		p.P("if !d.open('[') { return }")
		p.P(field, " = []", elem, "{}")
		p.P("for j := 0; d.next(j, ']'); j++ {")
		if kind == fkMessage {
			p.P("if d.null() { ", field, " = append(", field, ", nil); continue }")
		}
		p.P("var item ", elem)
		writeFastDecodeValue(p, f.Type, kind, bits, "item", true)
		p.P(field, " = append(", field, ", item)")
		p.P("}")
	case kind == fkMessage:
		p.P("if d.null() { ", field, " = nil; continue }")
		writeFastDecodeValue(p, f.Type, kind, bits, field, true)
	case f.Optional:
		p.P("if d.null() { ", field, " = nil; continue }")
		p.P("var value ", p.GoFieldType(f.Type))
		writeFastDecodeValue(p, f.Type, kind, bits, "value", true)
		p.P(field, " = &value")
	default:
		p.P("if d.null() { continue }")
		writeFastDecodeValue(p, f.Type, kind, bits, field, false)
	}
}

func writeFastDecodeValue(p *Printer, t *onkir.Type, kind fastKind, bits int, target string, fresh bool) {
	goType := p.GoFieldType(t)
	switch kind {
	case fkString:
		p.P(target, " = d.str()")
	case fkBool:
		p.P(target, " = d.boolean()")
	case fkInt:
		p.P(target, " = ", goType, "(d.int(", bits, "))")
	case fkUint:
		p.P(target, " = ", goType, "(d.uint(", bits, "))")
	case fkIntString:
		p.P(target, " = ", goType, "(d.intString(", bits, "))")
	case fkUintString:
		p.P(target, " = ", goType, "(d.uintString(", bits, "))")
	case fkFloat:
		p.P(target, " = ", goType, "(d.float(", bits, "))")
	case fkEnum:
		p.P("if raw := d.raw(); d.err == nil {")
		p.P("if err := (&", target, ").UnmarshalJSON(raw); err != nil { d.fail() }")
		p.P("}")
	case fkBytes:
		p.P(target, " = d.bytes()")
	case fkMessage:
		p.P(target, " = new(", strings.TrimPrefix(goType, "*"), ")")
		if p.fastChild(t.Message) {
			p.P(target, ".wsDecodeJSON(d)")
		} else {
			p.P("d.delegate(", target, ")")
		}
	default:
		p.P("d.delegate(&", target, ")")
	}
	_ = fresh
}

func writeFastDecodeOneof(p *Printer, m *onkir.Message, f *onkir.Field, field string) {
	disc := oneofDiscriminatorName(f)
	p.P("if d.null() { continue }")
	p.P("if !d.open('{') { return }")
	p.P("var tag string")
	var names []string
	names = append(names, fmt.Sprintf("%q", disc))
	for i, variant := range f.Oneof.Variants {
		p.P("var v", i, " *", strings.TrimPrefix(p.GoFieldType(variant.Type), "*"))
		names = append(names, fmt.Sprintf("%q", variant.Name))
	}
	p.P("var oneofSeen uint64")
	p.P("for j := 0; d.next(j, '}'); j++ {")
	p.P("oneofKey := d.key()")
	p.P("if d.err != nil { return }")
	p.P("switch string(oneofKey) {")
	p.P("case ", fmt.Sprintf("%q", disc), ":")
	p.P("if oneofSeen&1 != 0 { d.fail(); return }")
	p.P("oneofSeen |= 1")
	p.P("if !d.null() { tag = d.str() }")
	for i, variant := range f.Oneof.Variants {
		kind, bits := typeFastKind(variant.Type, nil, true)
		p.P("case ", fmt.Sprintf("%q", variant.Name), ":")
		p.P("if oneofSeen&(1<<", i+1, ") != 0 { d.fail(); return }")
		p.P("oneofSeen |= 1 << ", i+1)
		p.P("if d.null() { v", i, " = nil; continue }")
		if kind == fkMessage {
			writeFastDecodeValue(p, variant.Type, kind, bits, fmt.Sprintf("v%d", i), true)
		} else {
			p.P("var value ", p.GoFieldType(variant.Type))
			writeFastDecodeValue(p, variant.Type, kind, bits, "value", true)
			p.P("v", i, " = &value")
		}
	}
	p.P("default:")
	p.P("if wsFoldKey(oneofKey, ", strings.Join(names, ", "), ") { d.fail(); return }")
	p.P("d.skip(0)")
	p.P("}")
	p.P("}")
	p.P("if d.err != nil { return }")
	p.P("switch tag {")
	for i, variant := range f.Oneof.Variants {
		kind, _ := typeFastKind(variant.Type, nil, true)
		typeName := OneofVariantTypeName(m, f, variant)
		p.P("case ", fmt.Sprintf("%q", variant.Tag()), ":")
		if kind == fkMessage {
			p.P(field, " = &", typeName, "{", PascalCase(variant.Name), ": v", i, "}")
			continue
		}
		p.P("variant := &", typeName, "{}")
		p.P("if v", i, " != nil { variant.", PascalCase(variant.Name), " = *v", i, " }")
		p.P(field, " = variant")
	}
	p.P("}")
}
