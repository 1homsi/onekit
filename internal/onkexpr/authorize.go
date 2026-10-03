package onkexpr

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

const (
	AuthBinding    = "auth"
	RequestBinding = "req"
)

type AuthRule struct {
	Source  string
	Message string
	Expr    Node
}

func AuthEnv(principal, request *onkir.Message) map[string]*Type {
	return map[string]*Type{
		AuthBinding:    MessageType(principal),
		RequestBinding: MessageType(request),
	}
}

func CompileAuthorize(src string, principal, request *onkir.Message) (Node, error) {
	return CheckRule(src, AuthEnv(principal, request))
}

func AuthRulesFor(m *onkir.Method) ([]AuthRule, error) {
	decorators := m.AuthorizeRules()
	if len(decorators) == 0 {
		return nil, nil
	}
	if m.Principal == nil {
		return nil, fmt.Errorf("@authorize on %s needs a message marked @principal", m.Name)
	}
	out := make([]AuthRule, 0, len(decorators))
	for _, d := range decorators {
		if len(d.Args) != 2 {
			return nil, fmt.Errorf("@authorize on %s expects an expression and a message", m.Name)
		}
		node, err := CompileAuthorize(d.Args[0].Value, m.Principal, m.Request)
		if err != nil {
			return nil, fmt.Errorf("@authorize on %s: %w", m.Name, err)
		}
		out = append(out, AuthRule{Source: d.Args[0].Value, Message: d.Args[1].Value, Expr: node})
	}
	return out, nil
}
