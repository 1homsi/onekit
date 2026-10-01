package onkexpr

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

var (
	typeBool   = &Type{Kind: KindBool}
	typeInt    = &Type{Kind: KindInt}
	typeDouble = &Type{Kind: KindDouble}
	typeString = &Type{Kind: KindString}
	typeBytes  = &Type{Kind: KindBytes}
)

func MessageType(m *onkir.Message) *Type {
	return &Type{Kind: KindMessage, Message: m}
}

func FieldType(f *onkir.Field) (*Type, error) {
	if f.Oneof != nil {
		return nil, fmt.Errorf("oneof field %q cannot be used in a rule yet", f.Name)
	}
	t, err := valueType(f.Type, f.Name)
	if err != nil {
		return nil, err
	}
	if f.Repeated {
		return &Type{Kind: KindList, Elem: t}, nil
	}
	return t, nil
}

func valueType(t *onkir.Type, name string) (*Type, error) {
	switch t.Kind {
	case onkir.KindScalar:
		switch t.Scalar {
		case onkir.ScalarString:
			return typeString, nil
		case onkir.ScalarBool:
			return typeBool, nil
		case onkir.ScalarInt32, onkir.ScalarUint32, onkir.ScalarInt64:
			return typeInt, nil
		case onkir.ScalarFloat32, onkir.ScalarFloat64:
			return typeDouble, nil
		case onkir.ScalarBytes:
			return typeBytes, nil
		case onkir.ScalarUint64:
			return nil, fmt.Errorf("uint64 field %q cannot be used in a rule yet; rules work on 64-bit signed integers", name)
		default:
			return nil, fmt.Errorf("%s field %q cannot be used in a rule yet", t.Scalar, name)
		}
	case onkir.KindEnum:
		return &Type{Kind: KindEnum, Enum: t.Enum}, nil
	case onkir.KindMessage:
		return MessageType(t.Message), nil
	case onkir.KindMap:
		elem, err := valueType(t.MapValue, name)
		if err != nil {
			return nil, err
		}
		return &Type{Kind: KindMap, Elem: elem}, nil
	}
	return nil, fmt.Errorf("field %q cannot be used in a rule", name)
}

type checker struct {
	env   map[string]*Type
	scope []map[string]*Type
}

func Check(n Node, env map[string]*Type) (*Type, error) {
	c := &checker{env: env}
	return c.check(n)
}

func CheckRule(src string, env map[string]*Type) (Node, error) {
	n, err := Parse(src)
	if err != nil {
		return nil, err
	}
	t, err := Check(n, env)
	if err != nil {
		return nil, err
	}
	if t.Kind != KindBool {
		return nil, &Error{Offset: n.Position().Offset, Message: fmt.Sprintf("a rule must evaluate to bool, but this is %s", t)}
	}
	return n, nil
}

func (c *checker) errorf(n Node, format string, args ...any) error {
	return &Error{Offset: n.Position().Offset, Message: fmt.Sprintf(format, args...)}
}

func (c *checker) lookup(name string) (*Type, bool) {
	for i := len(c.scope) - 1; i >= 0; i-- {
		if t, ok := c.scope[i][name]; ok {
			return t, true
		}
	}
	t, ok := c.env[name]
	return t, ok
}

func (c *checker) check(n Node) (*Type, error) {
	t, err := c.infer(n)
	if err != nil {
		return nil, err
	}
	n.setType(t)
	return t, nil
}

func (c *checker) infer(n Node) (*Type, error) {
	switch n := n.(type) {
	case *IntLit:
		return typeInt, nil
	case *DoubleLit:
		return typeDouble, nil
	case *StringLit:
		if n.T != nil && n.T.Kind == KindEnum {
			return n.T, nil
		}
		return typeString, nil
	case *BoolLit:
		return typeBool, nil
	case *ListLit:
		return c.inferList(n)
	case *Ident:
		t, ok := c.lookup(n.Name)
		if !ok {
			return nil, c.errorf(n, "unknown name %q%s", n.Name, c.hintNames())
		}
		return t, nil
	case *Select:
		return c.inferSelect(n)
	case *Index:
		return c.inferIndex(n)
	case *Call:
		return c.inferCall(n)
	case *Macro:
		return c.inferMacro(n)
	case *Unary:
		return c.inferUnary(n)
	case *Binary:
		return c.inferBinary(n)
	case *Ternary:
		return c.inferTernary(n)
	}
	return nil, c.errorf(n, "unsupported expression")
}

func (c *checker) hintNames() string {
	var names []string
	for name := range c.env {
		names = append(names, name)
	}
	for _, frame := range c.scope {
		for name := range frame {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sortStrings(names)
	return " (available: " + strings.Join(names, ", ") + ")"
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func (c *checker) inferList(n *ListLit) (*Type, error) {
	if len(n.Elems) == 0 {
		return nil, c.errorf(n, "a list literal needs at least one element so its type is known")
	}
	first, err := c.check(n.Elems[0])
	if err != nil {
		return nil, err
	}
	for _, elem := range n.Elems[1:] {
		t, err := c.check(elem)
		if err != nil {
			return nil, err
		}
		if !t.Equal(first) {
			return nil, c.errorf(elem, "list elements must all have one type, but this is %s and the first is %s", t, first)
		}
	}
	if first.Kind == KindMessage || first.Kind == KindList || first.Kind == KindMap {
		return nil, c.errorf(n, "list literals can only hold bool, int, double or string values")
	}
	return &Type{Kind: KindList, Elem: first}, nil
}

func (c *checker) inferSelect(n *Select) (*Type, error) {
	x, err := c.check(n.X)
	if err != nil {
		return nil, err
	}
	if x.Kind != KindMessage {
		return nil, c.errorf(n, "cannot select .%s from a %s", n.Name, x)
	}
	for _, f := range x.Message.Fields {
		if f.Name != n.Name {
			continue
		}
		t, err := FieldType(f)
		if err != nil {
			return nil, c.errorf(n, "%v", err)
		}
		n.Field = f
		return t, nil
	}
	var names []string
	for _, f := range x.Message.Fields {
		names = append(names, f.Name)
	}
	return nil, c.errorf(n, "%s has no field %q (fields: %s)", x.Message.Name, n.Name, strings.Join(names, ", "))
}

func (c *checker) inferIndex(n *Index) (*Type, error) {
	x, err := c.check(n.X)
	if err != nil {
		return nil, err
	}
	idx, err := c.check(n.Idx)
	if err != nil {
		return nil, err
	}
	switch x.Kind {
	case KindList:
		if idx.Kind != KindInt {
			return nil, c.errorf(n.Idx, "a list index must be an int, not %s", idx)
		}
		return x.Elem, nil
	case KindMap:
		if idx.Kind != KindString {
			return nil, c.errorf(n.Idx, "a map key must be a string, not %s", idx)
		}
		return x.Elem, nil
	}
	return nil, c.errorf(n, "cannot index a %s", x)
}

func (c *checker) arity(n *Call, want int) error {
	if len(n.Args) != want {
		return c.errorf(n, "%s expects %d argument(s), got %d", n.Fn, want, len(n.Args))
	}
	return nil
}

func (c *checker) inferCall(n *Call) (*Type, error) {
	for _, arg := range n.Args {
		if _, err := c.check(arg); err != nil {
			return nil, err
		}
	}
	switch n.Fn {
	case "size":
		if err := c.arity(n, 1); err != nil {
			return nil, err
		}
		switch n.Args[0].Type().Kind {
		case KindString, KindList, KindMap, KindBytes:
			return typeInt, nil
		}
		return nil, c.errorf(n, "size() works on strings, bytes, lists and maps, not %s", n.Args[0].Type())
	case "has":
		if err := c.arity(n, 1); err != nil {
			return nil, err
		}
		sel, ok := n.Args[0].(*Select)
		if !ok || sel.Field == nil {
			return nil, c.errorf(n, "has() needs a field, as in has(self.name)")
		}
		return typeBool, nil
	case fnInt:
		if err := c.arity(n, 1); err != nil {
			return nil, err
		}
		if k := n.Args[0].Type().Kind; k != KindInt && k != KindDouble {
			return nil, c.errorf(n, "int() converts a double, not %s", n.Args[0].Type())
		}
		return typeInt, nil
	case fnDouble:
		if err := c.arity(n, 1); err != nil {
			return nil, err
		}
		if k := n.Args[0].Type().Kind; k != KindInt && k != KindDouble {
			return nil, c.errorf(n, "double() converts an int, not %s", n.Args[0].Type())
		}
		return typeDouble, nil
	case "startsWith", "endsWith", "contains":
		if err := c.arity(n, 2); err != nil {
			return nil, err
		}
		for _, arg := range n.Args {
			if arg.Type().Kind != KindString {
				return nil, c.errorf(arg, "%s works on strings, not %s", n.Fn, arg.Type())
			}
		}
		return typeBool, nil
	case "matches":
		return c.inferMatches(n)
	}
	return nil, c.errorf(n, "unknown function %q", n.Fn)
}

func (c *checker) inferMatches(n *Call) (*Type, error) {
	if err := c.arity(n, 2); err != nil {
		return nil, err
	}
	if n.Args[0].Type().Kind != KindString {
		return nil, c.errorf(n.Args[0], "matches works on strings, not %s", n.Args[0].Type())
	}
	lit, ok := n.Args[1].(*StringLit)
	if !ok {
		return nil, c.errorf(n.Args[1], "the pattern must be a string literal so it can be checked when the schema compiles")
	}
	if err := ValidateRegex(lit.Value); err != nil {
		return nil, c.errorf(lit, "invalid pattern %q: %v", lit.Value, err)
	}
	n.Regex = lit.Value
	return typeBool, nil
}

func (c *checker) inferMacro(n *Macro) (*Type, error) {
	r, err := c.check(n.Range)
	if err != nil {
		return nil, err
	}
	if r.Kind != KindList {
		return nil, c.errorf(n, "%s() works on lists, not %s", n.Kind, r)
	}
	c.scope = append(c.scope, map[string]*Type{n.Var: r.Elem})
	body, err := c.check(n.Body)
	c.scope = c.scope[:len(c.scope)-1]
	if err != nil {
		return nil, err
	}
	if body.Kind != KindBool {
		return nil, c.errorf(n.Body, "the body of %s() must be bool, not %s", n.Kind, body)
	}
	return typeBool, nil
}

func (c *checker) inferUnary(n *Unary) (*Type, error) {
	x, err := c.check(n.X)
	if err != nil {
		return nil, err
	}
	switch n.Op {
	case "!":
		if x.Kind != KindBool {
			return nil, c.errorf(n, "! needs a bool, not %s", x)
		}
		return typeBool, nil
	default:
		if x.Kind != KindInt && x.Kind != KindDouble {
			return nil, c.errorf(n, "unary - needs an int or a double, not %s", x)
		}
		return x, nil
	}
}

func (c *checker) inferTernary(n *Ternary) (*Type, error) {
	cond, err := c.check(n.Cond)
	if err != nil {
		return nil, err
	}
	if cond.Kind != KindBool {
		return nil, c.errorf(n.Cond, "the condition of ?: must be bool, not %s", cond)
	}
	a, err := c.check(n.A)
	if err != nil {
		return nil, err
	}
	b, err := c.check(n.B)
	if err != nil {
		return nil, err
	}
	if !a.Equal(b) {
		return nil, c.errorf(n, "both branches of ?: must have one type, but they are %s and %s", a, b)
	}
	return a, nil
}

func (c *checker) inferBinary(n *Binary) (*Type, error) {
	l, err := c.check(n.L)
	if err != nil {
		return nil, err
	}
	if n.Op == "in" {
		return c.inferIn(n, l)
	}
	if n.Op == "==" || n.Op == "!=" {
		if lit, ok := n.R.(*StringLit); ok && l.Kind == KindEnum {
			if err := c.enumLiteral(lit, l); err != nil {
				return nil, err
			}
		}
		if lit, ok := n.L.(*StringLit); ok {
			if r, err := c.check(n.R); err == nil && r.Kind == KindEnum {
				if err := c.enumLiteral(lit, r); err != nil {
					return nil, err
				}
			}
		}
	}
	r, err := c.check(n.R)
	if err != nil {
		return nil, err
	}
	if n.Op == "==" || n.Op == "!=" {
		return c.inferEquality(n, n.L.Type(), r)
	}
	l = n.L.Type()
	switch n.Op {
	case "&&", "||":
		if l.Kind != KindBool || r.Kind != KindBool {
			return nil, c.errorf(n, "%s needs two bools, not %s and %s", n.Op, l, r)
		}
		return typeBool, nil
	case "<", "<=", ">", ">=":
		if !l.Equal(r) || (l.Kind != KindInt && l.Kind != KindDouble) {
			return nil, c.errorf(n, "%s compares two ints or two doubles, not %s and %s%s", n.Op, l, r, c.mixHint(l, r))
		}
		return typeBool, nil
	case "+", "-", "*", "/":
		if !l.Equal(r) || (l.Kind != KindInt && l.Kind != KindDouble) {
			return nil, c.errorf(n, "%s needs two ints or two doubles, not %s and %s%s", n.Op, l, r, c.mixHint(l, r))
		}
		return l, nil
	case "%":
		if l.Kind != KindInt || r.Kind != KindInt {
			return nil, c.errorf(n, "%% needs two ints, not %s and %s", l, r)
		}
		return typeInt, nil
	}
	return nil, c.errorf(n, "unsupported operator %s", n.Op)
}

func (c *checker) mixHint(l, r *Type) string {
	if (l.Kind == KindInt && r.Kind == KindDouble) || (l.Kind == KindDouble && r.Kind == KindInt) {
		return "; convert one side with double(x) or int(x)"
	}
	return ""
}

func (c *checker) enumLiteral(lit *StringLit, enum *Type) error {
	for _, v := range enum.Enum.Values {
		if v.Name == lit.Value {
			lit.setType(enum)
			return nil
		}
	}
	var names []string
	for _, v := range enum.Enum.Values {
		names = append(names, v.Name)
	}
	return c.errorf(lit, "%q is not a value of %s (values: %s)", lit.Value, enum.Enum.Name, strings.Join(names, ", "))
}

func (c *checker) inferEquality(n *Binary, l, r *Type) (*Type, error) {
	if !l.Equal(r) {
		return nil, c.errorf(n, "%s compares values of one type, not %s and %s%s", n.Op, l, r, c.mixHint(l, r))
	}
	switch l.Kind {
	case KindBool, KindInt, KindDouble, KindString, KindEnum:
		return typeBool, nil
	}
	return nil, c.errorf(n, "%s cannot compare %s values; compare their fields instead", n.Op, l)
}

func (c *checker) inferIn(n *Binary, l *Type) (*Type, error) {
	if list, ok := n.R.(*ListLit); ok && l.Kind == KindEnum {
		for _, elem := range list.Elems {
			lit, ok := elem.(*StringLit)
			if !ok {
				return nil, c.errorf(elem, "an enum is matched against string literals naming its values")
			}
			if err := c.enumLiteral(lit, l); err != nil {
				return nil, err
			}
		}
		list.setType(&Type{Kind: KindList, Elem: l})
		return typeBool, nil
	}
	r, err := c.check(n.R)
	if err != nil {
		return nil, err
	}
	switch r.Kind {
	case KindList:
		if !l.Equal(r.Elem) {
			return nil, c.errorf(n, "in looks for a %s in a list of %s", l, r.Elem)
		}
		switch l.Kind {
		case KindBool, KindInt, KindDouble, KindString, KindEnum:
			return typeBool, nil
		}
		return nil, c.errorf(n, "in cannot compare %s values", l)
	case KindMap:
		if l.Kind != KindString {
			return nil, c.errorf(n, "in looks for a string key in a map, not %s", l)
		}
		return typeBool, nil
	}
	return nil, c.errorf(n, "the right side of in must be a list or a map, not %s", r)
}
