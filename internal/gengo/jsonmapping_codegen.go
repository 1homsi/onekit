package gengo

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

type fieldCategories struct {
	oneofs     []*onkir.Field
	int64s     []*onkir.Field
	int64Reps  []*onkir.Field
	int64Opts  []*onkir.Field
	enums      []*onkir.Field
	bytesF     []*onkir.Field
	timestamps []*onkir.Field
	flattens   []*onkir.Field
	emptys     []*onkir.Field
	// zeroCollections are EmitZero repeated, map and plain bytes fields whose nil
	// form would marshal as JSON null; the marshal aux struct writes [], {} or
	// "" for them instead.
	zeroCollections []*onkir.Field
	nulls           []*onkir.Field
}

func (c fieldCategories) needsUnmarshal() bool {
	return len(c.oneofs)+len(c.int64s)+len(c.int64Reps)+len(c.int64Opts)+len(c.enums)+
		len(c.bytesF)+len(c.timestamps)+len(c.flattens)+len(c.emptys)+len(c.nulls) > 0
}

func zeroCollectionField(f *onkir.Field) bool {
	if !f.EmitZero || f.Oneof != nil {
		return false
	}
	switch {
	case f.Repeated, f.Type.Kind == onkir.KindMap:
		return true
	case f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarBytes:
		return true
	}
	return false
}

func fieldTagOptions(f *onkir.Field) string {
	if f.EmitZero {
		return ""
	}
	return ",omitempty"
}

const goStringType = "string"

func categorizeFields(m *onkir.Message) fieldCategories {
	var c fieldCategories
	for _, f := range m.Fields {
		if f.Nullable {
			c.nulls = append(c.nulls, f)
		}
		switch {
		case f.Oneof != nil:
			c.oneofs = append(c.oneofs, f)
		case needsInt64StringEncoding(f) && f.Optional:
			c.int64Opts = append(c.int64Opts, f)
		case needsInt64StringEncoding(f) && f.Repeated:
			c.int64Reps = append(c.int64Reps, f)
		case needsInt64StringEncoding(f):
			c.int64s = append(c.int64s, f)
		case needsEnumNumberEncoding(f):
			c.enums = append(c.enums, f)
		case bytesEncodingValue(f) != "":
			c.bytesF = append(c.bytesF, f)
		case timestampEncodingValue(f) != "":
			c.timestamps = append(c.timestamps, f)
		case zeroCollectionField(f):
			c.zeroCollections = append(c.zeroCollections, f)
		}
		if _, ok := flattenPrefix(f); ok {
			c.flattens = append(c.flattens, f)
		}
		if v := emptyBehaviorValue(f); v != "" && v != emptyBehaviorPreserve {
			c.emptys = append(c.emptys, f)
		}
	}
	return c
}

func writeCustomJSONMethods(p *Printer, m *onkir.Message) {
	c := categorizeFields(m)
	writeCustomMarshalJSON(p, m, c)
	if c.needsUnmarshal() {
		writeCustomUnmarshalJSON(p, m, c)
	}
}

func timestampEncodeExpr(encoding, expr string) string {
	switch encoding {
	case timestampEncodeUnixSeconds:
		return expr + ".Unix()"
	case timestampEncodeUnixMillis:
		return expr + ".UnixMilli()"
	case timestampEncodeDate:
		return expr + `.Format("2006-01-02")`
	default:
		return expr
	}
}

func timestampAuxType(encoding string) string {
	if encoding == timestampEncodeDate {
		return goStringType
	}
	return "int64"
}

func int64FormatCall(kind onkir.ScalarKind, expr string) string {
	if kind == onkir.ScalarUint64 {
		return fmt.Sprintf("strconv.FormatUint(%s, 10)", expr)
	}
	return fmt.Sprintf("strconv.FormatInt(%s, 10)", expr)
}

func int64ParseCall(kind onkir.ScalarKind, expr string) string {
	if kind == onkir.ScalarUint64 {
		return fmt.Sprintf("strconv.ParseUint(%s, 10, 64)", expr)
	}
	return fmt.Sprintf("strconv.ParseInt(%s, 10, 64)", expr)
}

func bytesEncodeCall(encoding, expr string) string {
	switch encoding {
	case bytesEncodeHex:
		return fmt.Sprintf("hex.EncodeToString(%s)", expr)
	case bytesEncodeBase64Raw:
		return fmt.Sprintf("base64.RawStdEncoding.EncodeToString(%s)", expr)
	case bytesEncodeBase64URL:
		return fmt.Sprintf("base64.URLEncoding.EncodeToString(%s)", expr)
	case bytesEncodeBase64URLRaw:
		return fmt.Sprintf("base64.RawURLEncoding.EncodeToString(%s)", expr)
	default:
		return expr
	}
}

func bytesDecodeCall(encoding, expr string) string {
	switch encoding {
	case bytesEncodeHex:
		return fmt.Sprintf("hex.DecodeString(%s)", expr)
	case bytesEncodeBase64Raw:
		return fmt.Sprintf("base64.RawStdEncoding.DecodeString(%s)", expr)
	case bytesEncodeBase64URL:
		return fmt.Sprintf("base64.URLEncoding.DecodeString(%s)", expr)
	case bytesEncodeBase64URLRaw:
		return fmt.Sprintf("base64.RawURLEncoding.DecodeString(%s)", expr)
	default:
		return expr
	}
}

// writeAuxFieldDecls emits the override-field declarations shared by the
// marshal and unmarshal aux structs. includeEmpty is false for unmarshal:
// empty-behavior only affects the marshal side (see emptyBehaviorValue).
func writeAuxFieldDecls(p *Printer, m *onkir.Message, c fieldCategories, includeEmpty bool) {
	for _, f := range c.oneofs {
		if f.Oneof.Flatten() {
			p.P(GoFieldName(f), " json.RawMessage `json:\"", f.Name, ",omitempty\"`")
			continue
		}
		p.P(GoFieldName(f), " *", oneofWireName(m, f), " `json:\"", f.Name, ",omitempty\"`")
	}
	for _, f := range c.int64s {
		p.P(GoFieldName(f), " string `json:\"", f.Name, fieldTagOptions(f), "\"`")
	}
	for _, f := range c.int64Opts {
		p.P(GoFieldName(f), " *string `json:\"", f.Name, ",omitempty\"`")
	}
	for _, f := range c.int64Reps {
		p.P(GoFieldName(f), " []string `json:\"", f.Name, fieldTagOptions(f), "\"`")
	}
	for _, f := range c.enums {
		if f.Optional {
			p.P(GoFieldName(f), " *int32 `json:\"", f.Name, ",omitempty\"`")
			continue
		}
		// Non-optional number-encoded enums must not be omitempty: enum
		// member 0 is a legitimate wire value and would otherwise vanish.
		p.P(GoFieldName(f), " int32 `json:\"", f.Name, "\"`")
	}
	for _, f := range c.bytesF {
		auxType := "string"
		if f.Optional {
			auxType = "*string"
		}
		p.P(GoFieldName(f), " ", auxType, " `json:\"", f.Name, fieldTagOptions(f), "\"`")
	}
	for _, f := range c.timestamps {
		auxType := timestampAuxType(timestampEncodingValue(f))
		if f.Optional {
			p.P(GoFieldName(f), " *", auxType, " `json:\"", f.Name, ",omitempty\"`")
			continue
		}
		// Non-optional unix encodings must not be omitempty: epoch zero is
		// a legitimate wire value and would otherwise vanish.
		p.P(GoFieldName(f), " ", auxType, " `json:\"", f.Name, "\"`")
	}
	for _, f := range c.flattens {
		p.P(GoFieldName(f), " json.RawMessage `json:\"", f.Name, ",omitempty\"`")
	}
	if includeEmpty {
		for _, f := range c.emptys {
			p.P(GoFieldName(f), " json.RawMessage `json:\"", f.Name, ",omitempty\"`")
		}
		for _, f := range c.zeroCollections {
			p.P(GoFieldName(f), " ", zeroCollectionType(p, f), " `json:\"", f.Name, "\"`")
		}
	}
}

func zeroCollectionType(p *Printer, f *onkir.Field) string {
	if f.Repeated {
		return "[]" + p.GoFieldType(f.Type)
	}
	return p.GoFieldType(f.Type)
}

func writeZeroCollectionAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.zeroCollections {
		goName := GoFieldName(f)
		typ := zeroCollectionType(p, f)
		p.P("aux.", goName, " = m.", goName)
		p.P("if aux.", goName, " == nil {")
		p.P("aux.", goName, " = ", typ, "{}")
		p.P("}")
	}
}

func writeInt64MarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.int64s {
		goName := GoFieldName(f)
		if f.Type.Scalar == onkir.ScalarUint64 {
			p.P("aux.", goName, " = strconv.FormatUint(m.", goName, ", 10)")
		} else {
			p.P("aux.", goName, " = strconv.FormatInt(m.", goName, ", 10)")
		}
	}
	for _, f := range c.int64Opts {
		goName := GoFieldName(f)
		p.P("if m.", goName, " != nil {")
		p.P("encoded", goName, " := ", int64FormatCall(f.Type.Scalar, "*m."+goName))
		p.P("aux.", goName, " = &encoded", goName)
		p.P("}")
	}
	for _, f := range c.int64Reps {
		goName := GoFieldName(f)
		p.P("aux.", goName, " = make([]string, len(m.", goName, "))")
		p.P("for i, v := range m.", goName, " {")
		if f.Type.Scalar == onkir.ScalarUint64 {
			p.P("aux.", goName, "[i] = strconv.FormatUint(v, 10)")
		} else {
			p.P("aux.", goName, "[i] = strconv.FormatInt(v, 10)")
		}
		p.P("}")
	}
}

func writeEnumMarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.enums {
		goName := GoFieldName(f)
		if f.Optional {
			p.P("if m.", goName, " != nil {")
			p.P("v := int32(*m.", goName, ")")
			p.P("aux.", goName, " = &v")
			p.P("}")
			continue
		}
		p.P("aux.", goName, " = int32(m.", goName, ")")
	}
}

func writeBytesMarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.bytesF {
		goName := GoFieldName(f)
		if f.Optional {
			p.P("if m.", goName, " != nil {")
			p.P("encoded", goName, " := ", bytesEncodeCall(bytesEncodingValue(f), "*m."+goName))
			p.P("aux.", goName, " = &encoded", goName)
			p.P("}")
			continue
		}
		p.P("aux.", goName, " = ", bytesEncodeCall(bytesEncodingValue(f), "m."+goName))
	}
}

func writeTimestampMarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.timestamps {
		goName := GoFieldName(f)
		if f.Optional {
			p.P("if m.", goName, " != nil {")
			p.P("encoded", goName, " := ", timestampEncodeExpr(timestampEncodingValue(f), "m."+goName))
			p.P("aux.", goName, " = &encoded", goName)
			p.P("}")
			continue
		}
		p.P("aux.", goName, " = ", timestampEncodeExpr(timestampEncodingValue(f), "m."+goName))
	}
}

func writeEmptyMarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.emptys {
		writeEmptyMarshalField(p, f, emptyBehaviorValue(f))
	}
}

func writeFlattenMarshalMerge(p *Printer, c fieldCategories) {
	p.P("base, err := json.Marshal(aux)")
	p.P("if err != nil {")
	p.P("return nil, err")
	p.P("}")
	p.P("var merged map[string]json.RawMessage")
	p.P("if err := json.Unmarshal(base, &merged); err != nil {")
	p.P("return nil, err")
	p.P("}")
	for _, f := range c.flattens {
		goName := GoFieldName(f)
		prefix, _ := flattenPrefix(f)
		p.P("if m.", goName, " != nil {")
		p.P("childBytes, err := json.Marshal(m.", goName, ")")
		p.P("if err != nil {")
		p.P("return nil, err")
		p.P("}")
		p.P("var childMap map[string]json.RawMessage")
		p.P("if err := json.Unmarshal(childBytes, &childMap); err != nil {")
		p.P("return nil, err")
		p.P("}")
		p.P("for k, v := range childMap {")
		p.P("merged[", fmt.Sprintf("%q", prefix), "+k] = v")
		p.P("}")
		p.P("}")
	}
	writeNullMarshalAssignments(p, c)
	p.P("return json.Marshal(merged)")
}

func writeCustomMarshalJSON(p *Printer, m *onkir.Message, c fieldCategories) {
	p.P("func (m *", m.Name, ") MarshalJSON() ([]byte, error) {")
	p.P("type alias ", m.Name)
	p.P("aux := struct {")
	p.P("*alias")
	writeAuxFieldDecls(p, m, c, true)
	p.P("}{alias: (*alias)(m)}")

	for _, f := range c.oneofs {
		writeOneofMarshalField(p, m, f)
	}
	writeInt64MarshalAssignments(p, c)
	writeEnumMarshalAssignments(p, c)
	writeBytesMarshalAssignments(p, c)
	writeTimestampMarshalAssignments(p, c)
	writeEmptyMarshalAssignments(p, c)
	writeZeroCollectionAssignments(p, c)

	if len(c.flattens) == 0 && len(c.nulls) == 0 {
		p.P("return json.Marshal(aux)")
		p.P("}")
		p.P()
		return
	}

	if len(c.flattens) == 0 {
		writeNullMarshalMerge(p, c)
		p.P("}")
		p.P()
		return
	}

	writeFlattenMarshalMerge(p, c)
	p.P("}")
	p.P()
}

func nullPendingCondition(c fieldCategories) string {
	conds := make([]string, 0, len(c.nulls))
	for _, f := range c.nulls {
		goName := GoFieldName(f)
		conds = append(conds, "(m."+goName+"Null && m."+goName+" == nil)")
	}
	return strings.Join(conds, " || ")
}

func writeNullMarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.nulls {
		goName := GoFieldName(f)
		p.P("if m.", goName, "Null && m.", goName, " == nil {")
		p.P("merged[", fmt.Sprintf("%q", f.Name), `] = json.RawMessage("null")`)
		p.P("}")
	}
}

func writeNullMarshalMerge(p *Printer, c fieldCategories) {
	p.P("base, err := json.Marshal(aux)")
	p.P("if err != nil {")
	p.P("return nil, err")
	p.P("}")
	p.P("if !(", nullPendingCondition(c), ") {")
	p.P("return base, nil")
	p.P("}")
	p.P("var merged map[string]json.RawMessage")
	p.P("if err := json.Unmarshal(base, &merged); err != nil {")
	p.P("return nil, err")
	p.P("}")
	writeNullMarshalAssignments(p, c)
	p.P("return json.Marshal(merged)")
}

func writeEmptyMarshalField(p *Printer, f *onkir.Field, behavior string) {
	goName := GoFieldName(f)
	nullLiteral := `json.RawMessage("null")`
	varName := CamelCase(f.Name) + "Bytes"

	p.P("if m.", goName, " != nil {")
	p.P(varName, ", err := json.Marshal(m.", goName, ")")
	p.P("if err != nil {")
	p.P("return nil, err")
	p.P("}")
	p.P(`if string(`, varName, `) == "{}" {`)
	if behavior == emptyBehaviorNull {
		p.P("aux.", goName, " = ", nullLiteral)
	}
	p.P("} else {")
	p.P("aux.", goName, " = ", varName)
	p.P("}")
	p.P("} else {")
	if behavior == emptyBehaviorNull {
		p.P("aux.", goName, " = ", nullLiteral)
	}
	p.P("}")
}

func writeInt64UnmarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.int64s {
		goName := GoFieldName(f)
		p.P("if aux.", goName, " != \"\" {")
		if f.Type.Scalar == onkir.ScalarUint64 {
			p.P("v, err := strconv.ParseUint(aux.", goName, ", 10, 64)")
		} else {
			p.P("v, err := strconv.ParseInt(aux.", goName, ", 10, 64)")
		}
		p.P("if err != nil {")
		p.P("return err")
		p.P("}")
		p.P("m.", goName, " = v")
		p.P("}")
	}
	for _, f := range c.int64Opts {
		goName := GoFieldName(f)
		p.P("if aux.", goName, " != nil {")
		p.P("v, err := ", int64ParseCall(f.Type.Scalar, "*aux."+goName))
		p.P("if err != nil {")
		p.P("return err")
		p.P("}")
		p.P("m.", goName, " = &v")
		p.P("}")
	}
	for _, f := range c.int64Reps {
		goName := GoFieldName(f)
		p.P("if aux.", goName, " != nil {")
		p.P("m.", goName, " = make([]", p.GoFieldType(f.Type), ", len(aux.", goName, "))")
		p.P("for i, s := range aux.", goName, " {")
		if f.Type.Scalar == onkir.ScalarUint64 {
			p.P("v, err := strconv.ParseUint(s, 10, 64)")
		} else {
			p.P("v, err := strconv.ParseInt(s, 10, 64)")
		}
		p.P("if err != nil {")
		p.P("return err")
		p.P("}")
		p.P("m.", goName, "[i] = v")
		p.P("}")
		p.P("}")
	}
}

func writeEnumUnmarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.enums {
		goName := GoFieldName(f)
		if f.Optional {
			p.P("if aux.", goName, " != nil {")
			p.P("v := ", p.GoFieldType(f.Type), "(*aux.", goName, ")")
			p.P("m.", goName, " = &v")
			p.P("}")
			continue
		}
		p.P("m.", goName, " = ", p.GoFieldType(f.Type), "(aux.", goName, ")")
	}
}

func writeBytesUnmarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.bytesF {
		goName := GoFieldName(f)
		valueExpr := "aux." + goName
		if f.Optional {
			p.P("if aux.", goName, " != nil {")
			valueExpr = "*aux." + goName
		} else {
			p.P("if aux.", goName, " != \"\" {")
		}
		p.P("decoded, err := ", bytesDecodeCall(bytesEncodingValue(f), valueExpr))
		p.P("if err != nil {")
		p.P("return err")
		p.P("}")
		if f.Optional {
			p.P("m.", goName, " = &decoded")
		} else {
			p.P("m.", goName, " = decoded")
		}
		p.P("}")
	}
}

func writeTimestampUnmarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.timestamps {
		goName := GoFieldName(f)
		encoding := timestampEncodingValue(f)
		if f.Optional {
			p.P("if aux.", goName, " != nil {")
		}
		switch encoding {
		case timestampEncodeUnixSeconds:
			if f.Optional {
				p.P("value := time.Unix(*aux.", goName, ", 0).UTC()")
				p.P("m.", goName, " = &value")
			} else {
				// No zero guard: epoch zero (1970-01-01T00:00:00Z) is a
				// legitimate value and must round-trip like any other.
				p.P("m.", goName, " = time.Unix(aux.", goName, ", 0).UTC()")
			}
		case timestampEncodeUnixMillis:
			if f.Optional {
				p.P("value := time.UnixMilli(*aux.", goName, ").UTC()")
				p.P("m.", goName, " = &value")
			} else {
				p.P("m.", goName, " = time.UnixMilli(aux.", goName, ").UTC()")
			}
		case timestampEncodeDate:
			valueExpr := "aux." + goName
			if f.Optional {
				valueExpr = "*aux." + goName
			} else {
				p.P("if aux.", goName, " != \"\" {")
			}
			p.P("t, err := time.Parse(\"2006-01-02\", ", valueExpr, ")")
			p.P("if err != nil {")
			p.P("return err")
			p.P("}")
			if f.Optional {
				p.P("m.", goName, " = &t")
			} else {
				p.P("m.", goName, " = t")
				p.P("}")
			}
		}
		if f.Optional {
			p.P("}")
		}
	}
}

func writeRawObjectDecl(p *Printer) {
	p.P("var raw map[string]json.RawMessage")
	p.P("if err := json.Unmarshal(data, &raw); err != nil {")
	p.P("return err")
	p.P("}")
}

func writeNullUnmarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.nulls {
		goName := GoFieldName(f)
		p.P("if v, ok := raw[", fmt.Sprintf("%q", f.Name), `]; ok && string(v) == "null" {`)
		p.P("m.", goName, "Null = true")
		p.P("} else {")
		p.P("m.", goName, "Null = false")
		p.P("}")
	}
}

func writeFlattenUnmarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.flattens {
		goName := GoFieldName(f)
		prefix, _ := flattenPrefix(f)
		childType := p.GoFieldType(f.Type)
		p.P("{")
		p.P("childRaw := map[string]json.RawMessage{}")
		p.P("hasChild := false")
		p.P("for k, v := range raw {")
		p.P("if strings.HasPrefix(k, ", fmt.Sprintf("%q", prefix), ") {")
		p.P("childRaw[strings.TrimPrefix(k, ", fmt.Sprintf("%q", prefix), ")] = v")
		p.P("hasChild = true")
		p.P("}")
		p.P("}")
		p.P("if hasChild {")
		p.P("childBytes, err := json.Marshal(childRaw)")
		p.P("if err != nil {")
		p.P("return err")
		p.P("}")
		p.P("child := new(", childType[1:], ")") // strip leading "*"
		p.P("if err := json.Unmarshal(childBytes, child); err != nil {")
		p.P("return err")
		p.P("}")
		p.P("m.", goName, " = child")
		p.P("}")
		p.P("}")
	}
}

func writeCustomUnmarshalJSON(p *Printer, m *onkir.Message, c fieldCategories) {
	p.P("func (m *", m.Name, ") UnmarshalJSON(data []byte) error {")
	p.P("type alias ", m.Name)
	p.P("aux := struct {")
	p.P("*alias")
	writeAuxFieldDecls(p, m, c, false)
	p.P("}{alias: (*alias)(m)}")
	p.P("if err := json.Unmarshal(data, &aux); err != nil {")
	p.P("return err")
	p.P("}")

	for _, f := range c.oneofs {
		writeOneofUnmarshalField(p, m, f)
	}
	writeInt64UnmarshalAssignments(p, c)
	writeEnumUnmarshalAssignments(p, c)
	writeBytesUnmarshalAssignments(p, c)
	writeTimestampUnmarshalAssignments(p, c)

	if len(c.flattens) > 0 || len(c.nulls) > 0 {
		writeRawObjectDecl(p)
	}
	if len(c.flattens) > 0 {
		writeFlattenUnmarshalAssignments(p, c)
	}
	writeNullUnmarshalAssignments(p, c)

	p.P("return nil")
	p.P("}")
	p.P()
}

// writeRootUnwrapJSON handles a message whose entire JSON representation IS
// its single @unwrap field's value (an array or a map), not an object
// wrapping that field. Map-value unwrap (the other onk unwrap variant, where
// a message used as a map's value type collapses instead) isn't implemented -
// it would need the enclosing map field's own codegen to know about this
// message's internal shape.
func writeRootUnwrapJSON(p *Printer, m *onkir.Message, field *onkir.Field) {
	goName := GoFieldName(field)
	if needsInt64StringEncoding(field) && !field.Optional {
		writeRootUnwrapInt64JSON(p, m, field)
		return
	}
	p.P("func (m *", m.Name, ") MarshalJSON() ([]byte, error) {")
	if field.EmitZero && (field.Repeated || field.Type.Kind == onkir.KindMap) {
		empty := "[]"
		if !field.Repeated {
			empty = "{}"
		}
		p.P("if m.", goName, " == nil {")
		p.P("return []byte(", fmt.Sprintf("%q", empty), "), nil")
		p.P("}")
	}
	p.P("return json.Marshal(m.", goName, ")")
	p.P("}")
	p.P()
	p.P("func (m *", m.Name, ") UnmarshalJSON(data []byte) error {")
	p.P("return json.Unmarshal(data, &m.", goName, ")")
	p.P("}")
	p.P()
}

func writeRootUnwrapInt64JSON(p *Printer, m *onkir.Message, field *onkir.Field) {
	goName := GoFieldName(field)
	kind := field.Type.Scalar
	p.P("func (m *", m.Name, ") MarshalJSON() ([]byte, error) {")
	if field.Repeated {
		p.P("out := make([]string, len(m.", goName, "))")
		p.P("for i, v := range m.", goName, " {")
		p.P("out[i] = ", int64FormatCall(kind, "v"))
		p.P("}")
		p.P("return json.Marshal(out)")
	} else {
		p.P("return json.Marshal(", int64FormatCall(kind, "m."+goName), ")")
	}
	p.P("}")
	p.P()
	p.P("func (m *", m.Name, ") UnmarshalJSON(data []byte) error {")
	if field.Repeated {
		p.P("var aux []string")
		p.P("if err := json.Unmarshal(data, &aux); err != nil {")
		p.P("return err")
		p.P("}")
		p.P("m.", goName, " = make([]", p.GoFieldType(field.Type), ", len(aux))")
		p.P("for i, s := range aux {")
		p.P("v, err := ", int64ParseCall(kind, "s"))
		p.P("if err != nil {")
		p.P("return err")
		p.P("}")
		p.P("m.", goName, "[i] = v")
		p.P("}")
		p.P("return nil")
	} else {
		p.P("var aux string")
		p.P("if err := json.Unmarshal(data, &aux); err != nil {")
		p.P("return err")
		p.P("}")
		p.P("v, err := ", int64ParseCall(kind, "aux"))
		p.P("if err != nil {")
		p.P("return err")
		p.P("}")
		p.P("m.", goName, " = v")
		p.P("return nil")
	}
	p.P("}")
	p.P()
}
