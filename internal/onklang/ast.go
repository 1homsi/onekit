package onklang

type File struct {
	Package  string
	Imports  []string
	Messages []*MessageDecl
	Enums    []*EnumDecl
	Services []*ServiceDecl
}

type Arg struct {
	Name  string
	Value string
}

type Decorator struct {
	Name string
	Args []Arg
	Line int
}

type TypeRef struct {
	Name   string
	IsMap  bool
	MapKey string
	MapVal *TypeRef
}

type FieldDecl struct {
	Name       string
	Doc        string
	Type       *TypeRef
	Repeated   bool
	Optional   bool
	Decorators []Decorator
	Oneof      *OneofDecl
	Line       int
}

type OneofVariant struct {
	Name       string
	Type       *TypeRef
	Decorators []Decorator
	Line       int
}

type OneofDecl struct {
	Args     []Arg
	Variants []OneofVariant
	Line     int
}

type MessageDecl struct {
	Name       string
	Doc        string
	Decorators []Decorator
	Fields     []*FieldDecl
	Nested     []*MessageDecl
	NestedEn   []*EnumDecl
	Line       int
}

type EnumValueDecl struct {
	Name       string
	Doc        string
	Decorators []Decorator
	Line       int
}

type EnumDecl struct {
	Name   string
	Doc    string
	Values []EnumValueDecl
	Line   int
}

type HeaderDecl struct {
	Name       string
	Type       string
	Decorators []Decorator
	Line       int
}

type RPCDecl struct {
	Name         string
	Doc          string
	RequestType  string
	ResponseType string
	ErrorTypes   []string
	Decorators   []Decorator
	Headers      []HeaderDecl
	Line         int
}

type ServiceDecl struct {
	Name     string
	Doc      string
	BasePath string
	Headers  []HeaderDecl
	RPCs     []*RPCDecl
	Line     int
}
