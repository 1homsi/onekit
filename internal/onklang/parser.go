package onklang

import (
	"fmt"
	"strings"
)

const maxParserNestingDepth = 64

type Parser struct {
	lex          *Lexer
	tok          Token
	prev         Token
	nestingDepth int
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

func (p *Parser) unclosed(open Token, what string) error {
	return &Error{Line: p.tok.Line, Column: p.tok.Col, Message: fmt.Sprintf("missing } for %s opened at %d:%d", what, open.Line, open.Col)}
}

func (p *Parser) errf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return &Error{Line: p.tok.Line, Column: p.tok.Col, Message: msg}
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

func (p *Parser) enterNesting(kind string) error {
	if p.nestingDepth >= maxParserNestingDepth {
		return p.errf("maximum %s nesting depth of %d exceeded", kind, maxParserNestingDepth)
	}
	p.nestingDepth++
	return nil
}

func (p *Parser) leaveNesting() {
	p.nestingDepth--
}

// isKeywordIntroducer reports whether the current token is the identifier
// text used as a keyword (e.g. "message", "enum") introducing a declaration,
// as opposed to a field literally named "message"/"enum" (field syntax is
// `name: Type`, so a following COLON means it's a field name, not a
// keyword - checked via a cheap lexer clone since Lexer holds no pointers).
func (p *Parser) isKeywordIntroducer(text string) bool {
	if !p.isIdent(text) {
		return false
	}
	clone := *p.lex
	next, err := clone.Next()
	if err != nil {
		return true
	}
	return next.Kind != COLON
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
		f.LeadingComments = append([]string(nil), p.tok.LeadingComments...)
		f.PackageDoc = p.tok.Doc
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
		f.ImportComments = append(f.ImportComments, append([]string(nil), p.tok.LeadingComments...))
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
			f.Declarations = append(f.Declarations, m)
		case p.isIdent("enum"):
			e, err := p.parseEnum()
			if err != nil {
				return nil, err
			}
			f.Enums = append(f.Enums, e)
			f.Declarations = append(f.Declarations, e)
		case p.isIdent("service"):
			s, err := p.parseService()
			if err != nil {
				return nil, err
			}
			f.Services = append(f.Services, s)
			f.Declarations = append(f.Declarations, s)
		default:
			return nil, p.errf("expected message/enum/service, got %s %q", p.tok.Kind, p.tok.Text)
		}
	}
	f.TrailingComments = closingComments(p.tok)

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

func (p *Parser) parseValue() (string, bool, error) {
	switch p.tok.Kind {
	case STRING, IDENT, INT, FLOAT:
		v := p.tok.Text
		quoted := p.tok.Kind == STRING
		return v, quoted, p.next()
	default:
		return "", false, p.errf("expected value, got %s %q", p.tok.Kind, p.tok.Text)
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
			if p.tok.Kind == RPAREN {
				return nil, p.errf("trailing comma in decorator arguments")
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
				val, quoted, err := p.parseValue()
				if err != nil {
					return nil, err
				}
				args = append(args, Arg{Name: save.Text, Value: val, Quoted: quoted})
				continue
			}
			args = append(args, Arg{Value: save.Text})
			continue
		}
		val, quoted, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		args = append(args, Arg{Value: val, Quoted: quoted})
	}
	if _, err := p.expect(RPAREN); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *Parser) parseDecorators() ([]Decorator, error) {
	var decorators []Decorator
	for p.tok.Kind == AT {
		line, col := p.tok.Line, p.tok.Col
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
		decorators = append(decorators, Decorator{Name: name.Text, Args: args, Line: line, Col: col})
	}
	return decorators, nil
}

func (p *Parser) parseType() (*TypeRef, error) {
	if err := p.enterNesting("type"); err != nil {
		return nil, err
	}
	defer p.leaveNesting()
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
	start := p.tok
	name, err := p.parseDottedName()
	if err != nil {
		return nil, err
	}
	return &TypeRef{Name: name, Span: tokenSpan(start, p.prev)}, nil
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
	return OneofVariant{Name: name.Text, LeadingComments: append([]string(nil), name.LeadingComments...), Type: typ, Decorators: decorators, Line: name.Line, Col: name.Col}, nil
}

func (p *Parser) parseOneof() (*OneofDecl, error) {
	line := p.prev.Line
	args, err := p.parseArgs()
	if err != nil {
		return nil, err
	}
	open, err := p.expect(LBRACE)
	if err != nil {
		return nil, err
	}
	o := &OneofDecl{LeadingComments: append([]string(nil), p.prev.LeadingComments...), Args: args, Line: line, Col: p.prev.Col}
	for p.tok.Kind != RBRACE {
		if p.tok.Kind == EOF {
			return nil, p.unclosed(open, "oneof")
		}
		v, err := p.parseOneofVariant()
		if err != nil {
			return nil, err
		}
		o.Variants = append(o.Variants, v)
	}
	closing, err := p.expect(RBRACE)
	if err != nil {
		return nil, err
	}
	o.TrailingComments = closingComments(closing)
	return o, nil
}

func (p *Parser) parseField() (*FieldDecl, error) {
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	f := &FieldDecl{Name: name.Text, Doc: name.Doc, LeadingComments: append([]string(nil), name.LeadingComments...), Line: name.Line, Col: name.Col}

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
		f.Decorators = nil
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
	if err := p.enterNesting("message"); err != nil {
		return nil, err
	}
	defer p.leaveNesting()
	doc := p.tok.Doc
	// Capture the leading comments from the declaration keyword before it is
	// consumed: by the time the name has been read, p.prev is the name token,
	// whose leading comments are always empty.
	leading := append([]string(nil), p.tok.LeadingComments...)
	if err := p.expectIdentText("message"); err != nil {
		return nil, err
	}
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	m := &MessageDecl{Name: name.Text, Doc: doc, LeadingComments: leading, Line: name.Line, Col: name.Col}

	decorators, err := p.parseDecorators()
	if err != nil {
		return nil, err
	}
	m.Decorators = decorators

	open, err := p.expect(LBRACE)
	if err != nil {
		return nil, err
	}
	for p.tok.Kind != RBRACE {
		if p.tok.Kind == EOF {
			return nil, p.unclosed(open, "message "+m.Name)
		}
		switch {
		case p.isKeywordIntroducer("message"):
			nested, err := p.parseMessage()
			if err != nil {
				return nil, err
			}
			m.Nested = append(m.Nested, nested)
			m.Members = append(m.Members, nested)
		case p.isKeywordIntroducer("enum"):
			nested, err := p.parseEnum()
			if err != nil {
				return nil, err
			}
			m.NestedEn = append(m.NestedEn, nested)
			m.Members = append(m.Members, nested)
		default:
			field, err := p.parseField()
			if err != nil {
				return nil, err
			}
			m.Fields = append(m.Fields, field)
			m.Members = append(m.Members, field)
		}
	}
	closing, err := p.expect(RBRACE)
	if err != nil {
		return nil, err
	}
	m.TrailingComments = closingComments(closing)
	return m, nil
}

func (p *Parser) parseEnum() (*EnumDecl, error) {
	doc := p.tok.Doc
	// Capture the leading comments from the declaration keyword before it is
	// consumed: by the time the name has been read, p.prev is the name token,
	// whose leading comments are always empty.
	leading := append([]string(nil), p.tok.LeadingComments...)
	if err := p.expectIdentText("enum"); err != nil {
		return nil, err
	}
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	e := &EnumDecl{Name: name.Text, Doc: doc, LeadingComments: leading, Line: name.Line, Col: name.Col}

	open, err := p.expect(LBRACE)
	if err != nil {
		return nil, err
	}
	for p.tok.Kind != RBRACE {
		if p.tok.Kind == EOF {
			return nil, p.unclosed(open, "enum "+e.Name)
		}
		vname, err := p.expect(IDENT)
		if err != nil {
			return nil, err
		}
		decorators, err := p.parseDecorators()
		if err != nil {
			return nil, err
		}
		e.Values = append(e.Values, EnumValueDecl{Name: vname.Text, Doc: vname.Doc, LeadingComments: append([]string(nil), vname.LeadingComments...), Decorators: decorators, Line: vname.Line, Col: vname.Col})
	}
	closing, err := p.expect(RBRACE)
	if err != nil {
		return nil, err
	}
	e.TrailingComments = closingComments(closing)
	return e, nil
}

func (p *Parser) parseHeadersBlock() ([]HeaderDecl, []string, error) {
	if err := p.expectIdentText("headers"); err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(COLON); err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(LBRACE); err != nil {
		return nil, nil, err
	}
	var headers []HeaderDecl
	for p.tok.Kind != RBRACE {
		nameTok, err := p.expect(STRING)
		if err != nil {
			return nil, nil, err
		}
		if _, err := p.expect(COLON); err != nil {
			return nil, nil, err
		}
		typeTok, err := p.expect(IDENT)
		if err != nil {
			return nil, nil, err
		}
		decorators, err := p.parseDecorators()
		if err != nil {
			return nil, nil, err
		}
		headers = append(headers, HeaderDecl{
			Name:            nameTok.Text,
			LeadingComments: append([]string(nil), nameTok.LeadingComments...),
			Type:            typeTok.Text,
			Decorators:      decorators,
			Line:            nameTok.Line,
			Col:             nameTok.Col,
		})
	}
	closing, err := p.expect(RBRACE)
	if err != nil {
		return nil, nil, err
	}
	return headers, closingComments(closing), nil
}

func (p *Parser) parseRPC() (*RPCDecl, error) {
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	r := &RPCDecl{Name: name.Text, Doc: name.Doc, LeadingComments: append([]string(nil), name.LeadingComments...), Line: name.Line, Col: name.Col}

	if _, err := p.expect(LPAREN); err != nil {
		return nil, err
	}
	start := p.tok
	req, err := p.parseDottedName()
	if err != nil {
		return nil, err
	}
	r.RequestType = req
	r.RequestSpan = tokenSpan(start, p.prev)
	if _, err := p.expect(RPAREN); err != nil {
		return nil, err
	}
	if _, err := p.expect(ARROW); err != nil {
		return nil, err
	}
	start = p.tok
	resp, err := p.parseDottedName()
	if err != nil {
		return nil, err
	}
	r.ResponseType = resp
	r.ResponseSpan = tokenSpan(start, p.prev)

	for p.tok.Kind == PIPE {
		if err := p.next(); err != nil {
			return nil, err
		}
		start = p.tok
		errType, err := p.parseDottedName()
		if err != nil {
			return nil, err
		}
		r.ErrorTypes = append(r.ErrorTypes, errType)
		r.ErrorSpans = append(r.ErrorSpans, tokenSpan(start, p.prev))
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
		seenHeaders := false
		for p.tok.Kind != RBRACE {
			switch {
			case p.isIdent("headers"):
				if seenHeaders {
					return nil, p.errf("duplicate headers block in rpc %s", r.Name)
				}
				seenHeaders = true
				r.HeadersComments = append([]string(nil), p.tok.LeadingComments...)
				h, trailing, err := p.parseHeadersBlock()
				if err != nil {
					return nil, err
				}
				r.Headers, r.HeadersTrailingComments = h, trailing
			default:
				return nil, p.errf("unexpected token in rpc body: %s %q", p.tok.Kind, p.tok.Text)
			}
		}
		closing, err := p.expect(RBRACE)
		if err != nil {
			return nil, err
		}
		r.TrailingComments = closingComments(closing)
	}

	return r, nil
}

func (p *Parser) parseService() (*ServiceDecl, error) {
	doc := p.tok.Doc
	// Capture the leading comments from the declaration keyword before it is
	// consumed: by the time the name has been read, p.prev is the name token,
	// whose leading comments are always empty.
	leading := append([]string(nil), p.tok.LeadingComments...)
	if err := p.expectIdentText("service"); err != nil {
		return nil, err
	}
	name, err := p.expect(IDENT)
	if err != nil {
		return nil, err
	}
	s := &ServiceDecl{Name: name.Text, Doc: doc, LeadingComments: leading, Line: name.Line, Col: name.Col}

	open, err := p.expect(LBRACE)
	if err != nil {
		return nil, err
	}
	seenBasePath, seenHeaders := false, false
	for p.tok.Kind != RBRACE {
		if p.tok.Kind == EOF {
			return nil, p.unclosed(open, "service "+s.Name)
		}
		switch {
		case p.isIdent("base_path"):
			if seenBasePath {
				return nil, p.errf("duplicate base_path in service %s", s.Name)
			}
			seenBasePath = true
			s.BasePathComments = append([]string(nil), p.tok.LeadingComments...)
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
			if seenHeaders {
				return nil, p.errf("duplicate headers block in service %s", s.Name)
			}
			seenHeaders = true
			s.HeadersComments = append([]string(nil), p.tok.LeadingComments...)
			h, trailing, err := p.parseHeadersBlock()
			if err != nil {
				return nil, err
			}
			s.Headers, s.HeadersTrailingComments = h, trailing
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
	closing, err := p.expect(RBRACE)
	if err != nil {
		return nil, err
	}
	s.TrailingComments = closingComments(closing)
	return s, nil
}

// closingComments returns the comments written just before a closing `}` or
// EOF, which no declaration follows to own; a stray `///` line keeps its doc
// marker so formatting reproduces it rather than dropping it.
func closingComments(tok Token) []string {
	comments := append([]string(nil), tok.LeadingComments...)
	if tok.Doc != "" {
		for _, line := range strings.Split(tok.Doc, "\n") {
			comments = append(comments, strings.TrimSpace("/// "+line))
		}
	}
	return comments
}

func tokenSpan(start, end Token) Span {
	return Span{Line: start.Line, Col: start.Col, EndLine: end.Line, EndCol: end.Col + len(end.Text)}
}
