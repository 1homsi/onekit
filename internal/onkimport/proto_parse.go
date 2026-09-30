package onkimport

import (
	"fmt"
	"strings"
)

const (
	protoOption     = "option"
	protoDeprecated = "deprecated"
	protoTrue       = "true"
)

type protoFile struct {
	syntax   string
	pkg      string
	imports  []string
	messages []*protoMessage
	enums    []*protoEnum
	services []*protoService
}

type protoMessage struct {
	name       string
	doc        string
	deprecated bool
	fields     []*protoField
	nested     []*protoMessage
	enums      []*protoEnum
}

type protoField struct {
	name       string
	doc        string
	typ        string
	repeated   bool
	optional   bool
	oneof      string
	mapKey     string
	mapValue   string
	deprecated bool
	jsonName   string
}

type protoEnum struct {
	name       string
	doc        string
	deprecated bool
	values     []*protoEnumValue
}

type protoEnumValue struct {
	name       string
	number     string
	doc        string
	deprecated bool
}

type protoService struct {
	name    string
	doc     string
	methods []*protoMethod
}

type protoHTTPRule struct {
	verb string
	path string
	body string
}

type protoMethod struct {
	name         string
	doc          string
	request      string
	response     string
	clientStream bool
	serverStream bool
	deprecated   bool
	http         *protoHTTPRule
}

type protoParser struct {
	tokens []protoToken
	pos    int
}

func parseProto(src string) (*protoFile, error) {
	tokens, err := lexProto(src)
	if err != nil {
		return nil, err
	}
	p := &protoParser{tokens: tokens}
	file := &protoFile{}
	for p.peek().kind != protoEOF {
		if err := p.parseTopLevel(file); err != nil {
			return nil, err
		}
	}
	return file, nil
}

func (p *protoParser) peek() protoToken { return p.tokens[p.pos] }

func (p *protoParser) next() protoToken {
	tok := p.tokens[p.pos]
	if tok.kind != protoEOF {
		p.pos++
	}
	return tok
}

func (p *protoParser) errorf(tok protoToken, format string, args ...any) error {
	return fmt.Errorf("proto: line %d: %s", tok.line, fmt.Sprintf(format, args...))
}

func (p *protoParser) expectPunct(text string) error {
	tok := p.next()
	if tok.kind != protoPunct || tok.text != text {
		return p.errorf(tok, "expected %q, got %s", text, describeProtoToken(tok))
	}
	return nil
}

func (p *protoParser) isPunct(text string) bool {
	tok := p.peek()
	return tok.kind == protoPunct && tok.text == text
}

func (p *protoParser) acceptPunct(text string) bool {
	if p.isPunct(text) {
		p.pos++
		return true
	}
	return false
}

func (p *protoParser) expectIdent() (protoToken, error) {
	tok := p.next()
	if tok.kind != protoIdent {
		return tok, p.errorf(tok, "expected an identifier, got %s", describeProtoToken(tok))
	}
	return tok, nil
}

func describeProtoToken(tok protoToken) string {
	if tok.kind == protoEOF {
		return "end of file"
	}
	return fmt.Sprintf("%q", tok.text)
}

func (p *protoParser) fullIdent() (string, error) {
	var parts []string
	if p.acceptPunct(".") {
		parts = append(parts, "")
	}
	tok, err := p.expectIdent()
	if err != nil {
		return "", err
	}
	parts = append(parts, tok.text)
	for p.isPunct(".") && p.tokens[p.pos+1].kind == protoIdent {
		p.pos++
		parts = append(parts, p.next().text)
	}
	return strings.Join(parts, "."), nil
}

func (p *protoParser) parseTopLevel(file *protoFile) error {
	tok := p.peek()
	if tok.kind == protoPunct && tok.text == ";" {
		p.pos++
		return nil
	}
	if tok.kind != protoIdent {
		return p.errorf(tok, "unexpected %s", describeProtoToken(tok))
	}
	switch tok.text {
	case "syntax", "edition":
		p.pos++
		if err := p.expectPunct("="); err != nil {
			return err
		}
		value := p.next()
		file.syntax = value.text
		return p.expectPunct(";")
	case "package":
		p.pos++
		name, err := p.fullIdent()
		if err != nil {
			return err
		}
		file.pkg = name
		return p.expectPunct(";")
	case "import":
		p.pos++
		if p.peek().kind == protoIdent && (p.peek().text == "public" || p.peek().text == "weak") {
			p.pos++
		}
		path := p.next()
		if path.kind != protoString {
			return p.errorf(path, "expected an import path string")
		}
		file.imports = append(file.imports, path.text)
		return p.expectPunct(";")
	case protoOption:
		return p.skipStatement()
	case "message":
		message, err := p.parseMessage()
		if err != nil {
			return err
		}
		file.messages = append(file.messages, message)
		return nil
	case "enum":
		enum, err := p.parseEnum()
		if err != nil {
			return err
		}
		file.enums = append(file.enums, enum)
		return nil
	case "service":
		service, err := p.parseService()
		if err != nil {
			return err
		}
		file.services = append(file.services, service)
		return nil
	case "extend":
		return p.skipBlockStatement()
	}
	return p.errorf(tok, "unexpected %q at the top level", tok.text)
}

func (p *protoParser) skipStatement() error {
	depth := 0
	for {
		tok := p.next()
		switch {
		case tok.kind == protoEOF:
			return p.errorf(tok, "unexpected end of file in a statement")
		case tok.kind == protoPunct && tok.text == "{":
			depth++
		case tok.kind == protoPunct && tok.text == "}":
			depth--
		case tok.kind == protoPunct && tok.text == ";" && depth <= 0:
			return nil
		}
	}
}

func (p *protoParser) skipBlockStatement() error {
	for {
		tok := p.next()
		if tok.kind == protoEOF {
			return p.errorf(tok, "unexpected end of file")
		}
		if tok.kind == protoPunct && tok.text == "{" {
			return p.skipBlockBody()
		}
	}
}

func (p *protoParser) skipBlockBody() error {
	depth := 1
	for depth > 0 {
		tok := p.next()
		switch {
		case tok.kind == protoEOF:
			return p.errorf(tok, "unexpected end of file in a block")
		case tok.kind == protoPunct && tok.text == "{":
			depth++
		case tok.kind == protoPunct && tok.text == "}":
			depth--
		}
	}
	return nil
}

func (p *protoParser) parseMessage() (*protoMessage, error) {
	keyword := p.next()
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	message := &protoMessage{name: name.text, doc: keyword.doc}
	if err := p.expectPunct("{"); err != nil {
		return nil, err
	}
	for !p.isPunct("}") {
		if p.peek().kind == protoEOF {
			return nil, p.errorf(p.peek(), "unterminated message %s", message.name)
		}
		if err := p.parseMessageItem(message, ""); err != nil {
			return nil, err
		}
	}
	p.pos++
	return message, nil
}

func (p *protoParser) parseMessageItem(message *protoMessage, oneof string) error {
	tok := p.peek()
	if tok.kind == protoPunct && tok.text == ";" {
		p.pos++
		return nil
	}
	if tok.kind != protoIdent {
		return p.errorf(tok, "unexpected %s in message %s", describeProtoToken(tok), message.name)
	}
	switch tok.text {
	case "message":
		if oneof != "" {
			break
		}
		nested, err := p.parseMessage()
		if err != nil {
			return err
		}
		message.nested = append(message.nested, nested)
		return nil
	case "enum":
		enum, err := p.parseEnum()
		if err != nil {
			return err
		}
		message.enums = append(message.enums, enum)
		return nil
	case "oneof":
		return p.parseOneof(message)
	case protoOption:
		return p.parseMessageOption(message)
	case "reserved", "extensions":
		return p.skipStatement()
	case "extend":
		return p.skipBlockStatement()
	case "group":
		return p.errorf(tok, "proto2 groups are not supported")
	}
	field, err := p.parseField(oneof)
	if err != nil {
		return err
	}
	message.fields = append(message.fields, field)
	return nil
}

func (p *protoParser) parseMessageOption(message *protoMessage) error {
	p.pos++
	name, err := p.optionName()
	if err != nil {
		return err
	}
	if err := p.expectPunct("="); err != nil {
		return err
	}
	value, err := p.optionValue()
	if err != nil {
		return err
	}
	if name == protoDeprecated && value == protoTrue {
		message.deprecated = true
	}
	return p.expectPunct(";")
}

func (p *protoParser) optionName() (string, error) {
	var parts []string
	for {
		if p.acceptPunct("(") {
			name, err := p.fullIdent()
			if err != nil {
				return "", err
			}
			if err := p.expectPunct(")"); err != nil {
				return "", err
			}
			parts = append(parts, "("+name+")")
		} else {
			tok, err := p.expectIdent()
			if err != nil {
				return "", err
			}
			parts = append(parts, tok.text)
		}
		if !p.acceptPunct(".") {
			break
		}
	}
	return strings.Join(parts, "."), nil
}

func (p *protoParser) optionValue() (string, error) {
	var b strings.Builder
	tok := p.peek()
	if tok.kind == protoPunct && (tok.text == "-" || tok.text == "+") {
		b.WriteString(tok.text)
		p.pos++
		tok = p.peek()
	}
	switch tok.kind {
	case protoString:
		p.pos++
		b.WriteString(tok.text)
		for p.peek().kind == protoString {
			b.WriteString(p.next().text)
		}
		return b.String(), nil
	case protoInt, protoFloat:
		p.pos++
		b.WriteString(tok.text)
		return b.String(), nil
	case protoIdent:
		name, err := p.fullIdent()
		if err != nil {
			return "", err
		}
		return name, nil
	case protoPunct:
		if tok.text == "{" {
			p.pos++
			return "", p.skipBlockBody()
		}
	}
	return "", p.errorf(tok, "unexpected %s in an option value", describeProtoToken(tok))
}

func (p *protoParser) parseOneof(message *protoMessage) error {
	p.pos++
	name, err := p.expectIdent()
	if err != nil {
		return err
	}
	if err := p.expectPunct("{"); err != nil {
		return err
	}
	for !p.isPunct("}") {
		if p.peek().kind == protoEOF {
			return p.errorf(p.peek(), "unterminated oneof %s", name.text)
		}
		if p.peek().kind == protoIdent && p.peek().text == protoOption {
			if err := p.skipStatement(); err != nil {
				return err
			}
			continue
		}
		if p.acceptPunct(";") {
			continue
		}
		field, err := p.parseField(name.text)
		if err != nil {
			return err
		}
		message.fields = append(message.fields, field)
	}
	p.pos++
	return nil
}

func (p *protoParser) parseField(oneof string) (*protoField, error) {
	first := p.peek()
	field := &protoField{oneof: oneof, doc: first.doc}
	if first.kind == protoIdent {
		switch first.text {
		case "repeated":
			field.repeated = true
			p.pos++
		case "optional":
			field.optional = true
			p.pos++
		case "required":
			p.pos++
		}
	}
	if p.peek().kind == protoIdent && p.peek().text == "map" && p.tokens[p.pos+1].kind == protoPunct && p.tokens[p.pos+1].text == "<" {
		p.pos += 2
		key, err := p.fullIdent()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(","); err != nil {
			return nil, err
		}
		value, err := p.fullIdent()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(">"); err != nil {
			return nil, err
		}
		field.mapKey, field.mapValue = key, value
	} else {
		typ, err := p.fullIdent()
		if err != nil {
			return nil, err
		}
		field.typ = typ
	}
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	field.name = name.text
	if err := p.expectPunct("="); err != nil {
		return nil, err
	}
	number := p.next()
	if number.kind != protoInt {
		return nil, p.errorf(number, "expected a field number for %s", field.name)
	}
	if p.acceptPunct("[") {
		if err := p.parseFieldOptions(field); err != nil {
			return nil, err
		}
	}
	if err := p.expectPunct(";"); err != nil {
		return nil, err
	}
	return field, nil
}

func (p *protoParser) parseFieldOptions(field *protoField) error {
	for {
		name, err := p.optionName()
		if err != nil {
			return err
		}
		if err := p.expectPunct("="); err != nil {
			return err
		}
		value, err := p.optionValue()
		if err != nil {
			return err
		}
		switch name {
		case protoDeprecated:
			field.deprecated = value == protoTrue
		case "json_name":
			field.jsonName = value
		}
		if p.acceptPunct(",") {
			continue
		}
		return p.expectPunct("]")
	}
}

func (p *protoParser) parseEnum() (*protoEnum, error) {
	keyword := p.next()
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	enum := &protoEnum{name: name.text, doc: keyword.doc}
	if err := p.expectPunct("{"); err != nil {
		return nil, err
	}
	for !p.isPunct("}") {
		tok := p.peek()
		switch {
		case tok.kind == protoEOF:
			return nil, p.errorf(tok, "unterminated enum %s", enum.name)
		case tok.kind == protoPunct && tok.text == ";":
			p.pos++
		case tok.kind == protoIdent && (tok.text == protoOption || tok.text == "reserved"):
			if tok.text == protoOption {
				p.pos++
				optName, err := p.optionName()
				if err != nil {
					return nil, err
				}
				if err := p.expectPunct("="); err != nil {
					return nil, err
				}
				value, err := p.optionValue()
				if err != nil {
					return nil, err
				}
				if optName == protoDeprecated && value == protoTrue {
					enum.deprecated = true
				}
				if err := p.expectPunct(";"); err != nil {
					return nil, err
				}
				continue
			}
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
		case tok.kind == protoIdent:
			value, err := p.parseEnumValue()
			if err != nil {
				return nil, err
			}
			enum.values = append(enum.values, value)
		default:
			return nil, p.errorf(tok, "unexpected %s in enum %s", describeProtoToken(tok), enum.name)
		}
	}
	p.pos++
	return enum, nil
}

func (p *protoParser) parseEnumValue() (*protoEnumValue, error) {
	name := p.next()
	value := &protoEnumValue{name: name.text, doc: name.doc}
	if err := p.expectPunct("="); err != nil {
		return nil, err
	}
	sign := ""
	if p.acceptPunct("-") {
		sign = "-"
	}
	number := p.next()
	if number.kind != protoInt {
		return nil, p.errorf(number, "expected a number for enum value %s", value.name)
	}
	value.number = sign + number.text
	if p.acceptPunct("[") {
		for {
			optName, err := p.optionName()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct("="); err != nil {
				return nil, err
			}
			optValue, err := p.optionValue()
			if err != nil {
				return nil, err
			}
			if optName == protoDeprecated && optValue == protoTrue {
				value.deprecated = true
			}
			if p.acceptPunct(",") {
				continue
			}
			if err := p.expectPunct("]"); err != nil {
				return nil, err
			}
			break
		}
	}
	return value, p.expectPunct(";")
}

func (p *protoParser) parseService() (*protoService, error) {
	keyword := p.next()
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	service := &protoService{name: name.text, doc: keyword.doc}
	if err := p.expectPunct("{"); err != nil {
		return nil, err
	}
	for !p.isPunct("}") {
		tok := p.peek()
		switch {
		case tok.kind == protoEOF:
			return nil, p.errorf(tok, "unterminated service %s", service.name)
		case tok.kind == protoPunct && tok.text == ";":
			p.pos++
		case tok.kind == protoIdent && tok.text == protoOption:
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
		case tok.kind == protoIdent && tok.text == "rpc":
			method, err := p.parseMethod()
			if err != nil {
				return nil, err
			}
			service.methods = append(service.methods, method)
		default:
			return nil, p.errorf(tok, "unexpected %s in service %s", describeProtoToken(tok), service.name)
		}
	}
	p.pos++
	return service, nil
}

func (p *protoParser) parseMethod() (*protoMethod, error) {
	keyword := p.next()
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	method := &protoMethod{name: name.text, doc: keyword.doc}
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	if p.peek().kind == protoIdent && p.peek().text == "stream" {
		p.pos++
		method.clientStream = true
	}
	if method.request, err = p.fullIdent(); err != nil {
		return nil, err
	}
	if err := p.expectPunct(")"); err != nil {
		return nil, err
	}
	returns, err := p.expectIdent()
	if err != nil || returns.text != "returns" {
		return nil, p.errorf(returns, "expected \"returns\" in rpc %s", method.name)
	}
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	if p.peek().kind == protoIdent && p.peek().text == "stream" {
		p.pos++
		method.serverStream = true
	}
	if method.response, err = p.fullIdent(); err != nil {
		return nil, err
	}
	if err := p.expectPunct(")"); err != nil {
		return nil, err
	}
	if p.acceptPunct(";") {
		return method, nil
	}
	if err := p.expectPunct("{"); err != nil {
		return nil, err
	}
	for !p.isPunct("}") {
		if p.peek().kind == protoEOF {
			return nil, p.errorf(p.peek(), "unterminated rpc %s", method.name)
		}
		if p.acceptPunct(";") {
			continue
		}
		if err := p.parseMethodOption(method); err != nil {
			return nil, err
		}
	}
	p.pos++
	return method, nil
}

func (p *protoParser) parseMethodOption(method *protoMethod) error {
	tok, err := p.expectIdent()
	if err != nil || tok.text != protoOption {
		return p.errorf(tok, "unexpected %s in rpc %s", describeProtoToken(tok), method.name)
	}
	name, err := p.optionName()
	if err != nil {
		return err
	}
	if err := p.expectPunct("="); err != nil {
		return err
	}
	if name == "(google.api.http)" && p.isPunct("{") {
		p.pos++
		rule, err := p.parseHTTPRule()
		if err != nil {
			return err
		}
		method.http = rule
		return p.expectPunct(";")
	}
	value, err := p.optionValue()
	if err != nil {
		return err
	}
	if name == protoDeprecated && value == protoTrue {
		method.deprecated = true
	}
	return p.expectPunct(";")
}

func (p *protoParser) parseHTTPRule() (*protoHTTPRule, error) {
	rule := &protoHTTPRule{}
	for !p.isPunct("}") {
		key := p.next()
		if key.kind == protoEOF {
			return nil, p.errorf(key, "unterminated google.api.http option")
		}
		if key.kind != protoIdent {
			return nil, p.errorf(key, "unexpected %s in google.api.http option", describeProtoToken(key))
		}
		p.acceptPunct(":")
		if p.isPunct("{") {
			p.pos++
			if err := p.skipBlockBody(); err != nil {
				return nil, err
			}
			continue
		}
		value := p.next()
		if value.kind != protoString && value.kind != protoIdent {
			return nil, p.errorf(value, "unexpected %s in google.api.http option", describeProtoToken(value))
		}
		switch key.text {
		case "get", "put", "post", "delete", "patch":
			if rule.verb == "" {
				rule.verb, rule.path = key.text, value.text
			}
		case "body":
			rule.body = value.text
		}
	}
	p.pos++
	return rule, nil
}
