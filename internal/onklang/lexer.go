package onklang

import (
	"fmt"
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

func (l *Lexer) advance() byte {
	b := l.src[l.pos]
	l.pos++
	if b == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return b
}

func (l *Lexer) skipWhitespaceAndComments() string {
	var doc []string
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
			for l.pos < len(l.src) && l.peekByte() != '\n' {
				l.advance()
			}
			doc = nil
		case b == '/' && l.peekByteAt(1) == '*':
			l.advance()
			l.advance()
			for l.pos < len(l.src) && !(l.peekByte() == '*' && l.peekByteAt(1) == '/') {
				l.advance()
			}
			if l.pos < len(l.src) {
				l.advance()
				l.advance()
			}
			doc = nil
		default:
			return strings.Join(doc, "\n")
		}
	}
	return strings.Join(doc, "\n")
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
	doc := l.skipWhitespaceAndComments()
	if l.pos >= len(l.src) {
		return Token{Kind: EOF, Line: l.line, Col: l.col, Doc: doc}, nil
	}

	line, col := l.line, l.col
	b := l.peekByte()
	mk := func(k Kind, text string) Token {
		return Token{Kind: k, Text: text, Line: line, Col: col, Doc: doc}
	}

	switch {
	case isIdentStart(b):
		start := l.pos
		for l.pos < len(l.src) && isIdentCont(l.peekByte()) {
			l.advance()
		}
		return mk(IDENT, l.src[start:l.pos]), nil

	case isDigit(b):
		start := l.pos
		isFloat := false
		for l.pos < len(l.src) && isDigit(l.peekByte()) {
			l.advance()
		}
		if l.peekByte() == '.' && isDigit(l.peekByteAt(1)) {
			isFloat = true
			l.advance()
			for l.pos < len(l.src) && isDigit(l.peekByte()) {
				l.advance()
			}
		}
		kind := INT
		if isFloat {
			kind = FLOAT
		}
		return mk(kind, l.src[start:l.pos]), nil

	case b == '"':
		return l.lexString(line, col, doc)

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
			fmt.Errorf("onklang: unexpected character %q at %d:%d", r, line, col)
	}
}

func (l *Lexer) lexString(line, col int, doc string) (Token, error) {
	l.advance()
	var sb strings.Builder
	for {
		if l.pos >= len(l.src) {
			return Token{}, fmt.Errorf("onklang: unterminated string literal starting at %d:%d", line, col)
		}
		b := l.peekByte()
		if b == '"' {
			l.advance()
			break
		}
		if b == '\\' {
			l.advance()
			esc := l.advance()
			switch esc {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				sb.WriteByte(esc)
			}
			continue
		}
		sb.WriteByte(l.advance())
	}
	return Token{Kind: STRING, Text: sb.String(), Line: line, Col: col, Doc: doc}, nil
}
