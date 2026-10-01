package onkexpr

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/1homsi/onekit/internal/onkir"
)

type EvalError struct{ Message string }

func (e *EvalError) Error() string { return e.Message }

func evalErr(format string, args ...any) error {
	return &EvalError{Message: fmt.Sprintf(format, args...)}
}

type Scope map[string]any

func Eval(n Node, scope Scope) (any, error) {
	e := &evaluator{vars: scope}
	return e.eval(n)
}

func EvalBool(n Node, scope Scope) (bool, error) {
	v, err := Eval(n, scope)
	if err != nil {
		return false, err
	}
	b, _ := v.(bool)
	return b, nil
}

type evaluator struct {
	vars Scope
}

func (e *evaluator) eval(n Node) (any, error) {
	switch n := n.(type) {
	case *IntLit:
		return n.Value, nil
	case *DoubleLit:
		return n.Value, nil
	case *StringLit:
		return n.Value, nil
	case *BoolLit:
		return n.Value, nil
	case *ListLit:
		out := make([]any, 0, len(n.Elems))
		for _, elem := range n.Elems {
			v, err := e.eval(elem)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case *Ident:
		return e.vars[n.Name], nil
	case *Select:
		return e.evalSelect(n)
	case *Index:
		return e.evalIndex(n)
	case *Call:
		return e.evalCall(n)
	case *Macro:
		return e.evalMacro(n)
	case *Unary:
		return e.evalUnary(n)
	case *Binary:
		return e.evalBinary(n)
	case *Ternary:
		cond, err := e.eval(n.Cond)
		if err != nil {
			return nil, err
		}
		if asBool(cond) {
			return e.eval(n.A)
		}
		return e.eval(n.B)
	}
	return nil, evalErr("unsupported expression")
}

func (e *evaluator) evalSelect(n *Select) (any, error) {
	x, err := e.eval(n.X)
	if err != nil {
		return nil, err
	}
	msg, _ := x.(map[string]any)
	if v, ok := msg[n.Name]; ok {
		return v, nil
	}
	return ZeroValue(n.Type()), nil
}

func ZeroValue(t *Type) any {
	switch t.Kind {
	case KindBool:
		return false
	case KindInt:
		return int64(0)
	case KindDouble:
		return float64(0)
	case KindString:
		return ""
	case KindBytes:
		return []byte{}
	case KindList:
		return []any{}
	case KindMap, KindMessage:
		return map[string]any{}
	case KindEnum:
		return t.Enum.Values[0].Name
	}
	return nil
}

func (e *evaluator) evalIndex(n *Index) (any, error) {
	x, err := e.eval(n.X)
	if err != nil {
		return nil, err
	}
	idx, err := e.eval(n.Idx)
	if err != nil {
		return nil, err
	}
	switch c := x.(type) {
	case []any:
		i := asInt(idx)
		if i < 0 || i >= int64(len(c)) {
			return nil, evalErr("index %d is out of range for a list of %d", i, len(c))
		}
		return c[i], nil
	case map[string]any:
		key := asString(idx)
		v, ok := c[key]
		if !ok {
			return nil, evalErr("no such key %q", key)
		}
		return v, nil
	}
	return nil, evalErr("cannot index this value")
}

func (e *evaluator) evalMacro(n *Macro) (any, error) {
	r, err := e.eval(n.Range)
	if err != nil {
		return nil, err
	}
	list, _ := r.([]any)
	saved, had := e.vars[n.Var]
	defer func() {
		if had {
			e.vars[n.Var] = saved
		} else {
			delete(e.vars, n.Var)
		}
	}()
	if e.vars == nil {
		e.vars = Scope{}
	}
	for _, item := range list {
		e.vars[n.Var] = item
		v, err := e.eval(n.Body)
		if err != nil {
			return nil, err
		}
		if n.Kind == macroAll && !asBool(v) {
			return false, nil
		}
		if n.Kind == macroExists && asBool(v) {
			return true, nil
		}
	}
	return n.Kind == macroAll, nil
}

func (e *evaluator) evalUnary(n *Unary) (any, error) {
	x, err := e.eval(n.X)
	if err != nil {
		return nil, err
	}
	if n.Op == "!" {
		return !asBool(x), nil
	}
	switch v := x.(type) {
	case int64:
		if v == math.MinInt64 {
			return nil, evalErr("integer overflow")
		}
		return -v, nil
	case float64:
		return -v, nil
	}
	return nil, evalErr("cannot negate this value")
}

func (e *evaluator) evalBinary(n *Binary) (any, error) {
	if n.Op == "&&" || n.Op == "||" {
		l, err := e.eval(n.L)
		if err != nil {
			return nil, err
		}
		if n.Op == "&&" && !asBool(l) {
			return false, nil
		}
		if n.Op == "||" && asBool(l) {
			return true, nil
		}
		return e.eval(n.R)
	}
	l, err := e.eval(n.L)
	if err != nil {
		return nil, err
	}
	r, err := e.eval(n.R)
	if err != nil {
		return nil, err
	}
	switch n.Op {
	case "==":
		return equal(l, r), nil
	case "!=":
		return !equal(l, r), nil
	case "in":
		return contains(r, l), nil
	case "<", "<=", ">", ">=":
		return compare(n.Op, l, r), nil
	}
	return arithmetic(n.Op, l, r)
}

func equal(l, r any) bool {
	return l == r
}

func contains(container, item any) bool {
	switch c := container.(type) {
	case []any:
		for _, v := range c {
			if v == item {
				return true
			}
		}
	case map[string]any:
		_, ok := c[asString(item)]
		return ok
	}
	return false
}

func compare(op string, l, r any) bool {
	var cmp int
	switch a := l.(type) {
	case int64:
		b := asInt(r)
		switch {
		case a < b:
			cmp = -1
		case a > b:
			cmp = 1
		}
	case float64:
		b := asFloat(r)
		switch {
		case a < b:
			cmp = -1
		case a > b:
			cmp = 1
		}
	}
	switch op {
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	}
	return cmp >= 0
}

func arithmetic(op string, l, r any) (any, error) {
	if a, ok := l.(int64); ok {
		return intArithmetic(op, a, asInt(r))
	}
	a, b := asFloat(l), asFloat(r)
	var out float64
	switch op {
	case "+":
		out = a + b
	case "-":
		out = a - b
	case "*":
		out = a * b
	case "/":
		if b == 0 {
			return nil, evalErr("division by zero")
		}
		out = a / b
	}
	if math.IsInf(out, 0) || math.IsNaN(out) {
		return nil, evalErr("the result is not a finite number")
	}
	return out, nil
}

func intArithmetic(op string, a, b int64) (any, error) {
	switch op {
	case "+":
		sum := a + b
		if (sum > a) != (b > 0) {
			return nil, evalErr("integer overflow")
		}
		return sum, nil
	case "-":
		diff := a - b
		if (diff < a) != (b > 0) {
			return nil, evalErr("integer overflow")
		}
		return diff, nil
	case "*":
		if a == 0 || b == 0 {
			return int64(0), nil
		}
		product := a * b
		if product/b != a || (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
			return nil, evalErr("integer overflow")
		}
		return product, nil
	case "/", "%":
		if b == 0 {
			return nil, evalErr("division by zero")
		}
		if a == math.MinInt64 && b == -1 {
			return nil, evalErr("integer overflow")
		}
		if op == "/" {
			return a / b, nil
		}
		return a % b, nil
	}
	return nil, evalErr("unsupported operator %s", op)
}

func (e *evaluator) evalCall(n *Call) (any, error) {
	if n.Fn == "has" {
		sel, ok := n.Args[0].(*Select)
		if !ok {
			return nil, evalErr("has() needs a field")
		}
		return e.evalHas(sel)
	}
	args := make([]any, len(n.Args))
	for i, arg := range n.Args {
		v, err := e.eval(arg)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	switch n.Fn {
	case "size":
		return sizeOf(args[0]), nil
	case fnInt:
		return toInt(args[0])
	case fnDouble:
		if i, ok := args[0].(int64); ok {
			return float64(i), nil
		}
		return args[0], nil
	case "startsWith":
		return strings.HasPrefix(asString(args[0]), asString(args[1])), nil
	case "endsWith":
		return strings.HasSuffix(asString(args[0]), asString(args[1])), nil
	case "contains":
		return strings.Contains(asString(args[0]), asString(args[1])), nil
	case "matches":
		return fullMatch(n.Regex, asString(args[0])), nil
	}
	return nil, evalErr("unknown function %s", n.Fn)
}

var regexCache = map[string]*regexp.Regexp{}

func fullMatch(pattern, s string) bool {
	re, ok := regexCache[pattern]
	if !ok {
		re = regexp.MustCompile(`\A(?:` + pattern + `)\z`)
		regexCache[pattern] = re
	}
	return re.MatchString(s)
}

func sizeOf(v any) int64 {
	switch c := v.(type) {
	case string:
		return int64(utf8.RuneCountInString(c))
	case []byte:
		return int64(len(c))
	case []any:
		return int64(len(c))
	case map[string]any:
		return int64(len(c))
	}
	return 0
}

func toInt(v any) (any, error) {
	switch c := v.(type) {
	case int64:
		return c, nil
	case float64:
		if math.IsNaN(c) || math.IsInf(c, 0) || c >= 9223372036854775808.0 || c < -9223372036854775808.0 {
			return nil, evalErr("the double does not fit in an int")
		}
		return int64(c), nil
	}
	return nil, evalErr("cannot convert to int")
}

func (e *evaluator) evalHas(sel *Select) (any, error) {
	x, err := e.eval(sel.X)
	if err != nil {
		return nil, err
	}
	msg, _ := x.(map[string]any)
	v, present := msg[sel.Name]
	if !present {
		return false, nil
	}
	f := sel.Field
	if f.Optional || (f.Type != nil && f.Type.Kind == onkir.KindMessage && !f.Repeated) {
		return true, nil
	}
	switch c := v.(type) {
	case []any:
		return len(c) > 0, nil
	case map[string]any:
		return len(c) > 0, nil
	case []byte:
		return len(c) > 0, nil
	}
	return v != ZeroValue(sel.Type()), nil
}

func FromJSON(t *Type, raw any) (any, error) {
	switch t.Kind {
	case KindBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, errors.New("expected a bool")
		}
		return b, nil
	case KindInt:
		switch c := raw.(type) {
		case float64:
			return int64(c), nil
		case string:
			return strconv.ParseInt(c, 10, 64)
		}
		return nil, errors.New("expected an integer")
	case KindDouble:
		switch c := raw.(type) {
		case float64:
			return c, nil
		case string:
			return strconv.ParseFloat(c, 64)
		}
		return nil, errors.New("expected a number")
	case KindString:
		s, ok := raw.(string)
		if !ok {
			return nil, errors.New("expected a string")
		}
		return s, nil
	case KindBytes:
		s, ok := raw.(string)
		if !ok {
			return nil, errors.New("expected base64")
		}
		return base64.StdEncoding.DecodeString(s)
	case KindEnum:
		s, ok := raw.(string)
		if !ok {
			return nil, errors.New("expected an enum name")
		}
		for _, v := range t.Enum.Values {
			if v.JSONName() == s || v.Name == s {
				return v.Name, nil
			}
		}
		return nil, fmt.Errorf("unknown enum value %q", s)
	case KindList:
		items, ok := raw.([]any)
		if !ok {
			return nil, errors.New("expected a list")
		}
		out := make([]any, len(items))
		for i, item := range items {
			v, err := FromJSON(t.Elem, item)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case KindMap:
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("expected an object")
		}
		out := map[string]any{}
		for k, item := range obj {
			v, err := FromJSON(t.Elem, item)
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	case KindMessage:
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("expected an object")
		}
		out := map[string]any{}
		for _, f := range t.Message.Fields {
			item, present := obj[f.Name]
			if !present || f.Oneof != nil {
				continue
			}
			ft, err := FieldType(f)
			if err != nil {
				continue
			}
			v, err := FromJSON(ft, item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			out[f.Name] = v
		}
		return out, nil
	}
	return nil, errors.New("unsupported type")
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func asInt(v any) int64 {
	i, _ := v.(int64)
	return i
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
