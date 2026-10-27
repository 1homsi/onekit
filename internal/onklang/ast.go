package onklang

import "github.com/1homsi/onekit/internal/onkschema"

// The declaration types live in onkschema, the contract every frontend
// produces; these aliases keep the .onk parser and formatter on them.

type (
	File          = onkschema.File
	Declaration   = onkschema.Declaration
	Arg           = onkschema.Arg
	Decorator     = onkschema.Decorator
	Span          = onkschema.Span
	TypeRef       = onkschema.TypeRef
	FieldDecl     = onkschema.FieldDecl
	OneofVariant  = onkschema.OneofVariant
	OneofDecl     = onkschema.OneofDecl
	MessageMember = onkschema.MessageMember
	MessageDecl   = onkschema.MessageDecl
	EnumValueDecl = onkschema.EnumValueDecl
	EnumDecl      = onkschema.EnumDecl
	HeaderDecl    = onkschema.HeaderDecl
	RPCDecl       = onkschema.RPCDecl
	ServiceDecl   = onkschema.ServiceDecl
)
