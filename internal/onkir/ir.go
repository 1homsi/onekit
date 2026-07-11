package onkir

type ScalarKind int

const (
	ScalarString ScalarKind = iota
	ScalarBool
	ScalarInt32
	ScalarInt64
	ScalarUint32
	ScalarUint64
	ScalarFloat32
	ScalarFloat64
	ScalarBytes
	ScalarTimestamp
)

func (s ScalarKind) String() string {
	switch s {
	case ScalarString:
		return "string"
	case ScalarBool:
		return "bool"
	case ScalarInt32:
		return "int32"
	case ScalarInt64:
		return "int64"
	case ScalarUint32:
		return "uint32"
	case ScalarUint64:
		return "uint64"
	case ScalarFloat32:
		return "float32"
	case ScalarFloat64:
		return "float64"
	case ScalarBytes:
		return "bytes"
	case ScalarTimestamp:
		return "timestamp"
	default:
		return "unknown"
	}
}

func ParseScalarKind(name string) (ScalarKind, bool) {
	switch name {
	case "string":
		return ScalarString, true
	case "bool":
		return ScalarBool, true
	case "int32":
		return ScalarInt32, true
	case "int64":
		return ScalarInt64, true
	case "uint32":
		return ScalarUint32, true
	case "uint64":
		return ScalarUint64, true
	case "float32":
		return ScalarFloat32, true
	case "float64":
		return ScalarFloat64, true
	case "bytes":
		return ScalarBytes, true
	case "timestamp":
		return ScalarTimestamp, true
	default:
		return 0, false
	}
}

type TypeKind int

const (
	KindScalar TypeKind = iota
	KindMessage
	KindEnum
	KindMap
)

type Type struct {
	Kind     TypeKind
	Scalar   ScalarKind
	Message  *Message
	Enum     *Enum
	MapKey   ScalarKind
	MapValue *Type
}

type Arg struct {
	Name  string
	Value string
}

type Decorator struct {
	Name string
	Args []Arg
}

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

type OneofVariant struct {
	Name       string
	Type       *Type
	Decorators []Decorator
	Oneof      *Oneof
}

type Oneof struct {
	Field    *Field
	Args     []Arg
	Variants []*OneofVariant
}

type Message struct {
	Name        string
	Doc         string
	Fields      []*Field
	Nested      []*Message
	NestedEnums []*Enum
	Decorators  []Decorator
	File        *File
	Parent      *Message
}

type EnumValue struct {
	Name       string
	Doc        string
	Decorators []Decorator
	Enum       *Enum
	Index      int
}

type Enum struct {
	Name   string
	Doc    string
	Values []*EnumValue
	File   *File
	Parent *Message
}

type Header struct {
	Name       string
	Type       ScalarKind
	Decorators []Decorator
}

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

type Service struct {
	Name     string
	Doc      string
	BasePath string
	Headers  []*Header
	Methods  []*Method
	File     *File
}

type File struct {
	Path     string
	Package  string
	Imports  []string
	Messages []*Message
	Enums    []*Enum
	Services []*Service
}

type Package struct {
	Name  string
	Files []*File
}
