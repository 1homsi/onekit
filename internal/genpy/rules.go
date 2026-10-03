package genpy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

const (
	pyFalseLiteral = "False"
	pyNoneLiteral  = "None"
	pyIntFunction  = "int"
)

type pyRuleState struct {
	regexes []string
	used    bool
}

func (s *pyRuleState) regexVar(pattern string) string {
	for i, existing := range s.regexes {
		if existing == pattern {
			return fmt.Sprintf("_ONK_RE%d", i)
		}
	}
	s.regexes = append(s.regexes, pattern)
	return fmt.Sprintf("_ONK_RE%d", len(s.regexes)-1)
}

type pyRuleCompiler struct {
	state *pyRuleState
	field *onkir.Field
	vars  []string
}

func pyString(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return string(data)
}

func pyFileHasRules(file *onkir.File) bool {
	has := func(decorators []onkir.Decorator) bool {
		for _, d := range decorators {
			if d.Name == onkexpr.RuleDecorator {
				return true
			}
		}
		return false
	}
	var walk func(m *onkir.Message) bool
	walk = func(m *onkir.Message) bool {
		if has(m.Decorators) {
			return true
		}
		for _, f := range m.Fields {
			if has(f.Decorators) {
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

func writePyRuleChecks(p *Printer, m *onkir.Message) {
	rules, err := onkexpr.RulesFor(m)
	if err != nil {
		p.P("raise ValueError(", pyString("invalid @rule: "+err.Error()), ")")
		return
	}
	for _, r := range rules {
		c := &pyRuleCompiler{state: &p.rules, field: r.Field}
		code := c.expr(r.Expr)
		p.rules.used = true
		p.P("if not _onk_holds(lambda: ", code, "): violations.append(", pyString(r.Message), ")")
	}
}

func (c *pyRuleCompiler) expr(n onkexpr.Node) string {
	switch n := n.(type) {
	case *onkexpr.IntLit:
		return "(" + strconv.FormatInt(n.Value, 10) + ")"
	case *onkexpr.DoubleLit:
		return pyFloat(n.Value)
	case *onkexpr.StringLit:
		return c.stringLit(n)
	case *onkexpr.BoolLit:
		if n.Value {
			return "True"
		}
		return pyFalseLiteral
	case *onkexpr.ListLit:
		elems := make([]string, len(n.Elems))
		for i, e := range n.Elems {
			elems[i] = c.expr(e)
		}
		return "[" + strings.Join(elems, ", ") + "]"
	case *onkexpr.Ident:
		return c.ident(n)
	case *onkexpr.Select:
		return c.readField(c.expr(n.X), n.Field)
	case *onkexpr.Index:
		if n.X.Type().Kind == onkexpr.KindMap {
			return "_onk_get(" + c.expr(n.X) + ", " + c.expr(n.Idx) + ")"
		}
		return "_onk_at(" + c.expr(n.X) + ", " + c.expr(n.Idx) + ")"
	case *onkexpr.Call:
		return c.call(n)
	case *onkexpr.Macro:
		return c.macro(n)
	case *onkexpr.Unary:
		return c.unary(n)
	case *onkexpr.Binary:
		return c.binary(n)
	case *onkexpr.Ternary:
		return "(" + c.expr(n.A) + " if " + c.expr(n.Cond) + " else " + c.expr(n.B) + ")"
	}
	return pyFalseLiteral
}

func pyFloat(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnN") {
		s += ".0"
	}
	return "(" + s + ")"
}

func (c *pyRuleCompiler) stringLit(n *onkexpr.StringLit) string {
	if t := n.Type(); t != nil && t.Kind == onkexpr.KindEnum {
		for i, v := range t.Enum.Values {
			if v.Name == n.Value {
				return strconv.Itoa(i)
			}
		}
	}
	return pyString(n.Value)
}

func (c *pyRuleCompiler) ident(n *onkexpr.Ident) string {
	for i := len(c.vars) - 1; i >= 0; i-- {
		if c.vars[i] == n.Name {
			return fmt.Sprintf("v%d", i+1)
		}
	}
	if n.Name == "value" && c.field != nil {
		return c.readField("self", c.field)
	}
	return "self"
}

func pyZero(f *onkir.Field) string {
	switch {
	case f.Repeated:
		return "[]"
	case f.Type.Kind == onkir.KindMap:
		return "{}"
	case f.Type.Kind == onkir.KindMessage:
		return pyNoneLiteral
	case f.Type.Kind == onkir.KindEnum:
		return "0"
	case f.Type.Kind == onkir.KindScalar:
		switch f.Type.Scalar {
		case onkir.ScalarString:
			return `""`
		case onkir.ScalarBool:
			return pyFalseLiteral
		case onkir.ScalarFloat32, onkir.ScalarFloat64:
			return "0.0"
		case onkir.ScalarBytes:
			return `b""`
		}
	}
	return "0"
}

func (c *pyRuleCompiler) readField(base string, f *onkir.Field) string {
	return "_onk_field(" + base + ", " + pyString(f.Name) + ", " + pyZero(f) + ")"
}

func (c *pyRuleCompiler) call(n *onkexpr.Call) string {
	switch n.Fn {
	case "size":
		return "len(" + c.expr(n.Args[0]) + ")"
	case "startsWith":
		return c.expr(n.Args[0]) + ".startswith(" + c.expr(n.Args[1]) + ")"
	case "endsWith":
		return c.expr(n.Args[0]) + ".endswith(" + c.expr(n.Args[1]) + ")"
	case "contains":
		return "(" + c.expr(n.Args[1]) + " in " + c.expr(n.Args[0]) + ")"
	case "matches":
		return "(" + c.state.regexVar(n.Regex) + ".fullmatch(" + c.expr(n.Args[0]) + ") is not None)"
	case pyIntFunction:
		if n.Args[0].Type().Kind == onkexpr.KindDouble {
			return "_onk_to_int(" + c.expr(n.Args[0]) + ")"
		}
		return c.expr(n.Args[0])
	case "double":
		if n.Args[0].Type().Kind == onkexpr.KindInt {
			return "float(" + c.expr(n.Args[0]) + ")"
		}
		return c.expr(n.Args[0])
	case "has":
		if sel, ok := n.Args[0].(*onkexpr.Select); ok {
			return c.has(sel)
		}
	}
	return pyFalseLiteral
}

func (c *pyRuleCompiler) has(sel *onkexpr.Select) string {
	f := sel.Field
	base := c.expr(sel.X)
	if !f.Repeated && (f.Optional || f.Type.Kind == onkir.KindMessage) {
		return "(_onk_field(" + base + ", " + pyString(f.Name) + ", None) is not None)"
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
		return "(" + read + " is True)"
	case f.Type.Kind == onkir.KindScalar && (f.Type.Scalar == onkir.ScalarFloat32 || f.Type.Scalar == onkir.ScalarFloat64):
		return "(" + read + " != 0.0)"
	}
	return "(" + read + " != 0)"
}

func (c *pyRuleCompiler) macro(n *onkexpr.Macro) string {
	list := c.expr(n.Range)
	c.vars = append(c.vars, n.Var)
	name := fmt.Sprintf("v%d", len(c.vars))
	body := c.expr(n.Body)
	c.vars = c.vars[:len(c.vars)-1]
	fn := "any"
	if n.Kind == "all" {
		fn = "all"
	}
	return fn + "((" + body + ") for " + name + " in " + list + ")"
}

func (c *pyRuleCompiler) unary(n *onkexpr.Unary) string {
	x := c.expr(n.X)
	if n.Op == "!" {
		return "(not " + x + ")"
	}
	if n.X.Type().Kind == onkexpr.KindInt {
		return "_onk_neg(" + x + ")"
	}
	return "(-" + x + ")"
}

var pyIntOps = map[string]string{"+": "_onk_add", "-": "_onk_sub", "*": "_onk_mul", "/": "_onk_div", "%": "_onk_mod"}
var pyDoubleOps = map[string]string{"+": "_onk_fadd", "-": "_onk_fsub", "*": "_onk_fmul", "/": "_onk_fdiv"}

func (c *pyRuleCompiler) binary(n *onkexpr.Binary) string {
	l, r := c.expr(n.L), c.expr(n.R)
	switch n.Op {
	case "&&":
		return "(" + l + " and " + r + ")"
	case "||":
		return "(" + l + " or " + r + ")"
	case "==", "!=", "<", "<=", ">", ">=":
		return "(" + l + " " + n.Op + " " + r + ")"
	case "in":
		return "(" + l + " in " + r + ")"
	}
	if n.L.Type().Kind == onkexpr.KindInt {
		return pyIntOps[n.Op] + "(" + l + ", " + r + ")"
	}
	return pyDoubleOps[n.Op] + "(" + l + ", " + r + ")"
}

func writePyRuleRuntime(p *Printer) {
	p.Blank()
	p.b.WriteString(pyRuleRuntimeSource)
	for i, pattern := range p.rules.regexes {
		p.P("_ONK_RE", i, " = re.compile(", pyString(pattern), ")")
	}
}

const pyRuleRuntimeSource = `class _OnkRuleError(Exception):
    pass


def _onk_fail():
    raise _OnkRuleError()


def _onk_holds(rule):
    try:
        return rule() is True
    except Exception:
        return False


_ONK_MIN = -(2 ** 63)
_ONK_MAX = 2 ** 63 - 1


def _onk_int(x):
    if x < _ONK_MIN or x > _ONK_MAX:
        _onk_fail()
    return x


def _onk_add(a, b):
    return _onk_int(a + b)


def _onk_sub(a, b):
    return _onk_int(a - b)


def _onk_mul(a, b):
    return _onk_int(a * b)


def _onk_div(a, b):
    if b == 0:
        _onk_fail()
    q = abs(a) // abs(b)
    return _onk_int(q if (a < 0) == (b < 0) else -q)


def _onk_mod(a, b):
    if b == 0 or (a == _ONK_MIN and b == -1):
        _onk_fail()
    r = abs(a) % abs(b)
    return r if a >= 0 else -r


def _onk_neg(a):
    return _onk_int(-a)


def _onk_finite(x):
    if not math.isfinite(x):
        _onk_fail()
    return x


def _onk_fadd(a, b):
    return _onk_finite(a + b)


def _onk_fsub(a, b):
    return _onk_finite(a - b)


def _onk_fmul(a, b):
    return _onk_finite(a * b)


def _onk_fdiv(a, b):
    if b == 0:
        _onk_fail()
    return _onk_finite(a / b)


def _onk_to_int(x):
    if not math.isfinite(x) or x >= 9223372036854775808.0 or x < -9223372036854775808.0:
        _onk_fail()
    return int(x)


def _onk_at(items, index):
    if index < 0 or index >= len(items):
        _onk_fail()
    return items[index]


def _onk_get(items, key):
    if key not in items:
        _onk_fail()
    return items[key]


def _onk_field(base, name, zero):
    if base is None:
        return zero
    value = getattr(base, name)
    return zero if value is None else value


`
