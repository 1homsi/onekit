package onkimport

import (
	"fmt"
	"strings"
)

type protoTokenKind int

const (
	protoEOF protoTokenKind = iota
	protoIdent
	protoInt
	protoFloat
	protoString
	protoPunct
)

type protoToken struct {
	kind protoTokenKind
	text string
	line int
	doc  string
}

type protoLexer struct {
	src         string
	pos         int
	line        int
	pending     []string
	blanks      int
	lastComment bool
	tokens      []protoToken
}

func lexProto(src string) ([]protoToken, error) {
	l := &protoLexer{src: src, line: 1}
	for l.pos < len(l.src) {
		skipped, err := l.trivia()
		if err != nil {
			return nil, err
		}
		if skipped {
			continue
		}
		l.lastComment = false
		if err := l.token(); err != nil {
			return nil, err
		}
	}
	l.tokens = append(l.tokens, protoToken{kind: protoEOF, line: l.line})
	return l.tokens, nil
}

func (l *protoLexer) attachDoc() string {
	if len(l.pending) == 0 || l.blanks > 0 {
		l.pending = nil
		return ""
	}
	doc := strings.Join(l.pending, "\n")
	l.pending = nil
	return doc
}

func (l *protoLexer) addComment(lines ...string) {
	if len(l.pending) > 0 && l.blanks > 0 {
		l.pending = nil
	}
	l.pending = append(l.pending, lines...)
	l.blanks = 0
	l.lastComment = true
}

func (l *protoLexer) trivia() (bool, error) {
	c := l.src[l.pos]
	switch {
	case c == '\n':
		l.line++
		l.pos++
		if l.lastComment {
			l.lastComment = false
		} else {
			l.blanks++
		}
		return true, nil
	case c == ' ' || c == '\t' || c == '\r':
		l.pos++
		return true, nil
	case strings.HasPrefix(l.src[l.pos:], "//"):
		end := strings.IndexByte(l.src[l.pos:], '\n')
		if end < 0 {
			end = len(l.src) - l.pos
		}
		l.addComment(strings.TrimSpace(strings.TrimLeft(l.src[l.pos+2:l.pos+end], "/")))
		l.pos += end
		return true, nil
	case strings.HasPrefix(l.src[l.pos:], "/*"):
		end := strings.Index(l.src[l.pos+2:], "*/")
		if end < 0 {
			return false, fmt.Errorf("proto: line %d: unterminated block comment", l.line)
		}
		body := l.src[l.pos+2 : l.pos+2+end]
		var lines []string
		for _, part := range strings.Split(body, "\n") {
			if part = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(part), "*")); part != "" {
				lines = append(lines, part)
			}
		}
		l.addComment(lines...)
		l.line += strings.Count(body, "\n")
		l.pos += end + 4
		return true, nil
	}
	return false, nil
}

func (l *protoLexer) emit(kind protoTokenKind, text string, line int) {
	l.tokens = append(l.tokens, protoToken{kind: kind, text: text, line: line, doc: l.attachDoc()})
	l.blanks = 0
}

func (l *protoLexer) token() error {
	c := l.src[l.pos]
	start := l.line
	switch {
	case isProtoIdentStart(c):
		end := l.pos
		for end < len(l.src) && isProtoIdentPart(l.src[end]) {
			end++
		}
		l.emit(protoIdent, l.src[l.pos:end], start)
		l.pos = end
	case c >= '0' && c <= '9':
		text, isFloat := lexProtoNumber(l.src[l.pos:])
		kind := protoInt
		if isFloat {
			kind = protoFloat
		}
		l.emit(kind, text, start)
		l.pos += len(text)
	case c == '"' || c == '\'':
		value, next, err := lexProtoString(l.src, l.pos, l.line)
		if err != nil {
			return err
		}
		l.emit(protoString, value, start)
		l.pos = next
	case strings.ContainsRune("{}[]()<>=;,:.-+/", rune(c)):
		l.emit(protoPunct, string(c), start)
		l.pos++
	default:
		return fmt.Errorf("proto: line %d: unexpected character %q", l.line, string(c))
	}
	return nil
}

func lexProtoNumber(src string) (string, bool) {
	hex := strings.HasPrefix(src, "0x") || strings.HasPrefix(src, "0X")
	isFloat := false
	end := 0
	for end < len(src) {
		c := src[end]
		signInExponent := (c == '+' || c == '-') && end > 0 && (src[end-1] == 'e' || src[end-1] == 'E') && !hex
		if !isProtoIdentPart(c) && c != '.' && !signInExponent {
			break
		}
		if !hex && (c == '.' || c == 'e' || c == 'E') {
			isFloat = true
		}
		end++
	}
	return src[:end], isFloat
}

func isProtoIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isProtoIdentPart(c byte) bool {
	return isProtoIdentStart(c) || (c >= '0' && c <= '9')
}

func lexProtoString(src string, start, line int) (string, int, error) {
	quote := src[start]
	var out strings.Builder
	i := start + 1
	for i < len(src) {
		c := src[i]
		switch {
		case c == quote:
			return out.String(), i + 1, nil
		case c == '\n':
			return "", 0, fmt.Errorf("proto: line %d: unterminated string", line)
		case c == '\\' && i+1 < len(src):
			i++
			switch src[i] {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case 'r':
				out.WriteByte('\r')
			default:
				out.WriteByte(src[i])
			}
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return "", 0, fmt.Errorf("proto: line %d: unterminated string", line)
}
