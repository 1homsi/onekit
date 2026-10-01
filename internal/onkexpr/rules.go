package onkexpr

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

const RuleDecorator = "rule"

type Rule struct {
	Source  string
	Message string
	Expr    Node
	Field   *onkir.Field
}

func Env(m *onkir.Message, f *onkir.Field) (map[string]*Type, error) {
	env := map[string]*Type{"self": MessageType(m)}
	if f != nil {
		t, err := FieldType(f)
		if err != nil {
			return nil, err
		}
		env["value"] = t
	}
	return env, nil
}

func CompileRule(src string, m *onkir.Message, f *onkir.Field) (Node, error) {
	env, err := Env(m, f)
	if err != nil {
		return nil, &Error{Message: err.Error()}
	}
	return CheckRule(src, env)
}

func RulesFor(m *onkir.Message) ([]Rule, error) {
	var out []Rule
	add := func(decorators []onkir.Decorator, f *onkir.Field) error {
		for _, d := range decorators {
			if d.Name != RuleDecorator {
				continue
			}
			if len(d.Args) != 2 {
				return fmt.Errorf("@rule on %s expects an expression and a message", m.Name)
			}
			node, err := CompileRule(d.Args[0].Value, m, f)
			if err != nil {
				return fmt.Errorf("@rule on %s: %w", m.Name, err)
			}
			out = append(out, Rule{Source: d.Args[0].Value, Message: d.Args[1].Value, Expr: node, Field: f})
		}
		return nil
	}
	if err := add(m.Decorators, nil); err != nil {
		return nil, err
	}
	for _, f := range m.Fields {
		if err := add(f.Decorators, f); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func Violations(m *onkir.Message, value map[string]any) ([]string, error) {
	rules, err := RulesFor(m)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rules {
		scope := Scope{"self": value}
		if r.Field != nil {
			if v, ok := value[r.Field.Name]; ok {
				scope["value"] = v
			} else {
				ft, ferr := FieldType(r.Field)
				if ferr != nil {
					return nil, ferr
				}
				scope["value"] = ZeroValue(ft)
			}
		}
		ok, evalErr := EvalBool(r.Expr, scope)
		if evalErr != nil || !ok {
			out = append(out, r.Message)
		}
	}
	return out, nil
}
