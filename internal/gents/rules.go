package gents

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

const tsFalseLiteral = "false"

type tsRuleState struct {
	naming  tsNaming
	regexes []string
	used    bool
}

func (s *tsRuleState) regexVar(pattern string) string {
	full := "^(?:" + pattern + ")$"
	for i, existing := range s.regexes {
		if existing == full {
			return fmt.Sprintf("onkRe%d", i)
		}
	}
	s.regexes = append(s.regexes, full)
	return fmt.Sprintf("onkRe%d", len(s.regexes)-1)
}

type tsRuleCompiler struct {
	state    *tsRuleState
	self     string
	field    *onkir.Field
	vars     []string
	bindings map[string]string
}

func jsString(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return string(data)
}

func writeTSRuleChecks(p *Printer, m *onkir.Message, self string) {
	rules, err := onkexpr.RulesFor(m)
	if err != nil {
		p.P("throw new Error(", jsString("invalid @rule: "+err.Error()), ");")
		return
	}
	for _, r := range rules {
		c := &tsRuleCompiler{state: &p.rules, self: self, field: r.Field}
		code := c.expr(r.Expr)
		p.rules.used = true
		p.P("if (!Onk.ruleHolds(() => ", code, ")) violations.push(", jsString(r.Message), ");")
	}
}

func (c *tsRuleCompiler) expr(n onkexpr.Node) string {
	switch n := n.(type) {
	case *onkexpr.IntLit:
		return "(" + strconv.FormatInt(n.Value, 10) + "n)"
	case *onkexpr.DoubleLit:
		return "(" + strconv.FormatFloat(n.Value, 'g', -1, 64) + ")"
	case *onkexpr.StringLit:
		return c.stringLit(n)
	case *onkexpr.BoolLit:
		return strconv.FormatBool(n.Value)
	case *onkexpr.ListLit:
		elems := make([]string, len(n.Elems))
		for i, e := range n.Elems {
			elems[i] = c.expr(e)
		}
		return "[" + strings.Join(elems, ", ") + "]"
	case *onkexpr.Ident:
		return c.ident(n)
	case *onkexpr.Select:
		base := tsMessageView(c.state.naming, c.expr(n.X), n.X.Type().Message)
		return c.readField(base, n.Field)
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
		return "(" + c.expr(n.Cond) + " ? " + c.expr(n.A) + " : " + c.expr(n.B) + ")"
	}
	return tsFalseLiteral
}

func (c *tsRuleCompiler) stringLit(n *onkexpr.StringLit) string {
	if t := n.Type(); t != nil && t.Kind == onkexpr.KindEnum {
		for i, v := range t.Enum.Values {
			if v.Name == n.Value {
				return strconv.Itoa(i)
			}
		}
	}
	return jsString(n.Value)
}

func (c *tsRuleCompiler) ident(n *onkexpr.Ident) string {
	for i := len(c.vars) - 1; i >= 0; i-- {
		if c.vars[i] == n.Name {
			return fmt.Sprintf("v%d", i+1)
		}
	}
	if bound, ok := c.bindings[n.Name]; ok {
		return bound
	}
	if n.Name == "value" && c.field != nil {
		return c.readField(tsMessageView(c.state.naming, c.self, c.field.Message), c.field)
	}
	return c.self
}

func tsMessageView(naming tsNaming, base string, m *onkir.Message) string {
	if m == nil {
		return base
	}
	if field := rootUnwrapField(m); field != nil {
		return "({" + naming.key("", field) + ": " + base + "})"
	}
	return base
}

func tsFlatView(naming tsNaming, m *onkir.Message, base, wirePrefix string) string {
	return "((b: any) => (" + tsFlatObject(naming, m, "b", wirePrefix) + "))(" + base + ")"
}

func tsFlatObject(naming tsNaming, m *onkir.Message, b, wirePrefix string) string {
	var parts []string
	for _, f := range m.Fields {
		if f.Oneof != nil {
			continue
		}
		if prefix, ok := flattenPrefix(f); ok {
			parts = append(parts, naming.key("", f)+": "+tsFlatObject(naming, f.Type.Message, b, wirePrefix+prefix))
			continue
		}
		parts = append(parts, naming.key("", f)+": "+b+"?."+naming.key(wirePrefix, f))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (c *tsRuleCompiler) readField(base string, f *onkir.Field) string {
	if prefix, ok := flattenPrefix(f); ok && !f.Repeated {
		return tsFlatView(c.state.naming, f.Type.Message, base, prefix)
	}
	raw := base + "?." + c.state.naming.key("", f)
	switch {
	case f.Repeated:
		return tsWidenList(f, raw)
	case f.Type.Kind == onkir.KindMap:
		return tsWidenMap(f, raw)
	}
	return tsWidenScalar(f, raw)
}

func tsEnumNames(e *onkir.Enum) string {
	names := make([]string, len(e.Values))
	for i, v := range e.Values {
		names[i] = jsString(v.JSONName())
	}
	return "[" + strings.Join(names, ", ") + "]"
}

func tsWidenScalar(f *onkir.Field, raw string) string {
	switch f.Type.Kind {
	case onkir.KindEnum:
		if needsEnumNumberEncoding(f) {
			return "(" + raw + " ?? 0)"
		}
		return "Onk.ord(" + raw + ", " + tsEnumNames(f.Type.Enum) + ")"
	case onkir.KindScalar:
		switch f.Type.Scalar {
		case onkir.ScalarInt32, onkir.ScalarUint32, onkir.ScalarInt64:
			return "BigInt(" + raw + " ?? 0)"
		case onkir.ScalarFloat32, onkir.ScalarFloat64:
			return "(" + raw + " ?? 0)"
		case onkir.ScalarBool:
			return "(" + raw + " ?? false)"
		case onkir.ScalarString, onkir.ScalarBytes:
			return "(" + raw + ` ?? "")`
		}
	}
	return raw
}

func tsElementConversion(t *onkir.Type) string {
	switch t.Kind {
	case onkir.KindEnum:
		return "(e: any) => Onk.ord(e, " + tsEnumNames(t.Enum) + ")"
	case onkir.KindScalar:
		switch t.Scalar {
		case onkir.ScalarInt32, onkir.ScalarUint32, onkir.ScalarInt64:
			return "(e: any) => BigInt(e)"
		}
	}
	return ""
}

func tsWidenList(f *onkir.Field, raw string) string {
	if conv := tsElementConversion(f.Type); conv != "" {
		return "(" + raw + " ?? []).map(" + conv + ")"
	}
	return "(" + raw + " ?? [])"
}

func tsWidenMap(f *onkir.Field, raw string) string {
	if conv := tsElementConversion(f.Type.MapValue); conv != "" {
		return "Onk.mapVals(" + raw + " ?? {}, " + conv + ")"
	}
	return "(" + raw + " ?? {})"
}

func (c *tsRuleCompiler) index(n *onkexpr.Index) string {
	if n.X.Type().Kind == onkexpr.KindMap {
		return "Onk.get(" + c.expr(n.X) + ", " + c.expr(n.Idx) + ")"
	}
	return "Onk.at(" + c.expr(n.X) + ", " + c.expr(n.Idx) + ")"
}

func (c *tsRuleCompiler) call(n *onkexpr.Call) string {
	switch n.Fn {
	case "size":
		return c.size(n)
	case "startsWith":
		return "(" + c.expr(n.Args[0]) + ").startsWith(" + c.expr(n.Args[1]) + ")"
	case "endsWith":
		return "(" + c.expr(n.Args[0]) + ").endsWith(" + c.expr(n.Args[1]) + ")"
	case "contains":
		return "(" + c.expr(n.Args[0]) + ").includes(" + c.expr(n.Args[1]) + ")"
	case "matches":
		return c.state.regexVar(n.Regex) + ".test(" + c.expr(n.Args[0]) + ")"
	case "int":
		if n.Args[0].Type().Kind == onkexpr.KindDouble {
			return "Onk.toInt(" + c.expr(n.Args[0]) + ")"
		}
		return c.expr(n.Args[0])
	case "double":
		if n.Args[0].Type().Kind == onkexpr.KindInt {
			return "Number(" + c.expr(n.Args[0]) + ")"
		}
		return c.expr(n.Args[0])
	case "has":
		if sel, ok := n.Args[0].(*onkexpr.Select); ok {
			return c.has(sel)
		}
	}
	return tsFalseLiteral
}

func (c *tsRuleCompiler) size(n *onkexpr.Call) string {
	arg := c.expr(n.Args[0])
	switch n.Args[0].Type().Kind {
	case onkexpr.KindString:
		return "Onk.runes(" + arg + ")"
	case onkexpr.KindBytes:
		return "Onk.bytesLen(" + arg + ")"
	case onkexpr.KindMap:
		return "BigInt(Object.keys(" + arg + ").length)"
	}
	return "BigInt((" + arg + ").length)"
}

func (c *tsRuleCompiler) has(sel *onkexpr.Select) string {
	f := sel.Field
	base := tsMessageView(c.state.naming, c.expr(sel.X), sel.X.Type().Message)
	if f.Oneof != nil {
		return tsFalseLiteral
	}
	if _, ok := flattenPrefix(f); ok && !f.Repeated {
		return "true"
	}
	raw := base + "?." + c.state.naming.key("", f)
	if !f.Repeated && (f.Optional || f.Type.Kind == onkir.KindMessage) {
		return "((" + raw + ") !== undefined && (" + raw + ") !== null)"
	}
	read := c.readField(base, f)
	switch {
	case f.Repeated:
		return "((" + read + ").length > 0)"
	case f.Type.Kind == onkir.KindMap:
		return "(Object.keys(" + read + ").length > 0)"
	case f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarBytes:
		return "(Onk.bytesLen(" + read + ") > 0n)"
	case f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarString:
		return "(" + read + ` !== "")`
	case f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarBool:
		return read
	case f.Type.Kind == onkir.KindScalar && f.Type.Scalar != onkir.ScalarFloat32 && f.Type.Scalar != onkir.ScalarFloat64:
		return "(" + read + " !== 0n)"
	}
	return "(" + read + " !== 0)"
}

func (c *tsRuleCompiler) macro(n *onkexpr.Macro) string {
	list := c.expr(n.Range)
	c.vars = append(c.vars, n.Var)
	name := fmt.Sprintf("v%d", len(c.vars))
	body := c.expr(n.Body)
	c.vars = c.vars[:len(c.vars)-1]
	method := "some"
	if n.Kind == "all" {
		method = "every"
	}
	return "(" + list + ")." + method + "((" + name + ": any) => " + body + ")"
}

func (c *tsRuleCompiler) unary(n *onkexpr.Unary) string {
	x := c.expr(n.X)
	if n.Op == "!" {
		return "(!" + x + ")"
	}
	if n.X.Type().Kind == onkexpr.KindInt {
		return "Onk.neg(" + x + ")"
	}
	return "(-" + x + ")"
}

var tsIntOps = map[string]string{"+": "Onk.add", "-": "Onk.sub", "*": "Onk.mul", "/": "Onk.div", "%": "Onk.mod"}
var tsDoubleOps = map[string]string{"+": "Onk.fAdd", "-": "Onk.fSub", "*": "Onk.fMul", "/": "Onk.fDiv"}

func (c *tsRuleCompiler) binary(n *onkexpr.Binary) string {
	l, r := c.expr(n.L), c.expr(n.R)
	switch n.Op {
	case "&&", "||", "<", "<=", ">", ">=":
		return "(" + l + " " + n.Op + " " + r + ")"
	case "==":
		return "(" + l + " === " + r + ")"
	case "!=":
		return "(" + l + " !== " + r + ")"
	case "in":
		if n.R.Type().Kind == onkexpr.KindMap {
			return "Onk.has(" + r + ", " + l + ")"
		}
		return "(" + r + ").includes(" + l + ")"
	}
	if n.L.Type().Kind == onkexpr.KindInt {
		return tsIntOps[n.Op] + "(" + l + ", " + r + ")"
	}
	return tsDoubleOps[n.Op] + "(" + l + ", " + r + ")"
}

func writeTSRuleRuntime(p *Printer) {
	for i, pattern := range p.rules.regexes {
		p.P("const onkRe", i, " = new RegExp(", jsString(pattern), `, "u");`)
	}
	p.P(tsRuleRuntimeSource)
}

const tsRuleRuntimeSource = `class OnkRuleError extends Error {}

class Onk {
  static readonly MIN = -(2n ** 63n);
  static readonly MAX = 2n ** 63n - 1n;

  static fail(): never {
    throw new OnkRuleError("rule error");
  }

  static ruleHolds(rule: () => boolean): boolean {
    try {
      return rule() === true;
    } catch {
      return false;
    }
  }

  static int(x: bigint): bigint {
    if (x < Onk.MIN || x > Onk.MAX) Onk.fail();
    return x;
  }

  static add(a: bigint, b: bigint): bigint {
    return Onk.int(a + b);
  }

  static sub(a: bigint, b: bigint): bigint {
    return Onk.int(a - b);
  }

  static mul(a: bigint, b: bigint): bigint {
    return Onk.int(a * b);
  }

  static div(a: bigint, b: bigint): bigint {
    if (b === 0n) Onk.fail();
    return Onk.int(a / b);
  }

  static mod(a: bigint, b: bigint): bigint {
    if (b === 0n || (a === Onk.MIN && b === -1n)) Onk.fail();
    return a % b;
  }

  static neg(a: bigint): bigint {
    return Onk.int(-a);
  }

  static finite(x: number): number {
    if (!Number.isFinite(x)) Onk.fail();
    return x;
  }

  static fAdd(a: number, b: number): number {
    return Onk.finite(a + b);
  }

  static fSub(a: number, b: number): number {
    return Onk.finite(a - b);
  }

  static fMul(a: number, b: number): number {
    return Onk.finite(a * b);
  }

  static fDiv(a: number, b: number): number {
    if (b === 0) Onk.fail();
    return Onk.finite(a / b);
  }

  static toInt(x: number): bigint {
    if (!Number.isFinite(x) || x >= 9223372036854775808 || x < -9223372036854775808) Onk.fail();
    return BigInt(Math.trunc(x));
  }

  static at<T>(items: T[], index: bigint): T {
    if (index < 0n || index >= BigInt(items.length)) Onk.fail();
    return items[Number(index)];
  }

  static get<T>(items: Record<string, T>, key: string): T {
    if (!Object.prototype.hasOwnProperty.call(items, key)) Onk.fail();
    return items[key];
  }

  static has(items: Record<string, unknown>, key: string): boolean {
    return Object.prototype.hasOwnProperty.call(items, key);
  }

  static runes(s: string): bigint {
    let count = 0;
    for (const _ of s) count++;
    return BigInt(count);
  }

  static bytesLen(x: any): bigint {
    if (typeof x === "string") {
      const padding = x.endsWith("==") ? 2 : x.endsWith("=") ? 1 : 0;
      return BigInt(Math.floor((x.length * 3) / 4) - padding);
    }
    return BigInt(x?.length ?? 0);
  }

  static ord(x: any, names: string[]): number {
    if (x === undefined || x === null) return 0;
    return names.indexOf(x);
  }

  static mapVals(items: any, convert: (e: any) => any): Record<string, any> {
    const out: Record<string, any> = {};
    for (const key of Object.keys(items)) out[key] = convert(items[key]);
    return out;
  }
}
`
