package gengo

import (
	_ "embed"
	"strconv"

	"github.com/1homsi/onekit/internal/onkir"
)

//go:embed runtime/appendjson.go.tmpl
var appendJSONRuntimeSource string

func writeAppendJSONRuntime(p *Printer) {
	p.P(appendJSONRuntimeSource)
}

type appendKind int

const (
	akUnsupported appendKind = iota
	akString
	akBool
	akInt
	akUint
	akIntString
	akUintString
	akFloat32
	akFloat64
	akEnum
	akEnumNumber
	akBytes
	akBytesEncoded
	akTime
	akTimeEncoded
	akRaw
	akMessage
)

func (p *Printer) appendKindOf(t *onkir.Type, f *onkir.Field, inOneof bool) appendKind {
	if t == nil {
		return akUnsupported
	}
	switch t.Kind {
	case onkir.KindMessage:
		return akMessage
	case onkir.KindEnum:
		if f != nil && needsEnumNumberEncoding(f) {
			return akEnumNumber
		}
		return akEnum
	case onkir.KindScalar:
	default:
		return akUnsupported
	}
	switch t.Scalar {
	case onkir.ScalarString:
		return akString
	case onkir.ScalarBool:
		return akBool
	case onkir.ScalarInt32:
		return akInt
	case onkir.ScalarUint32:
		return akUint
	case onkir.ScalarFloat32:
		return akFloat32
	case onkir.ScalarFloat64:
		return akFloat64
	case onkir.ScalarInt64, onkir.ScalarUint64:
		unsigned := t.Scalar == onkir.ScalarUint64
		if inOneof || f != nil && needsInt64StringEncoding(f) {
			if unsigned {
				return akUintString
			}
			return akIntString
		}
		if unsigned {
			return akUint
		}
		return akInt
	case onkir.ScalarBytes:
		if f != nil && bytesEncodingValue(f) != "" {
			return akBytesEncoded
		}
		return akBytes
	case onkir.ScalarTimestamp:
		if f != nil && timestampEncodingValue(f) != "" {
			return akTimeEncoded
		}
		return akTime
	case onkir.ScalarJSON:
		return akRaw
	}
	return akUnsupported
}

func (p *Printer) isExternalEnum(e *onkir.Enum) bool {
	if p.resolver == nil {
		return false
	}
	_, ok := p.resolver.ResolveEnum(e)
	return ok
}

func (p *Printer) appendFieldSupported(f *onkir.Field) bool {
	if f.Oneof != nil {
		if f.Oneof.Flatten() || len(f.Oneof.Variants) > 62 {
			return false
		}
		for _, v := range f.Oneof.Variants {
			if p.appendKindOf(v.Type, nil, true) == akUnsupported {
				return false
			}
		}
		return true
	}
	if _, ok := flattenPrefix(f); ok {
		return false
	}
	if v := emptyBehaviorValue(f); v != "" && v != emptyBehaviorPreserve {
		return false
	}
	t := f.Type
	if t == nil {
		return false
	}
	if t.Kind == onkir.KindMap {
		if f.Repeated || f.Optional || f.Nullable {
			return false
		}
		return p.appendKindOf(t.MapValue, nil, false) != akUnsupported
	}
	kind := p.appendKindOf(t, f, false)
	if kind == akUnsupported {
		return false
	}
	if f.Optional && f.EmitZero {
		return false
	}
	if f.Nullable {
		if f.Repeated || f.EmitZero || !f.Optional {
			return false
		}
		switch kind {
		case akBytes, akBytesEncoded, akRaw, akTimeEncoded, akEnumNumber, akIntString, akUintString:
			return false
		}
	}
	if kind == akMessage && !f.Repeated && !f.Nullable && f.EmitZero && !f.AlwaysSent {
		return false
	}
	return true
}

func (p *Printer) appendEligible(m *onkir.Message) bool {
	if m == nil || rootUnwrapField(m) != nil {
		return false
	}
	for _, f := range m.Fields {
		if !p.appendFieldSupported(f) {
			return false
		}
	}
	return true
}

func appendChildMessages(m *onkir.Message) []*onkir.Message {
	var out []*onkir.Message
	add := func(t *onkir.Type) {
		for t != nil && t.Kind == onkir.KindMap {
			t = t.MapValue
		}
		if t != nil && t.Kind == onkir.KindMessage && t.Message != nil {
			out = append(out, t.Message)
		}
	}
	for _, f := range m.Fields {
		if f.Oneof != nil {
			for _, v := range f.Oneof.Variants {
				add(v.Type)
			}
			continue
		}
		add(f.Type)
	}
	return out
}

func (p *Printer) appendable(m *onkir.Message) bool {
	if m == nil || p.isExternal(m) {
		return false
	}
	if v, ok := p.appendMemo[m]; ok {
		return v
	}
	if p.appendMemo == nil {
		p.appendMemo = map[*onkir.Message]bool{}
	}
	var nodes []*onkir.Message
	seen := map[*onkir.Message]bool{}
	var visit func(n *onkir.Message)
	visit = func(n *onkir.Message) {
		if n == nil || seen[n] || p.isExternal(n) {
			return
		}
		seen[n] = true
		nodes = append(nodes, n)
		for _, c := range appendChildMessages(n) {
			visit(c)
		}
	}
	visit(m)
	in := map[*onkir.Message]bool{}
	eligible := map[*onkir.Message]bool{}
	for _, n := range nodes {
		eligible[n] = p.appendEligible(n)
		if eligible[n] && messageNeedsCustomJSON(n) {
			in[n] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, n := range nodes {
			if in[n] || !eligible[n] {
				continue
			}
			for _, c := range appendChildMessages(n) {
				if in[c] {
					in[n] = true
					changed = true
					break
				}
			}
		}
	}
	for _, n := range nodes {
		p.appendMemo[n] = in[n]
	}
	return in[m]
}

func (p *Printer) hasAppend(m *onkir.Message) bool {
	if m == nil || p.isExternal(m) {
		return false
	}
	if p.appendable(m) {
		return true
	}
	return p.appendPulled[m]
}

func (p *Printer) pullAppendChildren(file *onkir.File) {
	roots := fileMessagesDeep(file)
	inFile := make(map[*onkir.Message]bool, len(roots))
	for _, m := range roots {
		inFile[m] = true
	}
	p.appendPulled = map[*onkir.Message]bool{}
	var queue []*onkir.Message
	for _, m := range roots {
		if p.appendable(m) {
			queue = append(queue, m)
		}
	}
	for _, s := range file.Services {
		for _, method := range s.Methods {
			if method.IsWebSocket() || method.IsRawHTTP() {
				continue
			}
			wire := append([]*onkir.Message{method.Request, method.Response}, method.ErrorTypes...)
			for _, m := range wire {
				if m == nil || !inFile[m] || p.isExternal(m) || p.appendable(m) || p.appendPulled[m] || !p.appendEligible(m) {
					continue
				}
				p.appendPulled[m] = true
				queue = append(queue, m)
			}
		}
	}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		for _, c := range appendChildMessages(m) {
			if !inFile[c] || p.isExternal(c) || p.appendable(c) || p.appendPulled[c] || !p.appendEligible(c) {
				continue
			}
			p.appendPulled[c] = true
			queue = append(queue, c)
		}
	}
}

func fileHasAppendable(p *Printer, file *onkir.File) bool {
	for _, m := range fileMessagesDeep(file) {
		if p.hasAppend(m) {
			return true
		}
	}
	return false
}

func fileHasStreamingAux(p *Printer, file *onkir.File) bool {
	for _, m := range fileMessagesDeep(file) {
		if messageStreamsJSON(m) && !p.appendable(m) {
			return true
		}
	}
	return false
}

const (
	appendStateNone = iota
	appendStateSome
	appendStateMaybe
)

type appendGen struct {
	p     *Printer
	m     *onkir.Message
	state int
}

func writeAppendJSONMethods(p *Printer, m *onkir.Message, marshal bool) {
	g := &appendGen{p: p, m: m}
	g.write(marshal)
}

func (g *appendGen) write(marshal bool) {
	p, m := g.p, g.m
	p.P("func (m *", m.Name, ") AppendJSON(b []byte) ([]byte, error) {")
	p.P(`if m == nil {`)
	p.P(`return append(b, "null"...), nil`)
	p.P("}")
	p.P("var err error")
	conds := make([]string, len(m.Fields))
	always := make([]bool, len(m.Fields))
	for i, f := range m.Fields {
		conds[i], always[i] = g.fieldCondition(f)
	}
	g.state = appendStateNone
	if len(m.Fields) > 0 && !always[0] {
		g.state = appendStateMaybe
		p.P("sep := byte('{')")
	}
	for i, f := range m.Fields {
		g.field(f, conds[i], always[i])
	}
	p.P("_ = err")
	switch g.state {
	case appendStateNone:
		p.P(`return append(b, "{}"...), nil`)
	case appendStateMaybe:
		p.P("if sep == '{' {")
		p.P("b = append(b, '{')")
		p.P("}")
		p.P("return append(b, '}'), nil")
	default:
		p.P("return append(b, '}'), nil")
	}
	p.P("}")
	p.P()
	if !marshal {
		return
	}
	p.P("func (m *", m.Name, ") MarshalJSON() ([]byte, error) {")
	p.P("return onkMarshalJSON(m)")
	p.P("}")
	p.P()
	p.P("func (m *", m.Name, ") MarshalJSONTo(enc *jsontext.Encoder) error {")
	p.P("return onkMarshalJSONTo(enc, m)")
	p.P("}")
	p.P()
}

func (g *appendGen) fieldCondition(f *onkir.Field) (string, bool) {
	expr := "m." + GoFieldName(f)
	switch {
	case f.Oneof != nil:
		return expr + " != nil", false
	case f.Nullable:
		if f.AlwaysSent {
			return "", true
		}
		return expr + " != nil || " + expr + "Null", false
	case f.Type.Kind == onkir.KindMap:
		if f.EmitZero {
			return "", true
		}
		return "len(" + expr + ") > 0", false
	case f.Repeated:
		if f.EmitZero {
			return "", true
		}
		return "len(" + expr + ") > 0", false
	}
	kind := g.p.appendKindOf(f.Type, f, false)
	if f.Optional && kind != akMessage {
		return expr + " != nil", false
	}
	switch kind {
	case akMessage:
		if f.AlwaysSent {
			return "", true
		}
		return expr + " != nil", false
	case akIntString, akUintString, akEnumNumber, akTime, akTimeEncoded:
		return "", true
	}
	if f.EmitZero {
		return "", true
	}
	switch kind {
	case akString:
		return expr + ` != ""`, false
	case akBool:
		return expr, false
	case akBytes, akBytesEncoded, akRaw:
		return "len(" + expr + ") > 0", false
	}
	return expr + " != 0", false
}

func (g *appendGen) key(name string, conditional bool) {
	p := g.p
	k := `"` + name + `":`
	switch g.state {
	case appendStateNone:
		p.P("b = append(b, ", strconv.Quote("{"+k), "...)")
	case appendStateSome:
		p.P("b = append(b, ", strconv.Quote(","+k), "...)")
	default:
		p.P("b = append(b, sep)")
		p.P("b = append(b, ", strconv.Quote(k), "...)")
		if conditional {
			p.P("sep = ','")
		}
	}
}

func (g *appendGen) field(f *onkir.Field, cond string, always bool) {
	p := g.p
	if !always {
		p.P("if ", cond, " {")
	}
	g.key(f.Name, !always)
	g.fieldBody(f)
	if !always {
		p.P("}")
	}
	if always {
		g.state = appendStateSome
	} else if g.state == appendStateNone {
		g.state = appendStateMaybe
	}
}

func (g *appendGen) fieldBody(f *onkir.Field) {
	p := g.p
	expr := "m." + GoFieldName(f)
	t := f.Type
	switch {
	case f.Oneof != nil:
		g.oneofBody(f, expr)
	case f.Nullable:
		kind := p.appendKindOf(t, f, false)
		p.P("if ", expr, " == nil {")
		p.P(`b = append(b, "null"...)`)
		p.P("} else {")
		if kind == akMessage {
			g.value(kind, t, f, expr)
		} else {
			g.value(kind, t, f, "(*"+expr+")")
		}
		p.P("}")
	case t.Kind == onkir.KindMap:
		g.mapBody(f, expr)
	case f.Repeated:
		kind := p.appendKindOf(t, f, false)
		p.P("b = append(b, '[')")
		p.P("for i := range ", expr, " {")
		p.P("if i > 0 {")
		p.P("b = append(b, ',')")
		p.P("}")
		item := expr + "[i]"
		if kind == akMessage && f.ValueItems {
			item = "(&" + item + ")"
		}
		g.value(kind, t, f, item)
		p.P("}")
		p.P("b = append(b, ']')")
	default:
		kind := p.appendKindOf(t, f, false)
		switch {
		case kind == akMessage && f.AlwaysSent:
			p.P("{")
			p.P("v := ", expr)
			p.P("if v == nil {")
			p.P("v = new(", p.MessageTypeName(t.Message), ")")
			p.P("}")
			g.value(kind, t, f, "v")
			p.P("}")
		case kind == akMessage:
			g.value(kind, t, f, expr)
		case f.Optional:
			g.value(kind, t, f, "(*"+expr+")")
		case kind == akBytes && f.EmitZero:
			p.P("if ", expr, " == nil {")
			p.P("b = append(b, `\"\"`...)")
			p.P("} else {")
			g.value(kind, t, f, expr)
			p.P("}")
		default:
			g.value(kind, t, f, expr)
		}
	}
}

func (g *appendGen) mapBody(f *onkir.Field, expr string) {
	p := g.p
	t := f.Type
	valueKind := p.appendKindOf(t.MapValue, nil, false)
	if t.MapKey != onkir.ScalarString {
		p.P("if ", expr, " == nil {")
		p.P(`b = append(b, "{}"...)`)
		p.P("} else {")
		p.P("if b, err = onkAppendStd(b, ", expr, "); err != nil {")
		p.P("return b, err")
		p.P("}")
		p.P("}")
		return
	}
	p.P("{")
	p.P("keys := onkSortedKeys(", expr, ")")
	p.P("b = append(b, '{')")
	p.P("for i, k := range keys {")
	p.P("if i > 0 {")
	p.P("b = append(b, ',')")
	p.P("}")
	p.P("b = onkAppendString(b, k)")
	p.P("b = append(b, ':')")
	g.value(valueKind, t.MapValue, nil, expr+"[k]")
	p.P("}")
	p.P("b = append(b, '}')")
	p.P("}")
}

func (g *appendGen) oneofBody(f *onkir.Field, expr string) {
	p, m := g.p, g.m
	disc := oneofDiscriminatorName(f)
	p.P("switch v := ", expr, ".(type) {")
	for _, variant := range f.Oneof.Variants {
		kind := p.appendKindOf(variant.Type, nil, true)
		vName := PascalCase(variant.Name)
		value := "v." + vName
		p.P("case *", OneofVariantTypeName(m, f, variant), ":")
		p.P("b = append(b, ", strconv.Quote(`{"`+disc+`":`+strconv.Quote(variant.Tag())), "...)")
		if kind == akMessage {
			p.P("if ", value, " != nil {")
			p.P("b = append(b, ", strconv.Quote(`,"`+variant.Name+`":`), "...)")
			g.value(kind, variant.Type, nil, value)
			p.P("}")
		} else {
			p.P("b = append(b, ", strconv.Quote(`,"`+variant.Name+`":`), "...)")
			g.value(kind, variant.Type, nil, value)
		}
		p.P("b = append(b, '}')")
	}
	p.P("default:")
	p.P(`b = append(b, "null"...)`)
	p.P("}")
}

func (g *appendGen) value(kind appendKind, t *onkir.Type, f *onkir.Field, expr string) {
	p := g.p
	switch kind {
	case akString:
		p.P("b = onkAppendString(b, ", expr, ")")
	case akBool:
		p.P("b = strconv.AppendBool(b, ", expr, ")")
	case akInt, akEnumNumber:
		p.P("b = strconv.AppendInt(b, int64(", expr, "), 10)")
	case akUint:
		p.P("b = strconv.AppendUint(b, uint64(", expr, "), 10)")
	case akIntString:
		p.P("b = append(b, '\"')")
		p.P("b = strconv.AppendInt(b, int64(", expr, "), 10)")
		p.P("b = append(b, '\"')")
	case akUintString:
		p.P("b = append(b, '\"')")
		p.P("b = strconv.AppendUint(b, uint64(", expr, "), 10)")
		p.P("b = append(b, '\"')")
	case akFloat32:
		p.P("if b, err = onkAppendFloat(b, float64(", expr, "), 32); err != nil {")
		p.P("return b, err")
		p.P("}")
	case akFloat64:
		p.P("if b, err = onkAppendFloat(b, float64(", expr, "), 64); err != nil {")
		p.P("return b, err")
		p.P("}")
	case akEnum:
		if p.isExternalEnum(t.Enum) {
			g.std(expr)
			return
		}
		p.P("if !", expr, ".IsValid() {")
		p.P("return b, onkEnumInvalid(", strconv.Quote(t.Enum.Name), ", int32(", expr, "))")
		p.P("}")
		p.P("b = onkAppendString(b, ", expr, ".String())")
	case akBytes:
		p.P("b = onkAppendBytes(b, ", expr, ")")
	case akBytesEncoded:
		p.P("b = append(b, '\"')")
		switch bytesEncodingValue(f) {
		case bytesEncodeHex:
			p.P("b = hex.AppendEncode(b, ", expr, ")")
		case bytesEncodeBase64Raw:
			p.P("b = base64.RawStdEncoding.AppendEncode(b, ", expr, ")")
		case bytesEncodeBase64URL:
			p.P("b = base64.URLEncoding.AppendEncode(b, ", expr, ")")
		case bytesEncodeBase64URLRaw:
			p.P("b = base64.RawURLEncoding.AppendEncode(b, ", expr, ")")
		}
		p.P("b = append(b, '\"')")
	case akTime:
		p.P("b = append(b, '\"')")
		p.P("if b, err = ", expr, ".AppendText(b); err != nil {")
		p.P("return b, err")
		p.P("}")
		p.P("b = append(b, '\"')")
	case akTimeEncoded:
		switch timestampEncodingValue(f) {
		case timestampEncodeUnixSeconds:
			p.P("b = strconv.AppendInt(b, ", expr, ".Unix(), 10)")
		case timestampEncodeUnixMillis:
			p.P("b = strconv.AppendInt(b, ", expr, ".UnixMilli(), 10)")
		case timestampEncodeDate:
			p.P("b = append(b, '\"')")
			p.P("b = ", expr, `.AppendFormat(b, "2006-01-02")`)
			p.P("b = append(b, '\"')")
		}
	case akRaw:
		p.P("if b, err = onkAppendRaw(b, ", expr, "); err != nil {")
		p.P("return b, err")
		p.P("}")
	case akMessage:
		if p.hasAppend(t.Message) {
			p.P("if b, err = ", expr, ".AppendJSON(b); err != nil {")
			p.P("return b, err")
			p.P("}")
			return
		}
		p.P("if b, err = onkAppendValue(b, ", expr, "); err != nil {")
		p.P("return b, err")
		p.P("}")
	}
}

func (g *appendGen) std(expr string) {
	p := g.p
	p.P("if b, err = onkAppendStd(b, ", expr, "); err != nil {")
	p.P("return b, err")
	p.P("}")
}
