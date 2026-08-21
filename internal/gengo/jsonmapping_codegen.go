package gengo

import (
	"fmt"

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
}

const goStringType = "string"

func categorizeFields(m *onkir.Message) fieldCategories {
	var c fieldCategories
	for _, f := range m.Fields {
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
	writeCustomUnmarshalJSON(p, m, c)
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
func writeAuxFieldDecls(p *Printer, c fieldCategories, includeEmpty bool) {
	for _, f := range c.oneofs {
		p.P(PascalCase(f.Name), " json.RawMessage `json:\"", f.Name, ",omitempty\"`")
	}
	for _, f := range c.int64s {
		p.P(PascalCase(f.Name), " string `json:\"", f.Name, ",omitempty\"`")
	}
	for _, f := range c.int64Opts {
		p.P(PascalCase(f.Name), " *string `json:\"", f.Name, ",omitempty\"`")
	}
	for _, f := range c.int64Reps {
		p.P(PascalCase(f.Name), " []string `json:\"", f.Name, ",omitempty\"`")
	}
	for _, f := range c.enums {
		if f.Optional {
			p.P(PascalCase(f.Name), " int32 `json:\"", f.Name, ",omitempty\"`")
			continue
		}
		// Non-optional number-encoded enums must not be omitempty: enum
		// member 0 is a legitimate wire value and would otherwise vanish.
		p.P(PascalCase(f.Name), " int32 `json:\"", f.Name, "\"`")
	}
	for _, f := range c.bytesF {
		auxType := "string"
		if f.Optional {
			auxType = "*string"
		}
		p.P(PascalCase(f.Name), " ", auxType, " `json:\"", f.Name, ",omitempty\"`")
	}
	for _, f := range c.timestamps {
		auxType := timestampAuxType(timestampEncodingValue(f))
		if f.Optional {
			p.P(PascalCase(f.Name), " *", auxType, " `json:\"", f.Name, ",omitempty\"`")
			continue
		}
		// Non-optional unix encodings must not be omitempty: epoch zero is
		// a legitimate wire value and would otherwise vanish.
		p.P(PascalCase(f.Name), " ", auxType, " `json:\"", f.Name, "\"`")
	}
	for _, f := range c.flattens {
		p.P(PascalCase(f.Name), " json.RawMessage `json:\"", f.Name, ",omitempty\"`")
	}
	if includeEmpty {
		for _, f := range c.emptys {
			p.P(PascalCase(f.Name), " json.RawMessage `json:\"", f.Name, ",omitempty\"`")
		}
	}
}

func writeInt64MarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.int64s {
		goName := PascalCase(f.Name)
		if f.Type.Scalar == onkir.ScalarUint64 {
			p.P("aux.", goName, " = strconv.FormatUint(m.", goName, ", 10)")
		} else {
			p.P("aux.", goName, " = strconv.FormatInt(m.", goName, ", 10)")
		}
	}
	for _, f := range c.int64Opts {
		goName := PascalCase(f.Name)
		p.P("if m.", goName, " != nil {")
		p.P("encoded", goName, " := ", int64FormatCall(f.Type.Scalar, "*m."+goName))
		p.P("aux.", goName, " = &encoded", goName)
		p.P("}")
	}
	for _, f := range c.int64Reps {
		goName := PascalCase(f.Name)
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
		goName := PascalCase(f.Name)
		p.P("aux.", goName, " = int32(m.", goName, ")")
	}
}

func writeBytesMarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.bytesF {
		goName := PascalCase(f.Name)
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
		goName := PascalCase(f.Name)
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
		goName := PascalCase(f.Name)
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
	p.P("return json.Marshal(merged)")
}

func writeCustomMarshalJSON(p *Printer, m *onkir.Message, c fieldCategories) {
	p.P("func (m *", m.Name, ") MarshalJSON() ([]byte, error) {")
	p.P("type alias ", m.Name)
	p.P("aux := struct {")
	p.P("*alias")
	writeAuxFieldDecls(p, c, true)
	p.P("}{alias: (*alias)(m)}")

	for _, f := range c.oneofs {
		writeOneofMarshalField(p, m, f)
	}
	writeInt64MarshalAssignments(p, c)
	writeEnumMarshalAssignments(p, c)
	writeBytesMarshalAssignments(p, c)
	writeTimestampMarshalAssignments(p, c)
	writeEmptyMarshalAssignments(p, c)

	if len(c.flattens) == 0 {
		p.P("return json.Marshal(aux)")
		p.P("}")
		p.P()
		return
	}

	writeFlattenMarshalMerge(p, c)
	p.P("}")
	p.P()
}

func writeEmptyMarshalField(p *Printer, f *onkir.Field, behavior string) {
	goName := PascalCase(f.Name)
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
		goName := PascalCase(f.Name)
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
		goName := PascalCase(f.Name)
		p.P("if aux.", goName, " != nil {")
		p.P("v, err := ", int64ParseCall(f.Type.Scalar, "*aux."+goName))
		p.P("if err != nil {")
		p.P("return err")
		p.P("}")
		p.P("m.", goName, " = &v")
		p.P("}")
	}
	for _, f := range c.int64Reps {
		goName := PascalCase(f.Name)
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
		goName := PascalCase(f.Name)
		p.P("m.", goName, " = ", p.GoFieldType(f.Type), "(aux.", goName, ")")
	}
}

func writeBytesUnmarshalAssignments(p *Printer, c fieldCategories) {
	for _, f := range c.bytesF {
		goName := PascalCase(f.Name)
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
		goName := PascalCase(f.Name)
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
			}
			p.P("t, err := time.Parse(\"2006-01-02\", ", valueExpr, ")")
			p.P("if err != nil {")
			p.P("return err")
			p.P("}")
			if f.Optional {
				p.P("m.", goName, " = &t")
			} else {
				p.P("m.", goName, " = t")
			}
		}
		if f.Optional {
			p.P("}")
		}
	}
}

func writeFlattenUnmarshalAssignments(p *Printer, c fieldCategories) {
	p.P("var raw map[string]json.RawMessage")
	p.P("if err := json.Unmarshal(data, &raw); err != nil {")
	p.P("return err")
	p.P("}")
	for _, f := range c.flattens {
		goName := PascalCase(f.Name)
		prefix, _ := flattenPrefix(f)
		childType := p.GoFieldType(f.Type)
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
	}
}

func writeCustomUnmarshalJSON(p *Printer, m *onkir.Message, c fieldCategories) {
	p.P("func (m *", m.Name, ") UnmarshalJSON(data []byte) error {")
	p.P("type alias ", m.Name)
	p.P("aux := struct {")
	p.P("*alias")
	writeAuxFieldDecls(p, c, false)
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

	if len(c.flattens) > 0 {
		writeFlattenUnmarshalAssignments(p, c)
	}

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
	goName := PascalCase(field.Name)
	p.P("func (m *", m.Name, ") MarshalJSON() ([]byte, error) {")
	p.P("return json.Marshal(m.", goName, ")")
	p.P("}")
	p.P()
	p.P("func (m *", m.Name, ") UnmarshalJSON(data []byte) error {")
	p.P("return json.Unmarshal(data, &m.", goName, ")")
	p.P("}")
	p.P()
}
