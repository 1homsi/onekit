package onkcompile

import (
	"errors"
	"fmt"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func (c *compiler) validateRules(sources []Source) error {
	for _, src := range sources {
		for _, md := range src.AST.Messages {
			if err := c.validateMessageRules(md, src.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *compiler) validateMessageRules(md *onklang.MessageDecl, path string) error {
	m := c.msgNode[md]
	for _, d := range md.Decorators {
		if d.Name != onkexpr.RuleDecorator {
			continue
		}
		if err := checkRuleDecorator(d, path, m, nil); err != nil {
			return err
		}
	}
	for i, fd := range md.Fields {
		for _, d := range fd.Decorators {
			if d.Name != onkexpr.RuleDecorator {
				continue
			}
			if err := checkRuleDecorator(d, path, m, m.Fields[i]); err != nil {
				return err
			}
		}
	}
	for _, nested := range md.Nested {
		if err := c.validateMessageRules(nested, path); err != nil {
			return err
		}
	}
	return nil
}

func checkRuleDecorator(d onklang.Decorator, path string, m *onkir.Message, f *onkir.Field) error {
	source, message := d.Args[0].Value, d.Args[1].Value
	if _, err := onkexpr.CompileRule(source, m, f); err != nil {
		var exprErr *onkexpr.Error
		detail := err.Error()
		if errors.As(err, &exprErr) {
			detail = fmt.Sprintf("%s (column %d of the expression)", exprErr.Message, exprErr.Offset+1)
		}
		return &Error{Path: path, Line: d.Line, Column: d.Col, Code: "invalid_rule", Msg: fmt.Sprintf("invalid @rule %q: %s", source, detail)}
	}
	if len(message) > 200 {
		return &Error{Path: path, Line: d.Line, Column: d.Col, Code: "invalid_rule", Msg: "the @rule message is longer than 200 characters"}
	}
	return nil
}
