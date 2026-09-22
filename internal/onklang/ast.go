package onklang

type File struct {
	Package string
	// PackageDoc holds `///` documentation attached to the package
	// declaration so formatters can round-trip file-level docs.
	PackageDoc       string
	Imports          []string
	ImportComments   [][]string
	LeadingComments  []string
	TrailingComments []string
	Messages         []*MessageDecl
	Enums            []*EnumDecl
	Services         []*ServiceDecl
	// Declarations preserves source order for formatters and editor tooling.
	// The typed collections above remain the compiler-facing representation.
	Declarations []Declaration
}

type Declaration interface{ isDeclaration() }

func (*MessageDecl) isDeclaration() {}
func (*EnumDecl) isDeclaration()    {}
func (*ServiceDecl) isDeclaration() {}

type Arg struct {
	Name   string
	Value  string
	Quoted bool
}

type Decorator struct {
	Name string
	Args []Arg
	Line int
}

// Span uses one-based byte columns, matching lexer diagnostics. End is exclusive.
type Span struct {
	Line, Col, EndLine, EndCol int
}

type TypeRef struct {
	Span   Span
	Name   string
	IsMap  bool
	MapKey string
	MapVal *TypeRef
}

type FieldDecl struct {
	Name            string
	Doc             string
	LeadingComments []string
	Type            *TypeRef
	Repeated        bool
	Optional        bool
	Decorators      []Decorator
	Oneof           *OneofDecl
	Line            int
	Col             int
}

type OneofVariant struct {
	Name            string
	LeadingComments []string
	Type            *TypeRef
	Decorators      []Decorator
	Line            int
	Col             int
}

type OneofDecl struct {
	LeadingComments []string
	Args            []Arg
	Variants        []OneofVariant
	// TrailingComments are the comments written just before the closing
	// brace, after the last member; the same field on the other block
	// declarations below means the same thing.
	TrailingComments []string
	Line             int
	Col              int
}

type MessageMember interface{ isMessageMember() }

func (*FieldDecl) isMessageMember()   {}
func (*MessageDecl) isMessageMember() {}
func (*EnumDecl) isMessageMember()    {}

type MessageDecl struct {
	Name             string
	Doc              string
	LeadingComments  []string
	Decorators       []Decorator
	Fields           []*FieldDecl
	Nested           []*MessageDecl
	NestedEn         []*EnumDecl
	Line             int
	Col              int
	Members          []MessageMember
	TrailingComments []string
}

type EnumValueDecl struct {
	Name            string
	Doc             string
	LeadingComments []string
	Decorators      []Decorator
	Line            int
	Col             int
}

type EnumDecl struct {
	Name             string
	Doc              string
	LeadingComments  []string
	Values           []EnumValueDecl
	Line             int
	Col              int
	TrailingComments []string
}

type HeaderDecl struct {
	Name            string
	LeadingComments []string
	Type            string
	Decorators      []Decorator
	Line            int
	Col             int
}

type RPCDecl struct {
	Name                    string
	Doc                     string
	LeadingComments         []string
	RequestSpan             Span
	ResponseSpan            Span
	ErrorSpans              []Span
	RequestType             string
	ResponseType            string
	ErrorTypes              []string
	Decorators              []Decorator
	HeadersComments         []string
	Headers                 []HeaderDecl
	HeadersTrailingComments []string
	// TrailingComments are the comments before the rpc body's closing brace.
	TrailingComments []string
	Line             int
	Col              int
}

type ServiceDecl struct {
	Name                    string
	Doc                     string
	LeadingComments         []string
	BasePath                string
	BasePathComments        []string
	HeadersComments         []string
	Headers                 []HeaderDecl
	HeadersTrailingComments []string
	RPCs                    []*RPCDecl
	Line                    int
	Col                     int
	TrailingComments        []string
}
