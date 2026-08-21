package onklang

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Lexer struct {
	src  string
	pos  int
	line int
	col  int
}

func NewLexer(src string) *Lexer {
	return &Lexer{src: src, line: 1, col: 1}
}

func (l *Lexer) peekByte() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peekByteAt(off int) byte {
	if l.pos+off >= len(l.src) {
		return 0
	}
	return l.src[l.pos+off]
}

func (l *Lexer) advance() {
	b := l.src[l.pos]
	l.pos++
	if b == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
}

func (l *Lexer) skipWhitespaceAndComments() (string, []string, error) {
	var doc []string
	var comments []string
	for l.pos < len(l.src) {
		b := l.peekByte()
		switch {
		case b == ' ' || b == '\t' || b == '\r' || b == '\n':
			l.advance()
		case b == '/' && l.peekByteAt(1) == '/' && l.peekByteAt(2) == '/':
			l.advance()
			l.advance()
			l.advance()
			start := l.pos
			for l.pos < len(l.src) && l.peekByte() != '\n' {
				l.advance()
			}
			doc = append(doc, strings.TrimSpace(l.src[start:l.pos]))
		case b == '/' && l.peekByteAt(1) == '/':
			start := l.pos
			for l.pos < len(l.src) && l.peekByte() != '\n' {
				l.advance()
			}
			comments = append(comments, strings.TrimSpace(l.src[start:l.pos]))
			doc = nil
		case b == '/' && l.peekByteAt(1) == '*':
			start := l.pos
			l.advance()
			l.advance()
			for l.pos < len(l.src) && !(l.peekByte() == '*' && l.peekByteAt(1) == '/') {
				l.advance()
			}
			if l.pos >= len(l.src) {
				return "", nil, &Error{Line: l.line, Column: l.col, Message: "unterminated block comment"}
			}
			l.advance()
			l.advance()
			comments = append(comments, strings.TrimSpace(l.src[start:l.pos]))
			doc = nil
		default:
			return strings.Join(doc, "\n"), comments, nil
		}
	}
	return strings.Join(doc, "\n"), comments, nil
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentCont(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func (l *Lexer) Next() (Token, error) {
	doc, comments, err := l.skipWhitespaceAndComments()
	if err != nil {
		return Token{}, err
	}
	if l.pos >= len(l.src) {
		// Comments preceding EOF must survive so onek fmt can round-trip
		// them as trailing comments instead of silently dropping content.
		return Token{Kind: EOF, Line: l.line, Col: l.col, Doc: doc, LeadingComments: comments}, nil
	}

	line, col := l.line, l.col
	b := l.peekByte()
	mk := func(k Kind, text string) Token {
		return Token{Kind: k, Text: text, Line: line, Col: col, Doc: doc, LeadingComments: comments}
	}

	switch {
	case isIdentStart(b):
		start := l.pos
		for l.pos < len(l.src) && isIdentCont(l.peekByte()) {
			l.advance()
		}
		return mk(IDENT, l.src[start:l.pos]), nil

	case isDigit(b) || ((b == '-' || b == '+') && isDigit(l.peekByteAt(1))):
		return l.lexNumber(line, col, doc, comments)

	case b == '"':
		return l.lexString(line, col, doc, comments)

	case b == '{':
		l.advance()
		return mk(LBRACE, "{"), nil
	case b == '}':
		l.advance()
		return mk(RBRACE, "}"), nil
	case b == '[':
		l.advance()
		return mk(LBRACKET, "["), nil
	case b == ']':
		l.advance()
		return mk(RBRACKET, "]"), nil
	case b == '(':
		l.advance()
		return mk(LPAREN, "("), nil
	case b == ')':
		l.advance()
		return mk(RPAREN, ")"), nil
	case b == '@':
		l.advance()
		return mk(AT, "@"), nil
	case b == '?':
		l.advance()
		return mk(QUESTION, "?"), nil
	case b == '|':
		l.advance()
		return mk(PIPE, "|"), nil
	case b == ',':
		l.advance()
		return mk(COMMA, ","), nil
	case b == ':':
		l.advance()
		return mk(COLON, ":"), nil
	case b == '.':
		l.advance()
		return mk(DOT, "."), nil
	case b == '-' && l.peekByteAt(1) == '>':
		l.advance()
		l.advance()
		return mk(ARROW, "->"), nil

	default:
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += size
		return mk(ILLEGAL, string(r)),
			&Error{Line: line, Column: col, Message: fmt.Sprintf("unexpected character %q", r)}
	}
}

func (l *Lexer) lexNumber(line, col int, doc string, comments []string) (Token, error) {
	start := l.pos
	if l.peekByte() == '-' || l.peekByte() == '+' {
		l.advance()
	}
	for l.pos < len(l.src) && isDigit(l.peekByte()) {
		l.advance()
	}
	isFloat := false
	if l.peekByte() == '.' && isDigit(l.peekByteAt(1)) {
		isFloat = true
		l.advance()
		for l.pos < len(l.src) && isDigit(l.peekByte()) {
			l.advance()
		}
	}
	if l.peekByte() == 'e' || l.peekByte() == 'E' {
		isFloat = true
		l.advance()
		if l.peekByte() == '-' || l.peekByte() == '+' {
			l.advance()
		}
		if !isDigit(l.peekByte()) {
			return Token{}, &Error{Line: line, Column: col, Message: "invalid exponent"}
		}
		for l.pos < len(l.src) && isDigit(l.peekByte()) {
			l.advance()
		}
	}
	text := l.src[start:l.pos]
	if isIdentStart(l.peekByte()) {
		// Consume the glued identifier so the reported span covers the whole
		// malformed token (e.g. "123abc" or "0x1F") instead of failing later
		// with a confusing parser error about a stray identifier.
		for l.pos < len(l.src) && isIdentCont(l.peekByte()) {
			l.advance()
		}
		return Token{}, &Error{Line: line, Column: col, Message: fmt.Sprintf(
			"invalid number %q (numeric literals may not be followed by identifier characters)",
			l.src[start:l.pos],
		)}
	}
	if isFloat {
		if _, err := strconv.ParseFloat(text, 64); err != nil {
			return Token{}, &Error{Line: line, Column: col, Message: fmt.Sprintf("invalid number %q", text)}
		}
		return Token{Kind: FLOAT, Text: text, Line: line, Col: col, Doc: doc, LeadingComments: comments}, nil
	}
	if _, err := strconv.ParseInt(text, 10, 64); err != nil {
		return Token{}, &Error{Line: line, Column: col, Message: fmt.Sprintf("invalid integer %q", text)}
	}
	return Token{Kind: INT, Text: text, Line: line, Col: col, Doc: doc, LeadingComments: comments}, nil
}

func (l *Lexer) lexString(line, col int, doc string, comments []string) (Token, error) {
	start := l.pos
	l.advance()
	for {
		if l.pos >= len(l.src) {
			return Token{}, &Error{Line: line, Column: col, Message: "unterminated string literal"}
		}
		b := l.peekByte()
		if b == '"' {
			l.advance()
			break
		}
		if b == '\\' {
			l.advance()
			if l.pos >= len(l.src) {
				return Token{}, &Error{Line: line, Column: col, Message: "unterminated string literal"}
			}
			l.advance()
			continue
		}
		if b == '\n' || b == '\r' {
			return Token{}, &Error{Line: line, Column: col, Message: "newline in string literal"}
		}
		l.advance()
	}
	raw := l.src[start:l.pos]
	value, err := strconv.Unquote(raw)
	if err != nil {
		return Token{}, &Error{Line: line, Column: col, Message: fmt.Sprintf("invalid string literal: %v", err)}
	}
	return Token{Kind: STRING, Text: value, Line: line, Col: col, Doc: doc, LeadingComments: comments}, nil
}
