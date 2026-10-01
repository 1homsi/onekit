package onkexpr

import (
	"fmt"
	"math"
	"strconv"
)

type parser struct {
	tokens []token
	pos    int
	depth  int
}

func Parse(src string) (Node, error) {
	tokens, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	node, err := p.parseTernary()
	if err != nil {
		return nil, err
	}
	if tok := p.peek(); tok.kind != tokEOF {
		return nil, p.unexpected(tok)
	}
	return node, nil
}

func (p *parser) peek() token { return p.tokens[p.pos] }

func (p *parser) next() token {
	tok := p.tokens[p.pos]
	if tok.kind != tokEOF {
		p.pos++
	}
	return tok
}

func (p *parser) isPunct(text string) bool {
	tok := p.peek()
	return tok.kind == tokPunct && tok.text == text
}

func (p *parser) unexpected(tok token) error {
	if tok.kind == tokEOF {
		return &Error{Offset: tok.pos, Message: "unexpected end of expression"}
	}
	return &Error{Offset: tok.pos, Message: fmt.Sprintf("unexpected %q", describeToken(tok))}
}

func describeToken(tok token) string {
	if tok.kind == tokString {
		return strconv.Quote(tok.text)
	}
	return tok.text
}

func (p *parser) expectPunct(text string) error {
	tok := p.next()
	if tok.kind != tokPunct || tok.text != text {
		if tok.kind == tokEOF {
			return &Error{Offset: tok.pos, Message: fmt.Sprintf("expected %q but the expression ended", text)}
		}
		return &Error{Offset: tok.pos, Message: fmt.Sprintf("expected %q, got %q", text, describeToken(tok))}
	}
	return nil
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > maxDepth {
		return &Error{Offset: p.peek().pos, Message: "expression is nested too deeply"}
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

func (p *parser) parseTernary() (Node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	cond, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.isPunct("?") {
		return cond, nil
	}
	pos := p.next().pos
	a, err := p.parseTernary()
	if err != nil {
		return nil, err
	}
	if err := p.expectPunct(":"); err != nil {
		return nil, err
	}
	b, err := p.parseTernary()
	if err != nil {
		return nil, err
	}
	return &Ternary{base: base{Pos: Pos{pos}}, Cond: cond, A: a, B: b}, nil
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isPunct("||") {
		pos := p.next().pos
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Binary{base: base{Pos: Pos{pos}}, Op: "||", L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseRelation()
	if err != nil {
		return nil, err
	}
	for p.isPunct("&&") {
		pos := p.next().pos
		right, err := p.parseRelation()
		if err != nil {
			return nil, err
		}
		left = &Binary{base: base{Pos: Pos{pos}}, Op: "&&", L: left, R: right}
	}
	return left, nil
}

var relationOps = map[string]bool{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

func (p *parser) parseRelation() (Node, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	tok := p.peek()
	var op string
	switch {
	case tok.kind == tokPunct && relationOps[tok.text]:
		op = tok.text
	case tok.kind == tokIdent && tok.text == "in":
		op = "in"
	default:
		return left, nil
	}
	p.next()
	right, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	node := &Binary{base: base{Pos: Pos{tok.pos}}, Op: op, L: left, R: right}
	if next := p.peek(); (next.kind == tokPunct && relationOps[next.text]) || (next.kind == tokIdent && next.text == "in") {
		return nil, &Error{Offset: next.pos, Message: "comparisons cannot be chained; join them with && or ||"}
	}
	return node, nil
}

func (p *parser) parseAdd() (Node, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.isPunct("+") || p.isPunct("-") {
		tok := p.next()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &Binary{base: base{Pos: Pos{tok.pos}}, Op: tok.text, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseMul() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.isPunct("*") || p.isPunct("/") || p.isPunct("%") {
		tok := p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &Binary{base: base{Pos: Pos{tok.pos}}, Op: tok.text, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (Node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	if p.isPunct("!") || p.isPunct("-") {
		tok := p.next()
		if tok.text == "-" && p.peek().kind == tokInt && p.peek().text == "9223372036854775808" {
			p.next()
			return &IntLit{base: base{Pos: Pos{tok.pos}}, Value: math.MinInt64}, nil
		}
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{base: base{Pos: Pos{tok.pos}}, Op: tok.text, X: operand}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (Node, error) {
	node, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.isPunct("."):
			p.next()
			name := p.next()
			if name.kind != tokIdent {
				return nil, p.unexpected(name)
			}
			if p.isPunct("(") {
				node, err = p.parseMethod(node, name)
				if err != nil {
					return nil, err
				}
				continue
			}
			node = &Select{base: base{Pos: Pos{name.pos}}, X: node, Name: name.text}
		case p.isPunct("["):
			open := p.next()
			idx, err := p.parseTernary()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct("]"); err != nil {
				return nil, err
			}
			node = &Index{base: base{Pos: Pos{open.pos}}, X: node, Idx: idx}
		default:
			return node, nil
		}
	}
}

func (p *parser) parseMethod(recv Node, name token) (Node, error) {
	p.next()
	if name.text == macroAll || name.text == macroExists {
		variable := p.next()
		if variable.kind != tokIdent {
			return nil, &Error{Offset: variable.pos, Message: fmt.Sprintf("%s() needs a variable name first, as in %s(item, item > 0)", name.text, name.text)}
		}
		if err := p.expectPunct(","); err != nil {
			return nil, err
		}
		body, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		return &Macro{base: base{Pos: Pos{name.pos}}, Kind: name.text, Range: recv, Var: variable.text, Body: body}, nil
	}
	args, err := p.parseArgs()
	if err != nil {
		return nil, err
	}
	return &Call{base: base{Pos: Pos{name.pos}}, Fn: name.text, Args: append([]Node{recv}, args...)}, nil
}

func (p *parser) parseArgs() ([]Node, error) {
	var args []Node
	if p.isPunct(")") {
		p.next()
		return args, nil
	}
	for {
		arg, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.isPunct(",") {
			p.next()
			continue
		}
		return args, p.expectPunct(")")
	}
}

func (p *parser) parsePrimary() (Node, error) {
	tok := p.next()
	switch tok.kind {
	case tokInt:
		value, err := strconv.ParseInt(tok.text, 10, 64)
		if err != nil {
			return nil, &Error{Offset: tok.pos, Message: fmt.Sprintf("integer %s does not fit in 64 bits", tok.text)}
		}
		return &IntLit{base: base{Pos: Pos{tok.pos}}, Value: value}, nil
	case tokDouble:
		value, err := strconv.ParseFloat(tok.text, 64)
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			return nil, &Error{Offset: tok.pos, Message: fmt.Sprintf("number %s is out of range", tok.text)}
		}
		return &DoubleLit{base: base{Pos: Pos{tok.pos}}, Value: value}, nil
	case tokString:
		return &StringLit{base: base{Pos: Pos{tok.pos}}, Value: tok.text}, nil
	case tokIdent:
		switch tok.text {
		case "true", "false":
			return &BoolLit{base: base{Pos: Pos{tok.pos}}, Value: tok.text == "true"}, nil
		}
		if p.isPunct("(") {
			p.next()
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			return &Call{base: base{Pos: Pos{tok.pos}}, Fn: tok.text, Args: args}, nil
		}
		return &Ident{base: base{Pos: Pos{tok.pos}}, Name: tok.text}, nil
	case tokPunct:
		switch tok.text {
		case "(":
			inner, err := p.parseTernary()
			if err != nil {
				return nil, err
			}
			return inner, p.expectPunct(")")
		case "[":
			list := &ListLit{base: base{Pos: Pos{tok.pos}}}
			if p.isPunct("]") {
				p.next()
				return list, nil
			}
			for {
				elem, err := p.parseTernary()
				if err != nil {
					return nil, err
				}
				list.Elems = append(list.Elems, elem)
				if p.isPunct(",") {
					p.next()
					continue
				}
				return list, p.expectPunct("]")
			}
		}
	}
	return nil, p.unexpected(tok)
}
