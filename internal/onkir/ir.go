package onkir

// ScalarKind enumerates the primitive types of the .onk language.
type ScalarKind int

const (
	// ScalarString is the UTF-8 string scalar.
	ScalarString ScalarKind = iota
	// ScalarBool is the boolean scalar.
	ScalarBool
	// ScalarInt32 is a signed 32-bit integer scalar.
	ScalarInt32
	// ScalarInt64 is a signed 64-bit integer scalar.
	ScalarInt64
	// ScalarUint32 is an unsigned 32-bit integer scalar.
	ScalarUint32
	// ScalarUint64 is an unsigned 64-bit integer scalar.
	ScalarUint64
	// ScalarFloat32 is an IEEE-754 single-precision float scalar.
	ScalarFloat32
	// ScalarFloat64 is an IEEE-754 double-precision float scalar.
	ScalarFloat64
	// ScalarBytes is an opaque byte-string scalar.
	ScalarBytes
	// ScalarTimestamp is an RFC 3339 timestamp scalar.
	ScalarTimestamp
	// ScalarJSON is an arbitrary JSON value scalar.
	ScalarJSON
)

// scalarKindNames is the single source of truth mapping each ScalarKind to
// its .onk source spelling; String and ParseScalarKind both derive from it so
// the two can never drift apart.
var scalarKindNames = [...]string{
	ScalarString:    "string",
	ScalarBool:      "bool",
	ScalarInt32:     "int32",
	ScalarInt64:     "int64",
	ScalarUint32:    "uint32",
	ScalarUint64:    "uint64",
	ScalarFloat32:   "float32",
	ScalarFloat64:   "float64",
	ScalarBytes:     "bytes",
	ScalarTimestamp: "timestamp",
	ScalarJSON:      "json",
}

var scalarKindsByName = func() map[string]ScalarKind {
	kinds := make(map[string]ScalarKind, len(scalarKindNames))
	for kind, name := range scalarKindNames {
		kinds[name] = ScalarKind(kind)
	}
	return kinds
}()

// String returns the .onk source spelling of the scalar kind, or "unknown"
// for values outside the defined range.
func (s ScalarKind) String() string {
	if s < 0 || int(s) >= len(scalarKindNames) {
		return "unknown"
	}
	return scalarKindNames[s]
}

// ParseScalarKind maps a .onk scalar type name onto its IR kind.
func ParseScalarKind(name string) (ScalarKind, bool) {
	kind, ok := scalarKindsByName[name]
	return kind, ok
}

// TypeKind classifies a Type as one of the four shapes the IR supports.
type TypeKind int

const (
	// KindScalar is a primitive scalar type.
	KindScalar TypeKind = iota
	// KindMessage is a reference to a named message.
	KindMessage
	// KindEnum is a reference to a named enum.
	KindEnum
	// KindMap is a map from a string scalar key to another type.
	KindMap
)

// Type is a resolved field or variant type in the compiled IR.
type Type struct {
	// Kind selects which of Scalar/Message+Enum/MapValue carries the payload.
	Kind TypeKind
	// Scalar is set when Kind is KindScalar.
	Scalar ScalarKind
	// Message is set when Kind is KindMessage.
	Message *Message
	// Enum is set when Kind is KindEnum.
	Enum *Enum
	// MapKey is set when Kind is KindMap and is always ScalarString on the wire.
	MapKey ScalarKind
	// MapValue is set when Kind is KindMap.
	MapValue *Type
}

// Arg is one positional or named decorator argument.
type Arg struct {
	Name   string
	Value  string
	Quoted bool
}

// Decorator is an @name(args...) annotation attached to a declaration, field,
// method, header, or enum value.
type Decorator struct {
	Name string
	Args []Arg
}

// Field is one member of a message.
type Field struct {
	Name       string
	Doc        string
	Type       *Type
	Repeated   bool
	Optional   bool
	Decorators []Decorator
	Oneof      *Oneof
	Message    *Message
}

// OneofVariant is one alternative of a discriminated oneof field.
type OneofVariant struct {
	Name       string
	Type       *Type
	Decorators []Decorator
	Oneof      *Oneof
}

// Oneof is a tagged union bound to its owning Field.
type Oneof struct {
	Field    *Field
	Args     []Arg
	Variants []*OneofVariant
}

// Message is a compiled message with nested messages/enums and back links to
// its File and enclosing Message.
type Message struct {
	Name        string
	SchemaName  string
	Doc         string
	ErrorType   bool
	Fields      []*Field
	Nested      []*Message
	NestedEnums []*Enum
	Decorators  []Decorator
	File        *File
	Parent      *Message
}

// EnumValue is one member of an enum.
type EnumValue struct {
	Name       string
	Doc        string
	Decorators []Decorator
	Enum       *Enum
	Index      int
}

// Enum is a compiled closed enum.
type Enum struct {
	Name       string
	SchemaName string
	Doc        string
	Values     []*EnumValue
	File       *File
	Parent     *Message
}

// Header is one typed HTTP header contract on a service or RPC.
type Header struct {
	Name       string
	Type       ScalarKind
	Decorators []Decorator
}

// Method is one RPC within a Service.
type Method struct {
	Name       string
	Doc        string
	Request    *Message
	Response   *Message
	ErrorTypes []*Message
	Decorators []Decorator
	Headers    []*Header
	Service    *Service
}

// Service is a compiled service with its base path, shared headers, and RPCs.
type Service struct {
	Name     string
	Doc      string
	BasePath string
	Headers  []*Header
	Methods  []*Method
	File     *File
}

// File is one compiled .onk source file.
type File struct {
	Path     string
	Package  string
	Imports  []string
	Messages []*Message
	Enums    []*Enum
	Services []*Service
}

// Package groups all files that compile together into one generation unit.
type Package struct {
	Name  string
	Files []*File
}
