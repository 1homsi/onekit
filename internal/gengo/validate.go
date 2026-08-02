package gengo

import (
	"fmt"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

func isNumericScalar(k onkir.ScalarKind) bool {
	switch k {
	case onkir.ScalarInt32, onkir.ScalarInt64, onkir.ScalarUint32, onkir.ScalarUint64,
		onkir.ScalarFloat32, onkir.ScalarFloat64:
		return true
	case onkir.ScalarString, onkir.ScalarBool, onkir.ScalarBytes, onkir.ScalarTimestamp:
		return false
	default:
		return false
	}
}

func fieldAccessor(f *onkir.Field) string {
	goName := PascalCase(f.Name)
	if f.Optional && f.Type != nil && f.Type.Kind == onkir.KindScalar {
		return "*m." + goName
	}
	return "m." + goName
}

func requiredRule(f *onkir.Field) []string {
	if !f.HasDecorator("required") {
		return nil
	}
	goName := PascalCase(f.Name)
	requiredMsg := f.Name + " is required"
	switch {
	case f.Optional, f.Type != nil && f.Type.Kind == onkir.KindMessage:
		return []string{fmt.Sprintf(
			"if m.%s == nil { violations = append(violations, %q) }", goName, requiredMsg,
		)}
	case f.Type != nil && f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarString && !f.Repeated:
		return []string{fmt.Sprintf(
			`if %s == "" { violations = append(violations, %q) }`, fieldAccessor(f), requiredMsg,
		)}
	case f.Repeated, f.Type != nil && f.Type.Kind == onkir.KindMap:
		return []string{fmt.Sprintf(
			"if len(m.%s) == 0 { violations = append(violations, %q) }", goName, requiredMsg,
		)}
	default:
		return nil
	}
}

func repeatedItemsRules(f *onkir.Field) []string {
	if !f.Repeated {
		return nil
	}
	goName := PascalCase(f.Name)
	var rules []string
	if d, ok := f.Decorator("min_items"); ok {
		n, _ := d.Value()
		rules = append(rules, fmt.Sprintf(
			"if len(m.%s) < %s { violations = append(violations, %q) }", goName, n,
			f.Name+" must have at least "+n+" items",
		))
	}
	if d, ok := f.Decorator("max_items"); ok {
		n, _ := d.Value()
		rules = append(rules, fmt.Sprintf(
			"if len(m.%s) > %s { violations = append(violations, %q) }", goName, n,
			f.Name+" must have at most "+n+" items",
		))
	}
	return rules
}

func stringValidationRules(m *onkir.Message, f *onkir.Field) []string {
	accessor := fieldAccessor(f)
	var rules []string

	if f.HasDecorator("email") {
		rules = append(rules, fmt.Sprintf(
			`if %s != "" && !emailPattern.MatchString(%s) { violations = append(violations, %q) }`,
			accessor, accessor, f.Name+" must be a valid email",
		))
	}
	if f.HasDecorator("uuid") {
		rules = append(rules, fmt.Sprintf(
			`if %s != "" && !uuidPattern.MatchString(%s) { violations = append(violations, %q) }`,
			accessor, accessor, f.Name+" must be a valid uuid",
		))
	}
	if f.HasDecorator("uri") {
		rules = append(rules, fmt.Sprintf(
			`if %s != "" && !uriPattern.MatchString(%s) { violations = append(violations, %q) }`,
			accessor, accessor, f.Name+" must be a valid uri",
		))
	}
	if _, ok := f.Decorator("pattern"); ok {
		rules = append(rules, fmt.Sprintf(
			`if %s != "" && !%s.MatchString(%s) { violations = append(violations, %q) }`,
			accessor, patternVarName(m, f), accessor, f.Name+" has an invalid format",
		))
	}
	if d, ok := f.Decorator("len"); ok {
		minArg, _ := d.Arg(0)
		maxArg, _ := d.Arg(1)
		rules = append(rules, fmt.Sprintf(
			"if len(%s) < %s || len(%s) > %s { violations = append(violations, %q) }",
			accessor, minArg, accessor, maxArg,
			fmt.Sprintf("%s must be between %s and %s characters", f.Name, minArg, maxArg),
		))
	}
	if d, ok := f.Decorator("in"); ok {
		var quoted []string
		for _, a := range d.Args {
			quoted = append(quoted, fmt.Sprintf("%q", a.Value))
		}
		rules = append(rules, fmt.Sprintf(
			"if !inSet(%s, %s) { violations = append(violations, %q) }",
			accessor, strings.Join(quoted, ", "), f.Name+" must be one of the allowed values",
		))
	}

	return rules
}

func numericValidationRules(f *onkir.Field) []string {
	accessor := fieldAccessor(f)
	var rules []string

	if d, ok := f.Decorator("range"); ok {
		minArg, _ := d.Arg(0)
		maxArg, _ := d.Arg(1)
		rules = append(rules, fmt.Sprintf(
			"if %s < %s || %s > %s { violations = append(violations, %q) }",
			accessor, minArg, accessor, maxArg,
			fmt.Sprintf("%s must be between %s and %s", f.Name, minArg, maxArg),
		))
	}
	if d, ok := f.Decorator("gt"); ok {
		n, _ := d.Value()
		rules = append(rules, fmt.Sprintf(
			"if %s <= %s { violations = append(violations, %q) }",
			accessor, n, f.Name+" must be greater than "+n,
		))
	}
	if d, ok := f.Decorator("gte"); ok {
		n, _ := d.Value()
		rules = append(rules, fmt.Sprintf(
			"if %s < %s { violations = append(violations, %q) }",
			accessor, n, f.Name+" must be greater than or equal to "+n,
		))
	}
	if d, ok := f.Decorator("lt"); ok {
		n, _ := d.Value()
		rules = append(rules, fmt.Sprintf(
			"if %s >= %s { violations = append(violations, %q) }",
			accessor, n, f.Name+" must be less than "+n,
		))
	}
	if d, ok := f.Decorator("lte"); ok {
		n, _ := d.Value()
		rules = append(rules, fmt.Sprintf(
			"if %s > %s { violations = append(violations, %q) }",
			accessor, n, f.Name+" must be less than or equal to "+n,
		))
	}

	return rules
}

func fieldValidationRules(m *onkir.Message, f *onkir.Field) []string {
	required := requiredRule(f)
	var valueRules []string
	valueRules = append(valueRules, repeatedItemsRules(f)...)

	if f.Type == nil || f.Type.Kind != onkir.KindScalar || f.Repeated {
		return dedupeEmpty(append(required, valueRules...))
	}

	if f.Type.Scalar == onkir.ScalarString {
		valueRules = append(valueRules, stringValidationRules(m, f)...)
	} else if isNumericScalar(f.Type.Scalar) {
		valueRules = append(valueRules, numericValidationRules(f)...)
	}
	if f.Optional && len(valueRules) > 0 {
		guard := "m." + PascalCase(f.Name) + " != nil"
		for index, rule := range valueRules {
			valueRules[index] = "if " + guard + " { " + rule + " }"
		}
	}

	return dedupeEmpty(append(required, valueRules...))
}

func dedupeEmpty(rules []string) []string {
	if len(rules) == 0 {
		return nil
	}
	return rules
}

// patternVarName is the package-level regexp variable generated for a field's
// @pattern decorator, compiled once rather than on every Validate() call.
// Scoped by message name too, not just field name, since two different
// messages can have a same-named field with different patterns.
func patternVarName(m *onkir.Message, f *onkir.Field) string {
	return PascalCase(m.Name) + PascalCase(f.Name) + "Pattern"
}

type patternDecl struct {
	varName string
	pattern string
}

// collectPatternDecls walks every message (including nested) for @pattern
// decorators and returns the distinct set of package-level regexp vars
// GenerateValidation needs to declare, sorted for stable output.
func collectPatternDecls(file *onkir.File) []patternDecl {
	seen := map[string]bool{}
	var decls []patternDecl

	var walk func(m *onkir.Message)
	walk = func(m *onkir.Message) {
		for _, f := range m.Fields {
			if d, ok := f.Decorator("pattern"); ok {
				name := patternVarName(m, f)
				if !seen[name] {
					seen[name] = true
					pattern, _ := d.Value()
					decls = append(decls, patternDecl{varName: name, pattern: pattern})
				}
			}
		}
		for _, nested := range m.Nested {
			walk(nested)
		}
	}
	for _, m := range file.Messages {
		walk(m)
	}

	sort.Slice(decls, func(i, j int) bool { return decls[i].varName < decls[j].varName })
	return decls
}

func GenerateValidation(file *onkir.File) ([]byte, error) {
	if len(file.Messages) == 0 {
		return nil, nil
	}

	p := &Printer{}
	p.P("// Code generated by onek. DO NOT EDIT.")
	p.P("package ", GoPackageName(file))
	p.P()
	p.P(`import (`)
	p.P(`"errors"`)
	p.P(`"regexp"`)
	p.P(`"strings"`)
	p.P(")")
	p.P()
	p.P(`var emailPattern = regexp.MustCompile(` + "`" + `^[^@\s]+@[^@\s]+\.[^@\s]+$` + "`" + `)`)
	uuidRegex := `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-` +
		`[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`
	p.P(`var uuidPattern = regexp.MustCompile(` + "`" + uuidRegex + "`" + `)`)
	uriRegex := `^[a-zA-Z][a-zA-Z0-9+.-]*://[^\s]+$`
	p.P(`var uriPattern = regexp.MustCompile(` + "`" + uriRegex + "`" + `)`)
	for _, decl := range collectPatternDecls(file) {
		p.P("var ", decl.varName, " = regexp.MustCompile(", fmt.Sprintf("%q", decl.pattern), ")")
	}
	p.P()
	p.P("func inSet(v string, allowed ...string) bool {")
	p.P("for _, a := range allowed {")
	p.P("if v == a {")
	p.P("return true")
	p.P("}")
	p.P("}")
	p.P("return false")
	p.P("}")
	p.P()

	for _, m := range file.Messages {
		writeValidate(p, m)
	}

	return p.Format()
}

func writeValidate(p *Printer, m *onkir.Message) {
	p.P("func (m *", m.Name, ") Validate() error {")
	p.P("if m == nil { return nil }")
	p.P("var violations []string")
	for _, f := range m.Fields {
		for _, rule := range fieldValidationRules(m, f) {
			p.P(rule)
		}
		writeNestedValidation(p, f)
	}
	p.P("if len(violations) > 0 {")
	p.P(`return errors.New(strings.Join(violations, "; "))`)
	p.P("}")
	p.P("return nil")
	p.P("}")
	p.P()
	for _, nested := range m.Nested {
		writeValidate(p, nested)
	}
}

func writeNestedValidation(p *Printer, field *onkir.Field) {
	if field.Type == nil {
		return
	}
	goName := PascalCase(field.Name)
	appendError := func(expression string) {
		p.P("if err := ", expression, ".Validate(); err != nil { violations = append(violations, ", fmt.Sprintf("%q", field.Name+": "), "+err.Error()) }")
	}
	switch {
	case field.Repeated && field.Type.Kind == onkir.KindMessage:
		p.P("for _, value := range m.", goName, " {")
		appendError("value")
		p.P("}")
	case field.Type.Kind == onkir.KindMap && field.Type.MapValue != nil && field.Type.MapValue.Kind == onkir.KindMessage:
		p.P("for _, value := range m.", goName, " {")
		appendError("value")
		p.P("}")
	case field.Type.Kind == onkir.KindMessage:
		p.P("if m.", goName, " != nil {")
		appendError("m." + goName)
		p.P("}")
	}
}
