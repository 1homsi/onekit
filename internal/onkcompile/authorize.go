package onkcompile

import (
	"errors"
	"fmt"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

type principalSite struct {
	message *onkir.Message
	path    string
	line    int
	col     int
}

func (c *compiler) findPrincipal(sources []Source) (*principalSite, error) {
	var found *principalSite
	var walk func(md *onklang.MessageDecl, path string) error
	walk = func(md *onklang.MessageDecl, path string) error {
		for _, d := range md.Decorators {
			if d.Name != principalDecorator {
				continue
			}
			message := c.msgNode[md]
			if message.IsError() {
				return &Error{Path: path, Line: d.Line, Column: d.Col, Code: "invalid_principal", Msg: fmt.Sprintf("@principal cannot mark the error message %s", message.Name)}
			}
			if found != nil {
				return &Error{Path: path, Line: d.Line, Column: d.Col, Code: "invalid_principal", Msg: fmt.Sprintf("only one message may be marked @principal, and %s already is", found.message.Name)}
			}
			found = &principalSite{message: message, path: path, line: d.Line, col: d.Col}
		}
		for _, nested := range md.Nested {
			if err := walk(nested, path); err != nil {
				return err
			}
		}
		return nil
	}
	for _, src := range sources {
		for _, md := range src.AST.Messages {
			if err := walk(md, src.Path); err != nil {
				return nil, err
			}
		}
	}
	return found, nil
}

func (c *compiler) validateAuthorize(sources []Source) error {
	principal, err := c.findPrincipal(sources)
	if err != nil {
		return err
	}
	for _, src := range sources {
		for _, sd := range src.AST.Services {
			for _, rpc := range sd.RPCs {
				if err := c.validateRPCAuthorize(src.Path, rpc, principal); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *compiler) validateRPCAuthorize(path string, rpc *onklang.RPCDecl, principal *principalSite) error {
	method := c.rpcNode[rpc]
	for _, d := range rpc.Decorators {
		if d.Name != authorizeDecorator {
			continue
		}
		line, col := d.Line, d.Col
		if line == 0 {
			line, col = rpc.Line, rpc.Col
		}
		if principal == nil {
			return &Error{Path: path, Line: line, Column: col, Code: "invalid_authorize", Msg: "@authorize needs a message marked @principal to describe the authenticated caller"}
		}
		method.Principal = principal.message
		source, message := d.Args[0].Value, d.Args[1].Value
		if _, err := onkexpr.CompileAuthorize(source, principal.message, method.Request); err != nil {
			detail := err.Error()
			var exprErr *onkexpr.Error
			if errors.As(err, &exprErr) {
				detail = fmt.Sprintf("%s (column %d of the expression)", exprErr.Message, exprErr.Offset+1)
			}
			return &Error{Path: path, Line: line, Column: col, Code: "invalid_authorize", Msg: fmt.Sprintf("invalid @authorize %q: %s", source, detail)}
		}
		if len(message) > 200 {
			return &Error{Path: path, Line: line, Column: col, Code: "invalid_authorize", Msg: "the @authorize message is longer than 200 characters"}
		}
	}
	return nil
}
