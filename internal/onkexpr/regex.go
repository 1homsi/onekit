package onkexpr

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	maxRegexBytes = 256
	maxRegexDepth = 8
	maxRepeat     = 1000
)

func ValidateRegex(re string) error {
	if re == "" {
		return errors.New("the pattern is empty")
	}
	if len(re) > maxRegexBytes {
		return fmt.Errorf("the pattern is longer than %d bytes", maxRegexBytes)
	}
	if !utf8.ValidString(re) {
		return errors.New("the pattern is not valid UTF-8")
	}
	p := &regexParser{src: []rune(re)}
	if _, err := p.alternation(0); err != nil {
		return err
	}
	if p.pos < len(p.src) {
		return p.errorf("unmatched ')'")
	}
	return nil
}

type regexParser struct {
	src []rune
	pos int
}

func (p *regexParser) errorf(format string, args ...any) error {
	return fmt.Errorf("at position %d: %s", p.pos+1, fmt.Sprintf(format, args...))
}

type regexInfo struct {
	repeats     bool
	alternation bool
}

func (p *regexParser) alternation(depth int) (regexInfo, error) {
	if depth > maxRegexDepth {
		return regexInfo{}, p.errorf("groups are nested too deeply")
	}
	var info regexInfo
	for {
		branch, err := p.concat(depth)
		if err != nil {
			return info, err
		}
		info.repeats = info.repeats || branch.repeats
		info.alternation = info.alternation || branch.alternation
		if p.pos < len(p.src) && p.src[p.pos] == '|' {
			info.alternation = true
			p.pos++
			continue
		}
		return info, nil
	}
}

func (p *regexParser) concat(depth int) (regexInfo, error) {
	var info regexInfo
	count := 0
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '|' || c == ')' {
			break
		}
		atom, err := p.atom(depth)
		if err != nil {
			return info, err
		}
		count++
		quant, err := p.quantifier()
		if err != nil {
			return info, err
		}
		if quant == quantRepeat {
			if atom.repeats {
				return info, p.errorf("nested repetition is not allowed")
			}
			if atom.alternation {
				return info, p.errorf("repeating a group that contains '|' is not allowed; use a bracket class instead")
			}
			info.repeats = true
		}
		info.repeats = info.repeats || atom.repeats
		info.alternation = info.alternation || atom.alternation
	}
	if count == 0 {
		return info, p.errorf("empty alternative")
	}
	return info, nil
}

func (p *regexParser) atom(depth int) (regexInfo, error) {
	c := p.src[p.pos]
	switch c {
	case '(':
		p.pos++
		if p.pos < len(p.src) && p.src[p.pos] == '?' {
			if p.pos+1 < len(p.src) && p.src[p.pos+1] == ':' {
				p.pos += 2
			} else {
				return regexInfo{}, p.errorf("only (?:...) is allowed after '(?'")
			}
		}
		info, err := p.alternation(depth + 1)
		if err != nil {
			return info, err
		}
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return info, p.errorf("missing ')'")
		}
		p.pos++
		return info, nil
	case '[':
		return regexInfo{}, p.class()
	case '\\':
		_, err := p.escape()
		return regexInfo{}, err
	case '.':
		return regexInfo{}, p.errorf("'.' is not portable; use a bracket class such as [^\\n]")
	case '^', '$':
		return regexInfo{}, p.errorf("anchors are not allowed; matches() always matches the whole string")
	case '*', '+', '?', '{':
		return regexInfo{}, p.errorf("%q has nothing to repeat", string(c))
	case '}', ']':
		return regexInfo{}, p.errorf("escape %q with a backslash to match it literally", string(c))
	}
	p.pos++
	return regexInfo{}, nil
}

func (p *regexParser) escape() (rune, error) {
	p.pos++
	if p.pos >= len(p.src) {
		return 0, p.errorf("a pattern cannot end with a backslash")
	}
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 'n':
		return '\n', nil
	case 't':
		return '\t', nil
	case 'r':
		return '\r', nil
	case '\\', '.', '^', '$', '|', '?', '*', '+', '(', ')', '[', ']', '{', '}', '-', '/':
		return c, nil
	}
	return 0, p.errorf("\\%c is not portable; shorthand classes such as \\d, \\w and \\s differ between languages, so spell them out as [0-9] or [A-Za-z0-9_]", c)
}

func (p *regexParser) class() error {
	p.pos++
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		p.pos++
	}
	items := 0
	for {
		if p.pos >= len(p.src) {
			return p.errorf("missing ']'")
		}
		if p.src[p.pos] == ']' {
			if items == 0 {
				return p.errorf("empty bracket class")
			}
			p.pos++
			return nil
		}
		low, err := p.classChar()
		if err != nil {
			return err
		}
		items++
		if p.pos+1 < len(p.src) && p.src[p.pos] == '-' && p.src[p.pos+1] != ']' {
			p.pos++
			high, err := p.classChar()
			if err != nil {
				return err
			}
			if low > high {
				return p.errorf("the range %q-%q is backwards", string(low), string(high))
			}
		}
	}
}

func (p *regexParser) classChar() (rune, error) {
	c := p.src[p.pos]
	switch c {
	case '\\':
		return p.escape()
	case '[':
		return 0, p.errorf("nested brackets and POSIX classes are not allowed")
	case '-':
		return 0, p.errorf("write a literal '-' as \\-")
	case '^':
		return 0, p.errorf("write a literal '^' inside a class as \\^")
	}
	p.pos++
	return c, nil
}

const (
	quantNone = iota
	quantOptional
	quantRepeat
)

func (p *regexParser) quantifier() (int, error) {
	if p.pos >= len(p.src) {
		return quantNone, nil
	}
	quant := quantRepeat
	switch p.src[p.pos] {
	case '?':
		quant = quantOptional
		p.pos++
	case '*', '+':
		p.pos++
	case '{':
		if err := p.bounds(); err != nil {
			return quantNone, err
		}
	default:
		return quantNone, nil
	}
	if p.pos < len(p.src) {
		switch p.src[p.pos] {
		case '?':
			return quantNone, p.errorf("lazy quantifiers are not allowed")
		case '+':
			return quantNone, p.errorf("possessive quantifiers are not allowed")
		case '*', '{':
			return quantNone, p.errorf("nested repetition is not allowed")
		}
	}
	return quant, nil
}

func (p *regexParser) bounds() error {
	p.pos++
	low, ok := p.number()
	if !ok {
		return p.errorf("expected a number after '{'")
	}
	high := low
	if p.pos < len(p.src) && p.src[p.pos] == ',' {
		p.pos++
		var ok bool
		high, ok = p.number()
		if !ok {
			return p.errorf("open-ended {n,} is not allowed; use + or * or give an upper bound")
		}
	}
	if p.pos >= len(p.src) || p.src[p.pos] != '}' {
		return p.errorf("missing '}'")
	}
	p.pos++
	if low > high || high > maxRepeat {
		return p.errorf("repetition bounds must satisfy n <= m <= %d", maxRepeat)
	}
	return nil
}

func (p *regexParser) number() (int, bool) {
	start := p.pos
	value := 0
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		value = value*10 + int(p.src[p.pos]-'0')
		if value > maxRepeat*10 {
			value = maxRepeat * 10
		}
		p.pos++
	}
	return value, p.pos > start
}
