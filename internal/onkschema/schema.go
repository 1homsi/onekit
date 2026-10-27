// Package onkschema defines the declaration-level schema contract every
// onekit frontend produces and the compiler consumes: packages of messages,
// enums and services whose type references and decorators are still
// unresolved names. Its JSON form (see Document) is the format a frontend
// such as the @onkt/typespec emitter writes; positions point back at that
// frontend's own source files.
package onkschema

type File struct {
	Package string `json:"package"`
	// PackageDoc holds `///` documentation attached to the package
	// declaration so formatters can round-trip file-level docs.
	PackageDoc       string         `json:"doc,omitempty"`
	Imports          []string       `json:"imports,omitempty"`
	ImportComments   [][]string     `json:"-"`
	LeadingComments  []string       `json:"-"`
	TrailingComments []string       `json:"-"`
	Messages         []*MessageDecl `json:"messages,omitempty"`
	Enums            []*EnumDecl    `json:"enums,omitempty"`
	Services         []*ServiceDecl `json:"services,omitempty"`
	// Declarations preserves source order for formatters and editor tooling.
	// The typed collections above remain the compiler-facing representation.
	Declarations []Declaration `json:"-"`
}

type Declaration interface{ isDeclaration() }

func (*MessageDecl) isDeclaration() {}
func (*EnumDecl) isDeclaration()    {}
func (*ServiceDecl) isDeclaration() {}

type Arg struct {
	Name   string `json:"name,omitempty"`
	Value  string `json:"value"`
	Quoted bool   `json:"quoted,omitempty"`
}

type Decorator struct {
	Name string `json:"name"`
	Args []Arg  `json:"args,omitempty"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

// Span uses one-based columns. End is exclusive.
type Span struct {
	Line    int `json:"line,omitempty"`
	Col     int `json:"col,omitempty"`
	EndLine int `json:"endLine,omitempty"`
	EndCol  int `json:"endCol,omitempty"`
}

type TypeRef struct {
	Span   Span     `json:"span"`
	Name   string   `json:"name,omitempty"`
	IsMap  bool     `json:"map,omitempty"`
	MapKey string   `json:"mapKey,omitempty"`
	MapVal *TypeRef `json:"mapValue,omitempty"`
}

type FieldDecl struct {
	Name            string      `json:"name"`
	Doc             string      `json:"doc,omitempty"`
	LeadingComments []string    `json:"-"`
	Type            *TypeRef    `json:"type,omitempty"`
	Repeated        bool        `json:"repeated,omitempty"`
	Optional        bool        `json:"optional,omitempty"`
	Decorators      []Decorator `json:"decorators,omitempty"`
	Oneof           *OneofDecl  `json:"oneof,omitempty"`
	Line            int         `json:"line,omitempty"`
	Col             int         `json:"col,omitempty"`
}

type OneofVariant struct {
	Name            string      `json:"name"`
	LeadingComments []string    `json:"-"`
	Type            *TypeRef    `json:"type"`
	Decorators      []Decorator `json:"decorators,omitempty"`
	Line            int         `json:"line,omitempty"`
	Col             int         `json:"col,omitempty"`
}

type OneofDecl struct {
	LeadingComments []string       `json:"-"`
	Args            []Arg          `json:"args,omitempty"`
	Variants        []OneofVariant `json:"variants"`
	// TrailingComments are the comments written just before the closing
	// brace, after the last member; the same field on the other block
	// declarations below means the same thing.
	TrailingComments []string `json:"-"`
	Line             int      `json:"line,omitempty"`
	Col              int      `json:"col,omitempty"`
}

type MessageMember interface{ isMessageMember() }

func (*FieldDecl) isMessageMember()   {}
func (*MessageDecl) isMessageMember() {}
func (*EnumDecl) isMessageMember()    {}

type MessageDecl struct {
	Name             string          `json:"name"`
	Doc              string          `json:"doc,omitempty"`
	LeadingComments  []string        `json:"-"`
	Decorators       []Decorator     `json:"decorators,omitempty"`
	Fields           []*FieldDecl    `json:"fields,omitempty"`
	Nested           []*MessageDecl  `json:"messages,omitempty"`
	NestedEn         []*EnumDecl     `json:"enums,omitempty"`
	Line             int             `json:"line,omitempty"`
	Col              int             `json:"col,omitempty"`
	Members          []MessageMember `json:"-"`
	TrailingComments []string        `json:"-"`
}

type EnumValueDecl struct {
	Name            string      `json:"name"`
	Doc             string      `json:"doc,omitempty"`
	LeadingComments []string    `json:"-"`
	Decorators      []Decorator `json:"decorators,omitempty"`
	Line            int         `json:"line,omitempty"`
	Col             int         `json:"col,omitempty"`
}

type EnumDecl struct {
	Name             string          `json:"name"`
	Doc              string          `json:"doc,omitempty"`
	LeadingComments  []string        `json:"-"`
	Values           []EnumValueDecl `json:"values"`
	Line             int             `json:"line,omitempty"`
	Col              int             `json:"col,omitempty"`
	TrailingComments []string        `json:"-"`
}

type HeaderDecl struct {
	Name            string      `json:"name"`
	LeadingComments []string    `json:"-"`
	Type            string      `json:"type"`
	Decorators      []Decorator `json:"decorators,omitempty"`
	Line            int         `json:"line,omitempty"`
	Col             int         `json:"col,omitempty"`
}

type RPCDecl struct {
	Name                    string       `json:"name"`
	Doc                     string       `json:"doc,omitempty"`
	LeadingComments         []string     `json:"-"`
	RequestSpan             Span         `json:"requestSpan"`
	ResponseSpan            Span         `json:"responseSpan"`
	ErrorSpans              []Span       `json:"errorSpans,omitempty"`
	RequestType             string       `json:"request"`
	ResponseType            string       `json:"response"`
	ErrorTypes              []string     `json:"errors,omitempty"`
	Decorators              []Decorator  `json:"decorators,omitempty"`
	HeadersComments         []string     `json:"-"`
	Headers                 []HeaderDecl `json:"headers,omitempty"`
	HeadersTrailingComments []string     `json:"-"`
	// TrailingComments are the comments before the rpc body's closing brace.
	TrailingComments []string `json:"-"`
	Line             int      `json:"line,omitempty"`
	Col              int      `json:"col,omitempty"`
}

type ServiceDecl struct {
	Name                    string       `json:"name"`
	Doc                     string       `json:"doc,omitempty"`
	LeadingComments         []string     `json:"-"`
	BasePath                string       `json:"basePath,omitempty"`
	BasePathComments        []string     `json:"-"`
	HeadersComments         []string     `json:"-"`
	Headers                 []HeaderDecl `json:"headers,omitempty"`
	HeadersTrailingComments []string     `json:"-"`
	RPCs                    []*RPCDecl   `json:"rpcs,omitempty"`
	Line                    int          `json:"line,omitempty"`
	Col                     int          `json:"col,omitempty"`
	TrailingComments        []string     `json:"-"`
}
