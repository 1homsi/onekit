package onkexpr

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokInt
	tokDouble
	tokString
	tokPunct
)

type token struct {
	kind tokKind
	text string
	pos  int
}

const (
	MaxSourceBytes = 1024
	maxDepth       = 48
)

func lex(src string) ([]token, error) {
	if len(src) > MaxSourceBytes {
		return nil, &Error{Message: fmt.Sprintf("expression is longer than %d bytes", MaxSourceBytes)}
	}
	var tokens []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			tokens = append(tokens, token{kind: tokIdent, text: src[i:j], pos: i})
			i = j
		case c >= '0' && c <= '9':
			text, kind := scanNumber(src[i:])
			tokens = append(tokens, token{kind: kind, text: text, pos: i})
			i += len(text)
		case c == '\'' || c == '"':
			value, next, err := scanString(src, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token{kind: tokString, text: value, pos: i})
			i = next
		default:
			punct := scanPunct(src[i:])
			if punct == "" {
				r, _ := utf8.DecodeRuneInString(src[i:])
				return nil, &Error{Offset: i, Message: fmt.Sprintf("unexpected character %q", r)}
			}
			tokens = append(tokens, token{kind: tokPunct, text: punct, pos: i})
			i += len(punct)
		}
	}
	tokens = append(tokens, token{kind: tokEOF, pos: len(src)})
	return tokens, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func scanNumber(src string) (string, tokKind) {
	i := 0
	for i < len(src) && src[i] >= '0' && src[i] <= '9' {
		i++
	}
	kind := tokInt
	if i+1 < len(src) && src[i] == '.' && src[i+1] >= '0' && src[i+1] <= '9' {
		kind = tokDouble
		i++
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
	}
	if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
		j := i + 1
		if j < len(src) && (src[j] == '+' || src[j] == '-') {
			j++
		}
		if j < len(src) && src[j] >= '0' && src[j] <= '9' {
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			kind = tokDouble
			i = j
		}
	}
	return src[:i], kind
}

var puncts = []string{"||", "&&", "==", "!=", "<=", ">=", "<", ">", "+", "-", "*", "/", "%", "!", "?", ":", ".", ",", "(", ")", "[", "]"}

func scanPunct(src string) string {
	for _, p := range puncts {
		if strings.HasPrefix(src, p) {
			return p
		}
	}
	return ""
}

func scanString(src string, start int) (string, int, error) {
	quote := src[start]
	var out strings.Builder
	i := start + 1
	for i < len(src) {
		c := src[i]
		switch c {
		case quote:
			return out.String(), i + 1, nil
		case '\n':
			return "", 0, &Error{Offset: start, Message: "unterminated string"}
		case '\\':
			if i+1 >= len(src) {
				return "", 0, &Error{Offset: start, Message: "unterminated string"}
			}
			i++
			switch src[i] {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case 'r':
				out.WriteByte('\r')
			case '\\', '\'', '"':
				out.WriteByte(src[i])
			case 'u':
				if i+4 >= len(src) {
					return "", 0, &Error{Offset: i, Message: "\\u needs four hex digits"}
				}
				code, ok := hexRune(src[i+1 : i+5])
				if !ok || (code >= 0xD800 && code <= 0xDFFF) {
					return "", 0, &Error{Offset: i, Message: "invalid \\u escape"}
				}
				out.WriteRune(code)
				i += 4
			default:
				return "", 0, &Error{Offset: i, Message: fmt.Sprintf("unknown escape \\%c", src[i])}
			}
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return "", 0, &Error{Offset: start, Message: "unterminated string"}
}

func hexRune(digits string) (rune, bool) {
	var code rune
	for _, c := range []byte(digits) {
		var v rune
		switch {
		case c >= '0' && c <= '9':
			v = rune(c - '0')
		case c >= 'a' && c <= 'f':
			v = rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = rune(c-'A') + 10
		default:
			return 0, false
		}
		code = code*16 + v
	}
	return code, true
}
