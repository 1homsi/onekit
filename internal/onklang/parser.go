package onklang

import (
	"fmt"
	"strings"
)

type Parser struct {
	lex  *Lexer
	tok  Token
	prev Token
}

func Parse(src string) (*File, error) {
	p := &Parser{lex: NewLexer(src)}
	if err := p.next(); err != nil {
		return nil, err
	}
	return p.parseFile()
}

func (p *Parser) next() error {
	p.prev = p.tok
	t, err := p.lex.Next()
	if err != nil {
		return err
	}
	p.tok = t
	return nil
}

func (p *Parser) errf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("onklang:%d:%d: %s", p.tok.Line, p.tok.Col, msg)
}

func (p *Parser) expect(k Kind) (Token, error) {
	if p.tok.Kind != k {
		return Token{}, p.errf("expected %s, got %s %q", k, p.tok.Kind, p.tok.Text)
	}
	t := p.tok
	if err := p.next(); err != nil {
		return Token{}, err
	}
	return t, nil
}

func (p *Parser) isIdent(text string) bool {
	return p.tok.Kind == IDENT && p.tok.Text == text
}

func (p *Parser) expectIdentText(text string) error {
	if !p.isIdent(text) {
		return p.errf("expected keyword %q, got %s %q", text, p.tok.Kind, p.tok.Text)
	}
	return p.next()
}

func (p *Parser) parseFile() (*File, error) {
	f := &File{}

	if p.isIdent("package") {
		if err := p.next(); err != nil {
			return nil, err
		}
		pkg, err := p.parseDottedName()
		if err != nil {
			return nil, err
		}
		f.Package = pkg
	}

	for p.isIdent("import") {
		if err := p.next(); err != nil {
			return nil, err
		}
		s, err := p.expect(STRING)
		if err != nil {
			return nil, err
		}
		f.Imports = append(f.Imports, s.Text)
	}

	for p.tok.Kind != EOF {
		switch {
		case p.isIdent("message"):
			m, err := p.parseMessage()
			if err != nil {
				return nil, err
			}
			f.Messages = append(f.Messages, m)
		case p.isIdent("enum"):
			e, err := p.parseEnum()
			if err != nil {
				return nil, err
			}
			f.Enums = append(f.Enums, e)
		case p.isIdent("service"):
			s, err := p.parseService()
			if err != nil {
				return nil, err
			}
			f.Services = append(f.Services, s)
		default:
			return nil, p.errf("expected message/enum/service, got %s %q", p.tok.Kind, p.tok.Text)
		}
	}

	return f, nil
}

func (p *Parser) parseDottedName() (string, error) {
	name, err := p.expect(IDENT)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(name.Text)
	for p.tok.Kind == DOT {
		if err := p.next(); err != nil {
			return "", err
		}
		part, err := p.expect(IDENT)
		if err != nil {
			return "", err
		}
		sb.WriteByte('.')
		sb.WriteString(part.Text)
	}
	return sb.String(), nil
}

func (p *Parser) parseValue() (string, error) {
	switch p.tok.Kind {
	case STRING, IDENT, INT, FLOAT:
		v := p.tok.Text
		return v, p.next()
	default:
		return "", p.errf("expected value, got %s %q", p.tok.Kind, p.tok.Text)
	}
}

func (p *Parser) parseArgs() ([]Arg, error) {
	if p.tok.Kind != LPAREN {
		return nil, nil
	}
	if err := p.next(); err != nil {
		return nil, err
	}
	var args []Arg
	for p.tok.Kind != RPAREN {
		if len(args) > 0 {
			if _, err := p.expect(COMMA); err != nil {
				return nil, err
			}
		}
		if p.tok.Kind == IDENT {
			save := p.tok
			if err := p.next(); err != nil {
				return nil, err
			}
			if p.tok.Kind == COLON {
				if err := p.next(); err != nil {
					return nil, err
				}
				val, err := p.parseValue()
				if err != nil {
					return nil, err
				}
				args = append(args, Arg{Name: save.Text, Value: val})
				continue
			}
			args = append(args, Arg{Value: save.Text})
			continue
		}
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		args = append(args, Arg{Value: val})
	}
	if _, err := p.expect(RPAREN); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *Parser) parseDecorators() ([]Decorator, error) {
	var decorators []Decorator
	for p.tok.Kind == AT {
		line := p.tok.Line
		if err := p.next(); err != nil {
			return nil, err
		}
		name, err := p.expect(IDENT)
		if err != nil {
			return nil, err
		}
		args, err := p.parseArgs()
		if err != nil {
			return nil, err
		}
		decorators = append(decorators, Decorator{Name: name.Text, Args: args, Line: line})
	}
	return decorators, nil
}

func (p *Parser) parseType() (*TypeRef, error) {
	if p.isIdent("map") {
		if err := p.next(); err != nil {
			return nil, err
		}
		if _, err := p.expect(LBRACKET); err != nil {
			return nil, err
		}
		key, err := p.expect(IDENT)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(COMMA); err != nil {
			return nil, err
		}
		val, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(RBRACKET); err != nil {
			return nil, err
		}
		return &TypeRef{IsMap: true, MapKey: key.Text, MapVal: val}, nil
	}
	name, err := p.parseDottedName()
	if err != nil {
		return nil, err
	}
	return &TypeRef{Name: name}, nil
}

func (p *Parser) parseOneofVariant() (OneofVariant, error) {
	name, err := p.expect(IDENT)
	if err != nil {
		return OneofVariant{}, err
	}
	if _, err := p.expect(COLON); err != nil {
		return OneofVariant{}, err
	}
	typ, err := p.parseType()
	if err != nil {
		return OneofVariant{}, err
	}
	decorators, err := p.parseDecorators()
	if err != nil {
		return OneofVariant{}, err
	}
	return OneofVariant{Name: name.Text, Type: typ, Decorators: decorators, Line: name.Line}, nil
}

func (p *Parser) parseOneof() (*OneofDecl, error) {
	line := p.prev.Line
	args, err := p.parseArgs()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(LBRACE); err != nil {
		return nil, err
	}
	o := &OneofDecl{Args: args, Line: line}
	for p.tok.Kind != RBRACE {
		v, err := p.parseOneofVariant()
		if err != nil {
			return nil, err
		}
		o.Variants = append(o.Variants, v)
	}
	if _, err := p.expect(RBRACE); err != nil {
		return nil, err
	}
	return o, nil
}

func (p *Parser) parseField() (*FieldDecl, error) {
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	f := &FieldDecl{Name: name.Text, Doc: name.Doc, Line: name.Line}

	if _, err := p.expect(COLON); err != nil {
		return nil, err
	}

	if p.isIdent("oneof") {
		if err := p.next(); err != nil {
			return nil, err
		}
		oneof, err := p.parseOneof()
		if err != nil {
			return nil, err
		}
		f.Oneof = oneof
		return f, nil
	}

	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}
	f.Type = typ

	if p.tok.Kind == QUESTION {
		f.Optional = true
		if err := p.next(); err != nil {
			return nil, err
		}
	} else if p.tok.Kind == LBRACKET {
		if err := p.next(); err != nil {
			return nil, err
		}
		if _, err := p.expect(RBRACKET); err != nil {
			return nil, err
		}
		f.Repeated = true
	}

	decorators, err := p.parseDecorators()
	if err != nil {
		return nil, err
	}
	f.Decorators = decorators

	return f, nil
}

func (p *Parser) parseMessage() (*MessageDecl, error) {
	doc := p.tok.Doc
	if err := p.expectIdentText("message"); err != nil {
		return nil, err
	}
	line := p.prev.Line
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	m := &MessageDecl{Name: name.Text, Doc: doc, Line: line}

	decorators, err := p.parseDecorators()
	if err != nil {
		return nil, err
	}
	m.Decorators = decorators

	if _, err := p.expect(LBRACE); err != nil {
		return nil, err
	}
	for p.tok.Kind != RBRACE {
		switch {
		case p.isIdent("message"):
			nested, err := p.parseMessage()
			if err != nil {
				return nil, err
			}
			m.Nested = append(m.Nested, nested)
		case p.isIdent("enum"):
			nested, err := p.parseEnum()
			if err != nil {
				return nil, err
			}
			m.NestedEn = append(m.NestedEn, nested)
		default:
			field, err := p.parseField()
			if err != nil {
				return nil, err
			}
			m.Fields = append(m.Fields, field)
		}
	}
	if _, err := p.expect(RBRACE); err != nil {
		return nil, err
	}
	return m, nil
}

func (p *Parser) parseEnum() (*EnumDecl, error) {
	doc := p.tok.Doc
	if err := p.expectIdentText("enum"); err != nil {
		return nil, err
	}
	line := p.prev.Line
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	e := &EnumDecl{Name: name.Text, Doc: doc, Line: line}

	if _, err := p.expect(LBRACE); err != nil {
		return nil, err
	}
	for p.tok.Kind != RBRACE {
		vname, err := p.expect(IDENT)
		if err != nil {
			return nil, err
		}
		decorators, err := p.parseDecorators()
		if err != nil {
			return nil, err
		}
		e.Values = append(e.Values, EnumValueDecl{Name: vname.Text, Doc: vname.Doc, Decorators: decorators, Line: vname.Line})
	}
	if _, err := p.expect(RBRACE); err != nil {
		return nil, err
	}
	return e, nil
}

func (p *Parser) parseHeadersBlock() ([]HeaderDecl, error) {
	if err := p.expectIdentText("headers"); err != nil {
		return nil, err
	}
	if _, err := p.expect(COLON); err != nil {
		return nil, err
	}
	if _, err := p.expect(LBRACE); err != nil {
		return nil, err
	}
	var headers []HeaderDecl
	for p.tok.Kind != RBRACE {
		nameTok, err := p.expect(STRING)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(COLON); err != nil {
			return nil, err
		}
		typeTok, err := p.expect(IDENT)
		if err != nil {
			return nil, err
		}
		decorators, err := p.parseDecorators()
		if err != nil {
			return nil, err
		}
		headers = append(headers, HeaderDecl{
			Name:       nameTok.Text,
			Type:       typeTok.Text,
			Decorators: decorators,
			Line:       nameTok.Line,
		})
	}
	if _, err := p.expect(RBRACE); err != nil {
		return nil, err
	}
	return headers, nil
}

func (p *Parser) parseRPC() (*RPCDecl, error) {
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	r := &RPCDecl{Name: name.Text, Doc: name.Doc, Line: name.Line}

	if _, err := p.expect(LPAREN); err != nil {
		return nil, err
	}
	req, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	r.RequestType = req.Text
	if _, err := p.expect(RPAREN); err != nil {
		return nil, err
	}
	if _, err := p.expect(ARROW); err != nil {
		return nil, err
	}
	resp, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	r.ResponseType = resp.Text

	for p.tok.Kind == PIPE {
		if err := p.next(); err != nil {
			return nil, err
		}
		errType, err := p.expect(IDENT)
		if err != nil {
			return nil, err
		}
		r.ErrorTypes = append(r.ErrorTypes, errType.Text)
	}

	decorators, err := p.parseDecorators()
	if err != nil {
		return nil, err
	}
	r.Decorators = decorators

	if p.tok.Kind == LBRACE {
		if err := p.next(); err != nil {
			return nil, err
		}
		for p.tok.Kind != RBRACE {
			switch {
			case p.isIdent("headers"):
				h, err := p.parseHeadersBlock()
				if err != nil {
					return nil, err
				}
				r.Headers = h
			default:
				return nil, p.errf("unexpected token in rpc body: %s %q", p.tok.Kind, p.tok.Text)
			}
		}
		if _, err := p.expect(RBRACE); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func (p *Parser) parseService() (*ServiceDecl, error) {
	doc := p.tok.Doc
	if err := p.expectIdentText("service"); err != nil {
		return nil, err
	}
	line := p.prev.Line
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	s := &ServiceDecl{Name: name.Text, Doc: doc, Line: line}

	if _, err := p.expect(LBRACE); err != nil {
		return nil, err
	}
	for p.tok.Kind != RBRACE {
		switch {
		case p.isIdent("base_path"):
			if err := p.next(); err != nil {
				return nil, err
			}
			if _, err := p.expect(COLON); err != nil {
				return nil, err
			}
			path, err := p.expect(STRING)
			if err != nil {
				return nil, err
			}
			s.BasePath = path.Text
		case p.isIdent("headers"):
			h, err := p.parseHeadersBlock()
			if err != nil {
				return nil, err
			}
			s.Headers = h
		case p.tok.Kind == IDENT:
			r, err := p.parseRPC()
			if err != nil {
				return nil, err
			}
			s.RPCs = append(s.RPCs, r)
		default:
			return nil, p.errf("unexpected token in service body: %s %q", p.tok.Kind, p.tok.Text)
		}
	}
	if _, err := p.expect(RBRACE); err != nil {
		return nil, err
	}
	return s, nil
}
