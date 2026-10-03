package gengo

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

const (
	ruleInt64   = "int64"
	ruleInt32   = "int32"
	ruleFloat64 = "float64"
)

type ruleFile struct {
	regexes []string
	used    bool
}

func (rf *ruleFile) regexVar(pattern string) string {
	full := `\A(?:` + pattern + `)\z`
	for i, existing := range rf.regexes {
		if existing == full {
			return fmt.Sprintf("onkRuleRe%d", i)
		}
	}
	rf.regexes = append(rf.regexes, full)
	return fmt.Sprintf("onkRuleRe%d", len(rf.regexes)-1)
}

type ruleCompiler struct {
	file  *ruleFile
	field *onkir.Field
	vars  []string
}

func messageRules(m *onkir.Message) ([]onkexpr.Rule, error) {
	return onkexpr.RulesFor(m)
}

func writeRuleChecks(p *Printer, rf *ruleFile, m *onkir.Message) error {
	rules, err := messageRules(m)
	if err != nil {
		return err
	}
	for _, r := range rules {
		c := &ruleCompiler{file: rf, field: r.Field}
		code := c.expr(r.Expr)
		rf.used = true
		p.P("if !onkRuleHolds(func() bool { return ", code, " }) { violations = append(violations, ", strconv.Quote(r.Message), ") }")
	}
	return nil
}

func scanRulesUsed(file *onkir.File) bool {
	var walk func(m *onkir.Message) bool
	walk = func(m *onkir.Message) bool {
		for _, d := range m.Decorators {
			if d.Name == onkexpr.RuleDecorator {
				return true
			}
		}
		for _, f := range m.Fields {
			for _, d := range f.Decorators {
				if d.Name == onkexpr.RuleDecorator {
					return true
				}
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

func canonicalGoType(t *onkexpr.Type) string {
	switch t.Kind {
	case onkexpr.KindBool:
		return "bool"
	case onkexpr.KindInt:
		return ruleInt64
	case onkexpr.KindDouble:
		return ruleFloat64
	case onkexpr.KindString:
		return goStringType
	case onkexpr.KindEnum:
		return ruleInt32
	case onkexpr.KindBytes, onkexpr.KindList, onkexpr.KindMap, onkexpr.KindMessage:
		return ""
	}
	return ""
}

func (c *ruleCompiler) expr(n onkexpr.Node) string {
	switch n := n.(type) {
	case *onkexpr.IntLit:
		return fmt.Sprintf(ruleInt64+"(%d)", n.Value)
	case *onkexpr.DoubleLit:
		return ruleFloat64 + "(" + strconv.FormatFloat(n.Value, 'g', -1, 64) + ")"
	case *onkexpr.StringLit:
		return c.stringLit(n)
	case *onkexpr.BoolLit:
		return strconv.FormatBool(n.Value)
	case *onkexpr.ListLit:
		return c.listLit(n)
	case *onkexpr.Ident:
		return c.ident(n)
	case *onkexpr.Select:
		return c.readField(c.expr(n.X), n.Field)
	case *onkexpr.Index:
		return c.index(n)
	case *onkexpr.Call:
		return c.call(n)
	case *onkexpr.Macro:
		return c.macro(n)
	case *onkexpr.Unary:
		return c.unary(n)
	case *onkexpr.Binary:
		return c.binary(n)
	case *onkexpr.Ternary:
		return fmt.Sprintf("func() %s { if %s { return %s }; return %s }()", canonicalGoType(n.Type()), c.expr(n.Cond), c.expr(n.A), c.expr(n.B))
	}
	return "false"
}

func (c *ruleCompiler) stringLit(n *onkexpr.StringLit) string {
	if t := n.Type(); t != nil && t.Kind == onkexpr.KindEnum {
		for i, v := range t.Enum.Values {
			if v.Name == n.Value {
				return fmt.Sprintf(ruleInt32+"(%d)", i)
			}
		}
	}
	return strconv.Quote(n.Value)
}

func (c *ruleCompiler) listLit(n *onkexpr.ListLit) string {
	elems := make([]string, len(n.Elems))
	for i, e := range n.Elems {
		elems[i] = c.expr(e)
	}
	return "onkList(" + strings.Join(elems, ", ") + ")"
}

func (c *ruleCompiler) ident(n *onkexpr.Ident) string {
	for i := len(c.vars) - 1; i >= 0; i-- {
		if c.vars[i] == n.Name {
			return fmt.Sprintf("v%d", i+1)
		}
	}
	if n.Name == "value" && c.field != nil {
		return c.readField("m", c.field)
	}
	return "m"
}

func (c *ruleCompiler) readField(base string, f *onkir.Field) string {
	raw := base + ".Get" + PascalCase(f.Name) + "()"
	switch {
	case f.Repeated:
		return widenSlice(f.Type, raw)
	case f.Type.Kind == onkir.KindMap:
		return widenMap(f.Type.MapValue, raw)
	}
	return widenScalar(f.Type, raw)
}

func widenTarget(t *onkir.Type) string {
	switch t.Kind {
	case onkir.KindScalar:
		switch t.Scalar {
		case onkir.ScalarInt32, onkir.ScalarUint32:
			return ruleInt64
		case onkir.ScalarFloat32:
			return ruleFloat64
		}
	case onkir.KindEnum:
		return ruleInt32
	}
	return ""
}

func widenScalar(t *onkir.Type, raw string) string {
	if target := widenTarget(t); target != "" {
		return target + "(" + raw + ")"
	}
	return raw
}

func widenSlice(t *onkir.Type, raw string) string {
	if target := widenTarget(t); target != "" {
		return "onkSlice[" + target + "](" + raw + ")"
	}
	return raw
}

func widenMap(t *onkir.Type, raw string) string {
	if target := widenTarget(t); target != "" {
		return "onkMapVals[" + target + "](" + raw + ")"
	}
	return raw
}

func (c *ruleCompiler) index(n *onkexpr.Index) string {
	if n.X.Type().Kind == onkexpr.KindMap {
		return "onkGet(" + c.expr(n.X) + ", " + c.expr(n.Idx) + ")"
	}
	return "onkAt(" + c.expr(n.X) + ", " + c.expr(n.Idx) + ")"
}

func (c *ruleCompiler) call(n *onkexpr.Call) string {
	switch n.Fn {
	case "size":
		arg := c.expr(n.Args[0])
		if n.Args[0].Type().Kind == onkexpr.KindString {
			return "onkRunes(" + arg + ")"
		}
		return ruleInt64 + "(len(" + arg + "))"
	case "startsWith":
		return "strings.HasPrefix(" + c.expr(n.Args[0]) + ", " + c.expr(n.Args[1]) + ")"
	case "endsWith":
		return "strings.HasSuffix(" + c.expr(n.Args[0]) + ", " + c.expr(n.Args[1]) + ")"
	case "contains":
		return "strings.Contains(" + c.expr(n.Args[0]) + ", " + c.expr(n.Args[1]) + ")"
	case "matches":
		return "onkMatch(" + c.file.regexVar(n.Regex) + ", " + c.expr(n.Args[0]) + ")"
	case "int":
		if n.Args[0].Type().Kind == onkexpr.KindDouble {
			return "onkToInt(" + c.expr(n.Args[0]) + ")"
		}
		return c.expr(n.Args[0])
	case "double":
		if n.Args[0].Type().Kind == onkexpr.KindInt {
			return ruleFloat64 + "(" + c.expr(n.Args[0]) + ")"
		}
		return c.expr(n.Args[0])
	case "has":
		if sel, ok := n.Args[0].(*onkexpr.Select); ok {
			return c.has(sel)
		}
	}
	return "false"
}

func (c *ruleCompiler) has(sel *onkexpr.Select) string {
	f := sel.Field
	base := c.expr(sel.X)
	goName := PascalCase(f.Name)
	pointerBacked := !f.Repeated && (f.Optional || f.Type.Kind == onkir.KindMessage)
	if pointerBacked {
		return fmt.Sprintf("func() bool { b := %s; return b != nil && b.%s != nil }()", base, goName)
	}
	read := c.readField(base, f)
	switch {
	case f.Repeated, f.Type.Kind == onkir.KindMap:
		return "(len(" + read + ") > 0)"
	case f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarBytes:
		return "(len(" + read + ") > 0)"
	case f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarString:
		return "(" + read + ` != "")`
	case f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarBool:
		return "(" + read + ")"
	}
	return "(" + read + " != 0)"
}

func (c *ruleCompiler) macro(n *onkexpr.Macro) string {
	list := c.expr(n.Range)
	c.vars = append(c.vars, n.Var)
	name := fmt.Sprintf("v%d", len(c.vars))
	body := c.expr(n.Body)
	c.vars = c.vars[:len(c.vars)-1]
	if n.Kind == "all" {
		return fmt.Sprintf("func() bool { for _, %s := range %s { _ = %s; if !(%s) { return false } }; return true }()", name, list, name, body)
	}
	return fmt.Sprintf("func() bool { for _, %s := range %s { _ = %s; if %s { return true } }; return false }()", name, list, name, body)
}

func (c *ruleCompiler) unary(n *onkexpr.Unary) string {
	x := c.expr(n.X)
	if n.Op == "!" {
		return "(!" + x + ")"
	}
	if n.X.Type().Kind == onkexpr.KindInt {
		return "onkNeg(" + x + ")"
	}
	return "(-" + x + ")"
}

var intOps = map[string]string{"+": "onkAdd", "-": "onkSub", "*": "onkMul", "/": "onkDiv", "%": "onkMod"}
var doubleOps = map[string]string{"+": "onkFAdd", "-": "onkFSub", "*": "onkFMul", "/": "onkFDiv"}

func (c *ruleCompiler) binary(n *onkexpr.Binary) string {
	l, r := c.expr(n.L), c.expr(n.R)
	switch n.Op {
	case "&&", "||", "==", "!=", "<", "<=", ">", ">=":
		return "(" + l + " " + n.Op + " " + r + ")"
	case "in":
		if n.R.Type().Kind == onkexpr.KindMap {
			return "onkHas(" + r + ", " + l + ")"
		}
		return "onkContains(" + r + ", " + l + ")"
	}
	if n.L.Type().Kind == onkexpr.KindInt {
		return intOps[n.Op] + "(" + l + ", " + r + ")"
	}
	return doubleOps[n.Op] + "(" + l + ", " + r + ")"
}

func writeRuleRuntime(p *Printer, rf *ruleFile) {
	for i, pattern := range rf.regexes {
		p.P("var onkRuleRe", i, " = regexp.MustCompile(", strconv.Quote(pattern), ")")
	}
	p.P(ruleRuntimeSource)
}

const ruleRuntimeSource = `type onkRuleError struct{}

func onkFail() { panic(onkRuleError{}) }

func onkRuleHolds(rule func() bool) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, isRule := r.(onkRuleError); !isRule {
				panic(r)
			}
			ok = false
		}
	}()
	return rule()
}

type onkNumber interface {
	~int32 | ~uint32 | ~int64 | ~float32 | ~float64
}

func onkList[T any](items ...T) []T { return items }

func onkSlice[D, S onkNumber](in []S) []D {
	out := make([]D, len(in))
	for i, v := range in {
		out[i] = D(v)
	}
	return out
}

func onkMapVals[D, S onkNumber](in map[string]S) map[string]D {
	out := make(map[string]D, len(in))
	for k, v := range in {
		out[k] = D(v)
	}
	return out
}

func onkAt[T any](items []T, index int64) T {
	if index < 0 || index >= int64(len(items)) {
		onkFail()
	}
	return items[index]
}

func onkGet[T any](items map[string]T, key string) T {
	v, ok := items[key]
	if !ok {
		onkFail()
	}
	return v
}

func onkHas[T any](items map[string]T, key string) bool {
	_, ok := items[key]
	return ok
}

func onkContains[T comparable](items []T, v T) bool { return slices.Contains(items, v) }

func onkMatch(re *regexp.Regexp, s string) bool { return re.MatchString(s) }

func onkRunes(s string) int64 { return int64(utf8.RuneCountInString(s)) }

func onkNeg(a int64) int64 {
	if a == math.MinInt64 {
		onkFail()
	}
	return -a
}

func onkAdd(a, b int64) int64 {
	sum := a + b
	if (sum > a) != (b > 0) {
		onkFail()
	}
	return sum
}

func onkSub(a, b int64) int64 {
	diff := a - b
	if (diff < a) != (b > 0) {
		onkFail()
	}
	return diff
}

func onkMul(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	product := a * b
	if product/b != a || (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		onkFail()
	}
	return product
}

func onkDiv(a, b int64) int64 {
	if b == 0 || (a == math.MinInt64 && b == -1) {
		onkFail()
	}
	return a / b
}

func onkMod(a, b int64) int64 {
	if b == 0 || (a == math.MinInt64 && b == -1) {
		onkFail()
	}
	return a % b
}

func onkFinite(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		onkFail()
	}
	return x
}

func onkFAdd(a, b float64) float64 { return onkFinite(a + b) }

func onkFSub(a, b float64) float64 { return onkFinite(a - b) }

func onkFMul(a, b float64) float64 { return onkFinite(a * b) }

func onkFDiv(a, b float64) float64 {
	if b == 0 {
		onkFail()
	}
	return onkFinite(a / b)
}

func onkToInt(x float64) int64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x >= 9223372036854775808.0 || x < -9223372036854775808.0 {
		onkFail()
	}
	return int64(x)
}
`
