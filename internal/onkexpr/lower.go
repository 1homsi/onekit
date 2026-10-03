package onkexpr

import "github.com/1homsi/onekit/internal/onkir"

type OpKind int

const (
	OpInt OpKind = iota
	OpDouble
	OpStr
	OpBool
	OpList
	OpSelf
	OpSlot
	OpSelect
	OpIndexList
	OpIndexMap
	OpSize
	OpStartsWith
	OpEndsWith
	OpContains
	OpMatches
	OpToInt
	OpToDouble
	OpHas
	OpAll
	OpExists
	OpNot
	OpNegInt
	OpNegDouble
	OpTernary
	OpAnd
	OpOr
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpIn
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpFAdd
	OpFSub
	OpFMul
	OpFDiv
)

type Zero int

const (
	ZeroStr Zero = iota
	ZeroInt
	ZeroDouble
	ZeroBool
	ZeroBytes
	ZeroList
	ZeroMap
	ZeroMsg
)

type Op struct {
	Kind   OpKind
	Int    int64
	Double float64
	Str    string
	Bool   bool
	Name   string
	Zero   Zero
	Ptr    bool
	Slot   int
	Regex  int
	Args   []*Op
}

type RegexTable struct {
	Patterns []string
}

func (t *RegexTable) Index(pattern string) int {
	for i, existing := range t.Patterns {
		if existing == pattern {
			return i
		}
	}
	t.Patterns = append(t.Patterns, pattern)
	return len(t.Patterns) - 1
}

type lowerer struct {
	field   *onkir.Field
	regexes *RegexTable
	vars    []string
}

func Lower(n Node, field *onkir.Field, regexes *RegexTable) *Op {
	l := &lowerer{field: field, regexes: regexes}
	return l.lower(n)
}

func ZeroFor(f *onkir.Field) Zero {
	switch {
	case f.Repeated:
		return ZeroList
	case f.Type.Kind == onkir.KindMap:
		return ZeroMap
	case f.Type.Kind == onkir.KindMessage:
		return ZeroMsg
	case f.Type.Kind == onkir.KindEnum:
		return ZeroInt
	case f.Type.Kind == onkir.KindScalar:
		switch f.Type.Scalar {
		case onkir.ScalarString:
			return ZeroStr
		case onkir.ScalarBool:
			return ZeroBool
		case onkir.ScalarFloat32, onkir.ScalarFloat64:
			return ZeroDouble
		case onkir.ScalarBytes:
			return ZeroBytes
		}
	}
	return ZeroInt
}

func PointerBacked(f *onkir.Field) bool {
	return !f.Repeated && (f.Optional || f.Type.Kind == onkir.KindMessage)
}

var binaryKinds = map[string]OpKind{
	"&&": OpAnd, "||": OpOr, "==": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe, "in": OpIn,
}

var intKinds = map[string]OpKind{"+": OpAdd, "-": OpSub, "*": OpMul, "/": OpDiv, "%": OpMod}
var doubleKinds = map[string]OpKind{"+": OpFAdd, "-": OpFSub, "*": OpFMul, "/": OpFDiv}

func (l *lowerer) lower(n Node) *Op {
	switch n := n.(type) {
	case *IntLit:
		return &Op{Kind: OpInt, Int: n.Value}
	case *DoubleLit:
		return &Op{Kind: OpDouble, Double: n.Value}
	case *StringLit:
		return l.stringLit(n)
	case *BoolLit:
		return &Op{Kind: OpBool, Bool: n.Value}
	case *ListLit:
		return &Op{Kind: OpList, Args: l.all(n.Elems)}
	case *Ident:
		return l.ident(n)
	case *Select:
		return &Op{Kind: OpSelect, Name: n.Field.Name, Zero: ZeroFor(n.Field), Args: []*Op{l.lower(n.X)}}
	case *Index:
		kind := OpIndexList
		if n.X.Type().Kind == KindMap {
			kind = OpIndexMap
		}
		return &Op{Kind: kind, Args: []*Op{l.lower(n.X), l.lower(n.Idx)}}
	case *Call:
		return l.call(n)
	case *Macro:
		return l.macro(n)
	case *Unary:
		return l.unary(n)
	case *Binary:
		return l.binary(n)
	case *Ternary:
		return &Op{Kind: OpTernary, Args: []*Op{l.lower(n.Cond), l.lower(n.A), l.lower(n.B)}}
	}
	return &Op{Kind: OpBool}
}

func (l *lowerer) all(nodes []Node) []*Op {
	out := make([]*Op, len(nodes))
	for i, n := range nodes {
		out[i] = l.lower(n)
	}
	return out
}

func (l *lowerer) stringLit(n *StringLit) *Op {
	if t := n.Type(); t != nil && t.Kind == KindEnum {
		for i, v := range t.Enum.Values {
			if v.Name == n.Value {
				return &Op{Kind: OpInt, Int: int64(i)}
			}
		}
	}
	return &Op{Kind: OpStr, Str: n.Value}
}

func (l *lowerer) ident(n *Ident) *Op {
	for i := len(l.vars) - 1; i >= 0; i-- {
		if l.vars[i] == n.Name {
			return &Op{Kind: OpSlot, Slot: i}
		}
	}
	self := &Op{Kind: OpSelf}
	if n.Name == "value" && l.field != nil {
		return &Op{Kind: OpSelect, Name: l.field.Name, Zero: ZeroFor(l.field), Args: []*Op{self}}
	}
	return self
}

func (l *lowerer) call(n *Call) *Op {
	args := l.all(n.Args)
	switch n.Fn {
	case fnSize:
		return &Op{Kind: OpSize, Args: args}
	case fnStartsWith:
		return &Op{Kind: OpStartsWith, Args: args}
	case fnEndsWith:
		return &Op{Kind: OpEndsWith, Args: args}
	case fnContains:
		return &Op{Kind: OpContains, Args: args}
	case fnMatches:
		return &Op{Kind: OpMatches, Regex: l.regexes.Index(n.Regex), Args: args}
	case fnInt:
		if n.Args[0].Type().Kind == KindDouble {
			return &Op{Kind: OpToInt, Args: args}
		}
		return args[0]
	case fnDouble:
		if n.Args[0].Type().Kind == KindInt {
			return &Op{Kind: OpToDouble, Args: args}
		}
		return args[0]
	case fnHas:
		if sel, ok := n.Args[0].(*Select); ok {
			return &Op{Kind: OpHas, Name: sel.Field.Name, Ptr: PointerBacked(sel.Field), Args: []*Op{l.lower(sel.X)}}
		}
	}
	return &Op{Kind: OpBool}
}

func (l *lowerer) macro(n *Macro) *Op {
	rng := l.lower(n.Range)
	slot := len(l.vars)
	l.vars = append(l.vars, n.Var)
	body := l.lower(n.Body)
	l.vars = l.vars[:len(l.vars)-1]
	kind := OpExists
	if n.Kind == macroAll {
		kind = OpAll
	}
	return &Op{Kind: kind, Slot: slot, Args: []*Op{rng, body}}
}

func (l *lowerer) unary(n *Unary) *Op {
	x := l.lower(n.X)
	switch {
	case n.Op == "!":
		return &Op{Kind: OpNot, Args: []*Op{x}}
	case n.X.Type().Kind == KindInt:
		return &Op{Kind: OpNegInt, Args: []*Op{x}}
	}
	return &Op{Kind: OpNegDouble, Args: []*Op{x}}
}

func (l *lowerer) binary(n *Binary) *Op {
	args := []*Op{l.lower(n.L), l.lower(n.R)}
	if kind, ok := binaryKinds[n.Op]; ok {
		return &Op{Kind: kind, Args: args}
	}
	if n.L.Type().Kind == KindInt {
		return &Op{Kind: intKinds[n.Op], Args: args}
	}
	return &Op{Kind: doubleKinds[n.Op], Args: args}
}

func MaxSlot(op *Op) int {
	best := -1
	var walk func(o *Op)
	walk = func(o *Op) {
		if o.Kind == OpAll || o.Kind == OpExists {
			if o.Slot > best {
				best = o.Slot
			}
		}
		for _, a := range o.Args {
			walk(a)
		}
	}
	walk(op)
	return best + 1
}
