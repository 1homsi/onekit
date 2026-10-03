package onkexpr

import "github.com/1homsi/onekit/internal/onkir"

type Pos struct {
	Offset int
}

type Error struct {
	Offset  int
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

const (
	fnInt        = "int"
	fnDouble     = "double"
	fnSize       = "size"
	fnHas        = "has"
	fnStartsWith = "startsWith"
	fnEndsWith   = "endsWith"
	fnContains   = "contains"
	fnMatches    = "matches"
	macroAll     = "all"
	macroExists  = "exists"
)

type Kind int

const (
	KindBool Kind = iota
	KindInt
	KindDouble
	KindString
	KindBytes
	KindList
	KindMap
	KindMessage
	KindEnum
)

type Type struct {
	Kind    Kind
	Elem    *Type
	Message *onkir.Message
	Enum    *onkir.Enum
}

func (t *Type) String() string {
	switch t.Kind {
	case KindBool:
		return "bool"
	case KindInt:
		return fnInt
	case KindDouble:
		return fnDouble
	case KindString:
		return "string"
	case KindBytes:
		return "bytes"
	case KindList:
		return "list<" + t.Elem.String() + ">"
	case KindMap:
		return "map<string, " + t.Elem.String() + ">"
	case KindMessage:
		return t.Message.Name
	case KindEnum:
		return t.Enum.Name
	}
	return "unknown"
}

func (t *Type) Equal(other *Type) bool {
	if t == nil || other == nil || t.Kind != other.Kind {
		return false
	}
	switch t.Kind {
	case KindList, KindMap:
		return t.Elem.Equal(other.Elem)
	case KindMessage:
		return t.Message == other.Message
	case KindEnum:
		return t.Enum == other.Enum
	}
	return true
}

type Node interface {
	Position() Pos
	Type() *Type
	setType(*Type)
}

type base struct {
	Pos Pos
	T   *Type
}

func (b *base) Position() Pos   { return b.Pos }
func (b *base) Type() *Type     { return b.T }
func (b *base) setType(t *Type) { b.T = t }

type (
	IntLit struct {
		base
		Value int64
	}
	DoubleLit struct {
		base
		Value float64
	}
	StringLit struct {
		base
		Value string
	}
	BoolLit struct {
		base
		Value bool
	}
	ListLit struct {
		base
		Elems []Node
	}
	Ident struct {
		base
		Name string
	}
	Select struct {
		base
		X     Node
		Name  string
		Field *onkir.Field
	}
	Index struct {
		base
		X   Node
		Idx Node
	}
	Call struct {
		base
		Fn    string
		Args  []Node
		Regex string
	}
	Macro struct {
		base
		Kind  string
		Range Node
		Var   string
		Body  Node
	}
	Unary struct {
		base
		Op string
		X  Node
	}
	Binary struct {
		base
		Op   string
		L, R Node
	}
	Ternary struct {
		base
		Cond, A, B Node
	}
)
