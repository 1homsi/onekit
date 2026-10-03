package genswift

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

type swiftRuleState struct {
	regexes  onkexpr.RegexTable
	index    map[*onkir.Message]int
	order    []*onkir.Message
	hasRules map[*onkir.Message][]onkexpr.Rule
}

func hasRuleDecorator(decorators []onkir.Decorator) bool {
	for _, d := range decorators {
		if d.Name == onkexpr.RuleDecorator {
			return true
		}
	}
	return false
}

func messageHasRules(m *onkir.Message) bool {
	if hasRuleDecorator(m.Decorators) {
		return true
	}
	for _, f := range m.Fields {
		if hasRuleDecorator(f.Decorators) {
			return true
		}
	}
	return false
}

func (p *Printer) prepareRules(file *onkir.File) {
	state := &swiftRuleState{index: map[*onkir.Message]int{}, hasRules: map[*onkir.Message][]onkexpr.Rule{}}
	p.rules = state
	for _, m := range fileMessagesDeep(file) {
		if !messageHasRules(m) {
			continue
		}
		rules, err := onkexpr.RulesFor(m)
		if err != nil {
			continue
		}
		state.hasRules[m] = rules
		state.reach(m)
	}
}

func (s *swiftRuleState) reach(m *onkir.Message) {
	if _, seen := s.index[m]; seen {
		return
	}
	s.index[m] = len(s.order)
	s.order = append(s.order, m)
	for _, f := range m.Fields {
		if f.Oneof != nil || f.Type == nil {
			continue
		}
		switch f.Type.Kind {
		case onkir.KindMessage:
			s.reach(f.Type.Message)
		case onkir.KindMap:
			if f.Type.MapValue != nil && f.Type.MapValue.Kind == onkir.KindMessage {
				s.reach(f.Type.MapValue.Message)
			}
		}
	}
}

func (p *Printer) rulesFor(m *onkir.Message) []onkexpr.Rule {
	if p.rules == nil {
		return nil
	}
	return p.rules.hasRules[m]
}

func (p *Printer) writeRuleValidateHook(m *onkir.Message) {
	if len(p.rulesFor(m)) == 0 {
		return
	}
	p.P("violations.append(contentsOf: onkCheckRules(onkRules", p.rules.index[m], ", self.onkRuleValue()))")
}

func swiftFloatLit(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnN") {
		s += ".0"
	}
	return s
}

var swiftKindNames = map[onkexpr.OpKind]string{
	onkexpr.OpInt: "IntLit", onkexpr.OpDouble: "DoubleLit", onkexpr.OpStr: "StrLit", onkexpr.OpBool: "BoolLit",
	onkexpr.OpList: "List", onkexpr.OpSelf: "Self", onkexpr.OpSlot: "Slot", onkexpr.OpSelect: "Select",
	onkexpr.OpIndexList: "IndexList", onkexpr.OpIndexMap: "IndexMap", onkexpr.OpSize: "Size",
	onkexpr.OpStartsWith: "StartsWith", onkexpr.OpEndsWith: "EndsWith", onkexpr.OpContains: "Contains",
	onkexpr.OpMatches: "Matches", onkexpr.OpToInt: "ToInt", onkexpr.OpToDouble: "ToDouble", onkexpr.OpHas: "Has",
	onkexpr.OpAll: "All", onkexpr.OpExists: "Exists", onkexpr.OpNot: "Not", onkexpr.OpNegInt: "NegInt",
	onkexpr.OpNegDouble: "NegDouble", onkexpr.OpTernary: "Ternary", onkexpr.OpAnd: "And", onkexpr.OpOr: "Or",
	onkexpr.OpEq: "Eq", onkexpr.OpNe: "Ne", onkexpr.OpLt: "Lt", onkexpr.OpLe: "Le", onkexpr.OpGt: "Gt", onkexpr.OpGe: "Ge",
	onkexpr.OpIn: "In", onkexpr.OpAdd: "Add", onkexpr.OpSub: "Sub", onkexpr.OpMul: "Mul", onkexpr.OpDiv: "Div",
	onkexpr.OpMod: "Mod", onkexpr.OpFAdd: "FAdd", onkexpr.OpFSub: "FSub", onkexpr.OpFMul: "FMul", onkexpr.OpFDiv: "FDiv",
}

var swiftZeroNames = map[onkexpr.Zero]string{
	onkexpr.ZeroStr: "Str", onkexpr.ZeroInt: "Int", onkexpr.ZeroDouble: "Double", onkexpr.ZeroBool: "Bool",
	onkexpr.ZeroBytes: "Bytes", onkexpr.ZeroList: "List", onkexpr.ZeroMap: "Map", onkexpr.ZeroMsg: "Msg",
}

func swiftNode(kind string, args []*onkexpr.Op, value, name, zero string, n int) string {
	items := make([]string, len(args))
	for i, a := range args {
		items[i] = swiftOp(a)
	}
	return fmt.Sprintf("OnkOp(%s, [%s], %s, %s, %s, %d)", kind, strings.Join(items, ", "), value, name, zero, n)
}

func swiftOp(op *onkexpr.Op) string {
	kind := "onkOp" + swiftKindNames[op.Kind]
	const noName, noZero = `""`, "onkZeroStr"
	switch op.Kind {
	case onkexpr.OpInt:
		if op.Int == -1<<63 {
			return swiftNode(kind, nil, ".int(Int64.min)", noName, noZero, 0)
		}
		return swiftNode(kind, nil, ".int("+strconv.FormatInt(op.Int, 10)+")", noName, noZero, 0)
	case onkexpr.OpDouble:
		return swiftNode(kind, nil, ".double("+swiftFloatLit(op.Double)+")", noName, noZero, 0)
	case onkexpr.OpStr:
		return swiftNode(kind, nil, ".str("+swiftString(op.Str)+")", noName, noZero, 0)
	case onkexpr.OpBool:
		return swiftNode(kind, nil, ".bool("+strconv.FormatBool(op.Bool)+")", noName, noZero, 0)
	case onkexpr.OpSlot:
		return swiftNode(kind, nil, "nil", noName, noZero, op.Slot)
	case onkexpr.OpSelect:
		return swiftNode(kind, op.Args, "nil", swiftString(op.Name), "onkZero"+swiftZeroNames[op.Zero], 0)
	case onkexpr.OpHas:
		return swiftNode(kind, op.Args, ".bool("+strconv.FormatBool(op.Ptr)+")", swiftString(op.Name), noZero, 0)
	case onkexpr.OpMatches:
		return swiftNode(kind, op.Args, "nil", noName, noZero, op.Regex)
	case onkexpr.OpAll, onkexpr.OpExists:
		return swiftNode(kind, op.Args, "nil", noName, noZero, op.Slot)
	}
	return swiftNode(kind, op.Args, "nil", noName, noZero, 0)
}

func (p *Printer) isExternalMessage(m *onkir.Message) bool {
	if p.resolver == nil {
		return false
	}
	namespace, ok := p.resolver.ResolveMessage(m)
	return ok && namespace != ""
}

func (p *Printer) writeRuleSupport() {
	state := p.rules
	if state == nil || len(state.order) == 0 {
		return
	}
	p.P()
	for _, m := range state.order {
		rules := state.hasRules[m]
		if len(rules) == 0 {
			continue
		}
		p.P("fileprivate let onkRules", state.index[m], ": [OnkRule] = [")
		p.Indent()
		for _, r := range rules {
			op := onkexpr.Lower(r.Expr, r.Field, &state.regexes)
			p.P("OnkRule(", swiftString(r.Message), ", ", swiftOp(op), "),")
		}
		p.Dedent()
		p.P("]")
		p.P()
	}
	for _, m := range state.order {
		p.writeRuleConverter(m)
	}
	p.P("fileprivate let onkRegexes = OnkRegexes(items: [")
	p.Indent()
	for _, pattern := range state.regexes.Patterns {
		p.P("try! NSRegularExpression(pattern: ", swiftString(`\A(?:`+pattern+`)\z`), ", options: []),")
	}
	p.Dedent()
	p.P("])")
	p.P()
	p.b.WriteString(swiftRuleRuntimeSource)
}

func swiftValueOf(t *onkir.Type, v string) string {
	switch t.Kind {
	case onkir.KindScalar:
		switch t.Scalar {
		case onkir.ScalarString:
			return ".str(" + v + ")"
		case onkir.ScalarBool:
			return ".bool(" + v + ")"
		case onkir.ScalarInt32, onkir.ScalarUint32, onkir.ScalarInt64:
			return ".int(Int64(" + v + "))"
		case onkir.ScalarFloat32, onkir.ScalarFloat64:
			return ".double(Double(" + v + "))"
		case onkir.ScalarBytes:
			return ".bytes(" + v + ".count)"
		}
	case onkir.KindEnum:
		return ".int(Int64(" + v + ".ordinal))"
	case onkir.KindMessage:
		return v + ".onkRuleValue()"
	}
	return ""
}

func (p *Printer) writeRuleConverter(m *onkir.Message) {
	var lines []string
	external := p.isExternalMessage(m)
	for _, f := range m.Fields {
		if line := p.ruleFieldLine(f, external); line != "" {
			lines = append(lines, line)
		}
	}
	name := p.messageRef(m)
	if !external && p.namespace != "" {
		name = p.namespace + "." + MessageName(m)
	}
	p.P("extension ", name, " {")
	p.Indent()
	p.P("fileprivate func onkRuleValue() -> OnkValue {")
	p.Indent()
	if len(lines) == 0 {
		p.P("return .msg([:])")
	} else {
		p.P("var f: [String: OnkValue] = [:]")
		for _, line := range lines {
			p.P(line)
		}
		p.P("return .msg(f)")
	}
	p.Dedent()
	p.P("}")
	p.Dedent()
	p.P("}")
	p.P()
}

func (p *Printer) ruleFieldLine(f *onkir.Field, external bool) string {
	if f.Oneof != nil || f.Type == nil {
		return ""
	}
	if external && isDeprecatedField(f) {
		return ""
	}
	id := "self." + Ident(f.Name)
	if !external {
		id = "self." + storageName(f)
	}
	key := swiftString(f.Name)
	switch {
	case f.Repeated:
		item := swiftValueOf(f.Type, "$0")
		if item == "" {
			return ""
		}
		return "f[" + key + "] = .list(" + id + ".map { " + item + " })"
	case f.Type.Kind == onkir.KindMap:
		if f.Type.MapValue == nil {
			return ""
		}
		item := swiftValueOf(f.Type.MapValue, "$0")
		if item == "" {
			return ""
		}
		return "f[" + key + "] = .map(" + id + ".mapValues { " + item + " })"
	case isNullableKind(f):
		item := swiftValueOf(f.Type, "v")
		if item == "" {
			return ""
		}
		return "if let v = " + id + " { f[" + key + "] = " + item + " }"
	}
	item := swiftValueOf(f.Type, id)
	if item == "" {
		return ""
	}
	return "f[" + key + "] = " + item
}

const swiftRuleRuntimeSource = `fileprivate struct OnkFail: Error {}

fileprivate enum OnkValue: Sendable {
    case bool(Bool)
    case int(Int64)
    case double(Double)
    case str(String)
    case bytes(Int)
    case list([OnkValue])
    case map([String: OnkValue])
    case msg([String: OnkValue])
}

fileprivate final class OnkOp: Sendable {
    let kind: Int
    let args: [OnkOp]
    let value: OnkValue?
    let name: String
    let zero: Int
    let n: Int

    init(_ kind: Int, _ args: [OnkOp], _ value: OnkValue?, _ name: String, _ zero: Int, _ n: Int) {
        self.kind = kind
        self.args = args
        self.value = value
        self.name = name
        self.zero = zero
        self.n = n
    }
}

fileprivate struct OnkRule: Sendable {
    let message: String
    let op: OnkOp

    init(_ message: String, _ op: OnkOp) {
        self.message = message
        self.op = op
    }
}

fileprivate struct OnkRegexes: @unchecked Sendable {
    let items: [NSRegularExpression]
}

fileprivate let onkOpIntLit = 0
fileprivate let onkOpDoubleLit = 1
fileprivate let onkOpStrLit = 2
fileprivate let onkOpBoolLit = 3
fileprivate let onkOpList = 4
fileprivate let onkOpSelf = 5
fileprivate let onkOpSlot = 6
fileprivate let onkOpSelect = 7
fileprivate let onkOpIndexList = 8
fileprivate let onkOpIndexMap = 9
fileprivate let onkOpSize = 10
fileprivate let onkOpStartsWith = 11
fileprivate let onkOpEndsWith = 12
fileprivate let onkOpContains = 13
fileprivate let onkOpMatches = 14
fileprivate let onkOpToInt = 15
fileprivate let onkOpToDouble = 16
fileprivate let onkOpHas = 17
fileprivate let onkOpAll = 18
fileprivate let onkOpExists = 19
fileprivate let onkOpNot = 20
fileprivate let onkOpNegInt = 21
fileprivate let onkOpNegDouble = 22
fileprivate let onkOpTernary = 23
fileprivate let onkOpAnd = 24
fileprivate let onkOpOr = 25
fileprivate let onkOpEq = 26
fileprivate let onkOpNe = 27
fileprivate let onkOpLt = 28
fileprivate let onkOpLe = 29
fileprivate let onkOpGt = 30
fileprivate let onkOpGe = 31
fileprivate let onkOpIn = 32
fileprivate let onkOpAdd = 33
fileprivate let onkOpSub = 34
fileprivate let onkOpMul = 35
fileprivate let onkOpDiv = 36
fileprivate let onkOpMod = 37
fileprivate let onkOpFAdd = 38
fileprivate let onkOpFSub = 39
fileprivate let onkOpFMul = 40
fileprivate let onkOpFDiv = 41

fileprivate let onkZeroStr = 0
fileprivate let onkZeroInt = 1
fileprivate let onkZeroDouble = 2
fileprivate let onkZeroBool = 3
fileprivate let onkZeroBytes = 4
fileprivate let onkZeroList = 5
fileprivate let onkZeroMap = 6
fileprivate let onkZeroMsg = 7

fileprivate func onkZeroValue(_ zero: Int) -> OnkValue {
    switch zero {
    case onkZeroInt: return .int(0)
    case onkZeroDouble: return .double(0)
    case onkZeroBool: return .bool(false)
    case onkZeroBytes: return .bytes(0)
    case onkZeroList: return .list([])
    case onkZeroMap: return .map([:])
    case onkZeroMsg: return .msg([:])
    default: return .str("")
    }
}

fileprivate func onkCheckRules(_ rules: [OnkRule], _ root: OnkValue) -> [String] {
    var out: [String] = []
    for rule in rules {
        var slots: [OnkValue] = []
        var holds = false
        if let result = try? onkEval(rule.op, root, &slots), case .bool(true) = result {
            holds = true
        }
        if !holds { out.append(rule.message) }
    }
    return out
}

fileprivate func onkInt(_ v: OnkValue) throws -> Int64 {
    if case .int(let i) = v { return i }
    throw OnkFail()
}

fileprivate func onkDouble(_ v: OnkValue) throws -> Double {
    if case .double(let d) = v { return d }
    throw OnkFail()
}

fileprivate func onkBool(_ v: OnkValue) throws -> Bool {
    if case .bool(let b) = v { return b }
    throw OnkFail()
}

fileprivate func onkStr(_ v: OnkValue) throws -> String {
    if case .str(let s) = v { return s }
    throw OnkFail()
}

fileprivate func onkList(_ v: OnkValue) throws -> [OnkValue] {
    if case .list(let items) = v { return items }
    throw OnkFail()
}

fileprivate func onkFinite(_ d: Double) throws -> OnkValue {
    if d.isFinite { return .double(d) }
    throw OnkFail()
}

fileprivate func onkSame(_ a: String, _ b: String) -> Bool {
    a.unicodeScalars.elementsEqual(b.unicodeScalars)
}

fileprivate func onkEqual(_ a: OnkValue, _ b: OnkValue) throws -> Bool {
    switch (a, b) {
    case (.int(let x), .int(let y)): return x == y
    case (.double(let x), .double(let y)): return x == y
    case (.str(let x), .str(let y)): return onkSame(x, y)
    case (.bool(let x), .bool(let y)): return x == y
    default: throw OnkFail()
    }
}

fileprivate func onkCompare(_ a: OnkValue, _ b: OnkValue) throws -> Int {
    switch (a, b) {
    case (.int(let x), .int(let y)): return x < y ? -1 : (x == y ? 0 : 1)
    case (.double(let x), .double(let y)): return x < y ? -1 : (x == y ? 0 : 1)
    default: throw OnkFail()
    }
}

fileprivate func onkPresent(_ v: OnkValue) -> Bool {
    switch v {
    case .str(let s): return !s.isEmpty
    case .int(let i): return i != 0
    case .double(let d): return d != 0
    case .bool(let b): return b
    case .bytes(let n): return n > 0
    case .list(let items): return !items.isEmpty
    case .map(let entries): return !entries.isEmpty
    case .msg: return true
    }
}

fileprivate func onkHasPrefix(_ a: String, _ b: String) -> Bool {
    let x = Array(a.unicodeScalars)
    let y = Array(b.unicodeScalars)
    return x.count >= y.count && x[0..<y.count].elementsEqual(y)
}

fileprivate func onkHasSuffix(_ a: String, _ b: String) -> Bool {
    let x = Array(a.unicodeScalars)
    let y = Array(b.unicodeScalars)
    return x.count >= y.count && x[(x.count - y.count)...].elementsEqual(y)
}

fileprivate func onkContains(_ a: String, _ b: String) -> Bool {
    let x = Array(a.unicodeScalars)
    let y = Array(b.unicodeScalars)
    if y.isEmpty { return true }
    if y.count > x.count { return false }
    for i in 0...(x.count - y.count) where x[i..<(i + y.count)].elementsEqual(y) {
        return true
    }
    return false
}

fileprivate func onkMatches(_ index: Int, _ text: String) -> Bool {
    let range = NSRange(location: 0, length: text.utf16.count)
    return onkRegexes.items[index].firstMatch(in: text, options: [], range: range) != nil
}

fileprivate func onkEval(_ op: OnkOp, _ root: OnkValue, _ slots: inout [OnkValue]) throws -> OnkValue {
    func arg(_ k: Int) throws -> OnkValue { try onkEval(op.args[k], root, &slots) }
    switch op.kind {
    case onkOpIntLit, onkOpDoubleLit, onkOpStrLit, onkOpBoolLit:
        guard let v = op.value else { throw OnkFail() }
        return v
    case onkOpList:
        var out: [OnkValue] = []
        for k in 0..<op.args.count { out.append(try arg(k)) }
        return .list(out)
    case onkOpSelf:
        return root
    case onkOpSlot:
        return slots[op.n]
    case onkOpSelect:
        guard case .msg(let fields) = try arg(0) else { throw OnkFail() }
        return fields[op.name] ?? onkZeroValue(op.zero)
    case onkOpIndexList:
        let items = try onkList(arg(0))
        let index = try onkInt(arg(1))
        if index < 0 || index >= Int64(items.count) { throw OnkFail() }
        return items[Int(index)]
    case onkOpIndexMap:
        guard case .map(let entries) = try arg(0) else { throw OnkFail() }
        let key = try onkStr(arg(1))
        guard let found = entries[key] else { throw OnkFail() }
        return found
    case onkOpSize:
        switch try arg(0) {
        case .str(let s): return .int(Int64(s.unicodeScalars.count))
        case .bytes(let n): return .int(Int64(n))
        case .list(let items): return .int(Int64(items.count))
        case .map(let entries): return .int(Int64(entries.count))
        default: throw OnkFail()
        }
    case onkOpStartsWith:
        return .bool(onkHasPrefix(try onkStr(arg(0)), try onkStr(arg(1))))
    case onkOpEndsWith:
        return .bool(onkHasSuffix(try onkStr(arg(0)), try onkStr(arg(1))))
    case onkOpContains:
        return .bool(onkContains(try onkStr(arg(0)), try onkStr(arg(1))))
    case onkOpMatches:
        return .bool(onkMatches(op.n, try onkStr(arg(0))))
    case onkOpToInt:
        let d = try onkDouble(arg(0))
        if !d.isFinite || d >= 9223372036854775808.0 || d < -9223372036854775808.0 { throw OnkFail() }
        return .int(Int64(d.rounded(.towardZero)))
    case onkOpToDouble:
        return .double(Double(try onkInt(arg(0))))
    case onkOpHas:
        guard case .msg(let fields) = try arg(0) else { throw OnkFail() }
        guard let present = fields[op.name] else { return .bool(false) }
        if case .bool(true)? = op.value { return .bool(true) }
        return .bool(onkPresent(present))
    case onkOpAll, onkOpExists:
        let items = try onkList(arg(0))
        let wantAll = op.kind == onkOpAll
        while slots.count <= op.n { slots.append(.bool(false)) }
        var result = wantAll
        for item in items {
            slots[op.n] = item
            let holds = try onkBool(arg(1))
            if wantAll && !holds {
                result = false
                break
            }
            if !wantAll && holds {
                result = true
                break
            }
        }
        return .bool(result)
    case onkOpNot:
        return .bool(!(try onkBool(arg(0))))
    case onkOpNegInt:
        let v = try onkInt(arg(0))
        if v == Int64.min { throw OnkFail() }
        return .int(-v)
    case onkOpNegDouble:
        return .double(-(try onkDouble(arg(0))))
    case onkOpTernary:
        return try onkBool(arg(0)) ? arg(1) : arg(2)
    case onkOpAnd:
        if !(try onkBool(arg(0))) { return .bool(false) }
        return .bool(try onkBool(arg(1)))
    case onkOpOr:
        if try onkBool(arg(0)) { return .bool(true) }
        return .bool(try onkBool(arg(1)))
    case onkOpEq:
        return .bool(try onkEqual(arg(0), arg(1)))
    case onkOpNe:
        return .bool(!(try onkEqual(arg(0), arg(1))))
    case onkOpLt:
        return .bool(try onkCompare(arg(0), arg(1)) < 0)
    case onkOpLe:
        return .bool(try onkCompare(arg(0), arg(1)) <= 0)
    case onkOpGt:
        return .bool(try onkCompare(arg(0), arg(1)) > 0)
    case onkOpGe:
        return .bool(try onkCompare(arg(0), arg(1)) >= 0)
    case onkOpIn:
        let needle = try arg(0)
        switch try arg(1) {
        case .list(let items):
            for item in items {
                if try onkEqual(needle, item) { return .bool(true) }
            }
            return .bool(false)
        case .map(let entries):
            let key = try onkStr(needle)
            return .bool(entries[key] != nil)
        default:
            throw OnkFail()
        }
    case onkOpAdd:
        let (r, o) = try onkInt(arg(0)).addingReportingOverflow(try onkInt(arg(1)))
        if o { throw OnkFail() }
        return .int(r)
    case onkOpSub:
        let (r, o) = try onkInt(arg(0)).subtractingReportingOverflow(try onkInt(arg(1)))
        if o { throw OnkFail() }
        return .int(r)
    case onkOpMul:
        let (r, o) = try onkInt(arg(0)).multipliedReportingOverflow(by: try onkInt(arg(1)))
        if o { throw OnkFail() }
        return .int(r)
    case onkOpDiv:
        let a = try onkInt(arg(0))
        let b = try onkInt(arg(1))
        let (r, o) = a.dividedReportingOverflow(by: b)
        if o { throw OnkFail() }
        return .int(r)
    case onkOpMod:
        let a = try onkInt(arg(0))
        let b = try onkInt(arg(1))
        let (r, o) = a.remainderReportingOverflow(dividingBy: b)
        if o { throw OnkFail() }
        return .int(r)
    case onkOpFAdd:
        return try onkFinite(try onkDouble(arg(0)) + onkDouble(arg(1)))
    case onkOpFSub:
        return try onkFinite(try onkDouble(arg(0)) - onkDouble(arg(1)))
    case onkOpFMul:
        return try onkFinite(try onkDouble(arg(0)) * onkDouble(arg(1)))
    case onkOpFDiv:
        let a = try onkDouble(arg(0))
        let b = try onkDouble(arg(1))
        if b == 0 { throw OnkFail() }
        return try onkFinite(a / b)
    default:
        throw OnkFail()
    }
}
`
