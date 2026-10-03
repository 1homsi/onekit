package gendart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

type dartRuleState struct {
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
	state := &dartRuleState{index: map[*onkir.Message]int{}, hasRules: map[*onkir.Message][]onkexpr.Rule{}}
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

func (s *dartRuleState) reach(m *onkir.Message) {
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
	k := p.rules.index[m]
	p.P("violations.addAll(_onkCheckRules(_onkRules", k, ", _onkValue", k, "(this)));")
}

func dartFloatLit(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnN") {
		s += ".0"
	}
	return s
}

var dartKindNames = map[onkexpr.OpKind]string{
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

var dartZeroNames = map[onkexpr.Zero]string{
	onkexpr.ZeroStr: "Str", onkexpr.ZeroInt: "Int", onkexpr.ZeroDouble: "Double", onkexpr.ZeroBool: "Bool",
	onkexpr.ZeroBytes: "Bytes", onkexpr.ZeroList: "List", onkexpr.ZeroMap: "Map", onkexpr.ZeroMsg: "Msg",
}

func dartOps(ops []*onkexpr.Op) string {
	items := make([]string, len(ops))
	for i, o := range ops {
		items[i] = dartOp(o)
	}
	return "<_OnkOp>[" + strings.Join(items, ", ") + "]"
}

func dartNode(kind string, args []*onkexpr.Op, value, name, zero string, n int) string {
	list := "_kNoArgs"
	if len(args) > 0 {
		list = dartOps(args)
	}
	return fmt.Sprintf("_OnkOp(%s, %s, %s, %s, %s, %d)", kind, list, value, name, zero, n)
}

func dartOp(op *onkexpr.Op) string {
	kind := "_kOp" + dartKindNames[op.Kind]
	switch op.Kind {
	case onkexpr.OpInt:
		if op.Int == -1<<63 {
			return dartNode(kind, nil, "-9223372036854775807 - 1", "''", "_kZeroStr", 0)
		}
		return dartNode(kind, nil, strconv.FormatInt(op.Int, 10), "''", "_kZeroStr", 0)
	case onkexpr.OpDouble:
		return dartNode(kind, nil, dartFloatLit(op.Double), "''", "_kZeroStr", 0)
	case onkexpr.OpStr:
		return dartNode(kind, nil, dartString(op.Str), "''", "_kZeroStr", 0)
	case onkexpr.OpBool:
		return dartNode(kind, nil, strconv.FormatBool(op.Bool), "''", "_kZeroStr", 0)
	case onkexpr.OpSlot:
		return dartNode(kind, nil, "null", "''", "_kZeroStr", op.Slot)
	case onkexpr.OpSelect:
		return dartNode(kind, op.Args, "null", dartString(op.Name), "_kZero"+dartZeroNames[op.Zero], 0)
	case onkexpr.OpHas:
		return dartNode(kind, op.Args, strconv.FormatBool(op.Ptr), dartString(op.Name), "_kZeroStr", 0)
	case onkexpr.OpMatches:
		return dartNode(kind, op.Args, "null", "''", "_kZeroStr", op.Regex)
	case onkexpr.OpAll, onkexpr.OpExists:
		return dartNode(kind, op.Args, "null", "''", "_kZeroStr", op.Slot)
	}
	return dartNode(kind, op.Args, "null", "''", "_kZeroStr", 0)
}

func (p *Printer) writeRuleSupport() {
	state := p.rules
	if state == nil || len(state.order) == 0 {
		return
	}
	for _, m := range state.order {
		rules := state.hasRules[m]
		if len(rules) == 0 {
			continue
		}
		k := state.index[m]
		p.P("final List<_OnkRule> _onkRules", k, " = <_OnkRule>[")
		p.Indent()
		for _, r := range rules {
			field := ""
			if r.Field != nil {
				field = r.Field.Name
			}
			op := onkexpr.Lower(r.Expr, r.Field, &state.regexes)
			p.P("_OnkRule(", dartString(field), ", ", dartString(r.Message), ", ", dartOp(op), "),")
		}
		p.Dedent()
		p.P("];")
		p.P()
	}
	p.writeRuleConverters()
	p.writeRuleRegexes()
	p.b.WriteString(dartRuleRuntimeSource)
}

func (p *Printer) writeRuleRegexes() {
	p.P("final List<RegExp> _onkRegexes = <RegExp>[")
	p.Indent()
	for _, pattern := range p.rules.regexes.Patterns {
		p.P("RegExp(", dartString("^(?:"+pattern+")$"), ", unicode: true),")
	}
	p.Dedent()
	p.P("];")
	p.P()
}

func (p *Printer) dartValueOf(t *onkir.Type, v string) string {
	switch t.Kind {
	case onkir.KindScalar:
		switch t.Scalar {
		case onkir.ScalarString, onkir.ScalarBool, onkir.ScalarInt32, onkir.ScalarUint32, onkir.ScalarInt64,
			onkir.ScalarFloat32, onkir.ScalarFloat64:
			return v
		case onkir.ScalarBytes:
			return "_OnkBytes(" + v + ".length)"
		}
	case onkir.KindEnum:
		return v + ".index"
	case onkir.KindMessage:
		return fmt.Sprintf("_onkValue%d(%s)", p.rules.index[t.Message], v)
	}
	return ""
}

func (p *Printer) writeRuleConverters() {
	state := p.rules
	for _, m := range state.order {
		k := state.index[m]
		p.P("_OnkMsg _onkValue", k, "(", p.MessageTypeName(m), " m) {")
		p.Indent()
		p.P("final f = <String, Object?>{};")
		for _, f := range m.Fields {
			p.writeRuleFieldConverter(f)
		}
		p.P("return _OnkMsg(f);")
		p.Dedent()
		p.P("}")
		p.P()
	}
}

func (p *Printer) writeRuleFieldConverter(f *onkir.Field) {
	if f.Oneof != nil || f.Type == nil {
		return
	}
	id := "m." + Ident(f.Name)
	key := dartString(f.Name)
	switch {
	case f.Repeated:
		item := p.dartValueOf(f.Type, "v")
		if item == "" {
			return
		}
		if item == "v" {
			p.P("f[", key, "] = ", id, ";")
			return
		}
		p.P("f[", key, "] = <Object?>[for (final v in ", id, ") ", item, "];")
	case f.Type.Kind == onkir.KindMap:
		if f.Type.MapValue == nil {
			return
		}
		item := p.dartValueOf(f.Type.MapValue, "e.value")
		if item == "" {
			return
		}
		if item == "e.value" {
			p.P("f[", key, "] = ", id, ";")
			return
		}
		p.P("f[", key, "] = <String, Object?>{for (final e in ", id, ".entries) e.key: ", item, "};")
	case isNullableKind(f):
		item := p.dartValueOf(f.Type, "v")
		if item == "" {
			return
		}
		p.P("{")
		p.Indent()
		p.P("final v = ", id, ";")
		p.P("if (v != null) f[", key, "] = ", item, ";")
		p.Dedent()
		p.P("}")
	default:
		item := p.dartValueOf(f.Type, id)
		if item == "" {
			return
		}
		p.P("f[", key, "] = ", item, ";")
	}
}

const dartRuleRuntimeSource = `class _OnkFail implements Exception {
  const _OnkFail();
}

class _OnkMsg {
  _OnkMsg(this.fields);
  final Map<String, Object?> fields;
}

class _OnkBytes {
  const _OnkBytes(this.length);
  final int length;
}

class _OnkOp {
  _OnkOp(this.kind, this.args, this.value, this.name, this.zero, this.n);
  final int kind;
  final List<_OnkOp> args;
  final Object? value;
  final String name;
  final int zero;
  final int n;
}

const List<_OnkOp> _kNoArgs = <_OnkOp>[];

class _OnkRule {
  _OnkRule(this.field, this.message, this.op);
  final String field;
  final String message;
  final _OnkOp op;
}

const int _kOpIntLit = 0;
const int _kOpDoubleLit = 1;
const int _kOpStrLit = 2;
const int _kOpBoolLit = 3;
const int _kOpList = 4;
const int _kOpSelf = 5;
const int _kOpSlot = 6;
const int _kOpSelect = 7;
const int _kOpIndexList = 8;
const int _kOpIndexMap = 9;
const int _kOpSize = 10;
const int _kOpStartsWith = 11;
const int _kOpEndsWith = 12;
const int _kOpContains = 13;
const int _kOpMatches = 14;
const int _kOpToInt = 15;
const int _kOpToDouble = 16;
const int _kOpHas = 17;
const int _kOpAll = 18;
const int _kOpExists = 19;
const int _kOpNot = 20;
const int _kOpNegInt = 21;
const int _kOpNegDouble = 22;
const int _kOpTernary = 23;
const int _kOpAnd = 24;
const int _kOpOr = 25;
const int _kOpEq = 26;
const int _kOpNe = 27;
const int _kOpLt = 28;
const int _kOpLe = 29;
const int _kOpGt = 30;
const int _kOpGe = 31;
const int _kOpIn = 32;
const int _kOpAdd = 33;
const int _kOpSub = 34;
const int _kOpMul = 35;
const int _kOpDiv = 36;
const int _kOpMod = 37;
const int _kOpFAdd = 38;
const int _kOpFSub = 39;
const int _kOpFMul = 40;
const int _kOpFDiv = 41;

const int _kZeroStr = 0;
const int _kZeroInt = 1;
const int _kZeroDouble = 2;
const int _kZeroBool = 3;
const int _kZeroBytes = 4;
const int _kZeroList = 5;
const int _kZeroMap = 6;
const int _kZeroMsg = 7;

final int _onkMinInt = (1 << 62) * 2;

Object? _onkZeroValue(int zero) {
  switch (zero) {
    case _kZeroStr:
      return '';
    case _kZeroInt:
      return 0;
    case _kZeroDouble:
      return 0.0;
    case _kZeroBool:
      return false;
    case _kZeroBytes:
      return const _OnkBytes(0);
    case _kZeroList:
      return <Object?>[];
    case _kZeroMap:
      return <String, Object?>{};
    case _kZeroMsg:
      return _OnkMsg(<String, Object?>{});
    default:
      return '';
  }
}

List<String> _onkCheckRules(List<_OnkRule> rules, _OnkMsg root) {
  final out = <String>[];
  for (final rule in rules) {
    var holds = false;
    try {
      holds = _onkEval(rule.op, root, <Object?>[]) == true;
    } catch (_) {
      holds = false;
    }
    if (!holds) out.add(rule.message);
  }
  return out;
}

int _onkInt(Object? v) {
  if (v is int) return v;
  throw const _OnkFail();
}

double _onkDouble(Object? v) {
  if (v is double) return v;
  throw const _OnkFail();
}

bool _onkBool(Object? v) {
  if (v is bool) return v;
  throw const _OnkFail();
}

String _onkStr(Object? v) {
  if (v is String) return v;
  throw const _OnkFail();
}

List<Object?> _onkList(Object? v) {
  if (v is List<Object?>) return v;
  throw const _OnkFail();
}

double _onkFinite(double v) {
  if (v.isFinite) return v;
  throw const _OnkFail();
}

bool _onkEqual(Object? a, Object? b) {
  if (a is int && b is int) return a == b;
  if (a is double && b is double) return a == b;
  if (a is String && b is String) return a == b;
  if (a is bool && b is bool) return a == b;
  throw const _OnkFail();
}

int _onkCompare(Object? a, Object? b) {
  if (a is int && b is int) return a < b ? -1 : (a == b ? 0 : 1);
  if (a is double && b is double) return a < b ? -1 : (a == b ? 0 : 1);
  throw const _OnkFail();
}

bool _onkPresent(Object? v) {
  if (v is String) return v.isNotEmpty;
  if (v is int) return v != 0;
  if (v is double) return v != 0.0;
  if (v is bool) return v;
  if (v is _OnkBytes) return v.length > 0;
  if (v is List<Object?>) return v.isNotEmpty;
  if (v is Map<String, Object?>) return v.isNotEmpty;
  return true;
}

int _onkAdd(int a, int b) {
  final r = a + b;
  if ((b > 0 && r < a) || (b < 0 && r > a)) throw const _OnkFail();
  return r;
}

int _onkSub(int a, int b) {
  final r = a - b;
  if ((b > 0 && r > a) || (b < 0 && r < a)) throw const _OnkFail();
  return r;
}

int _onkMul(int a, int b) {
  if (a == 0 || b == 0) return 0;
  if ((a == _onkMinInt && b == -1) || (b == _onkMinInt && a == -1)) throw const _OnkFail();
  final r = a * b;
  if (r ~/ b != a) throw const _OnkFail();
  return r;
}

Object? _onkEval(_OnkOp op, Object? root, List<Object?> slots) {
  switch (op.kind) {
    case _kOpIntLit:
    case _kOpDoubleLit:
    case _kOpStrLit:
    case _kOpBoolLit:
      return op.value;
    case _kOpList:
      return <Object?>[for (final a in op.args) _onkEval(a, root, slots)];
    case _kOpSelf:
      return root;
    case _kOpSlot:
      return slots[op.n];
    case _kOpSelect:
      {
        final base = _onkEval(op.args[0], root, slots);
        if (base is! _OnkMsg) throw const _OnkFail();
        return base.fields.containsKey(op.name) ? base.fields[op.name] : _onkZeroValue(op.zero);
      }
    case _kOpIndexList:
      {
        final items = _onkList(_onkEval(op.args[0], root, slots));
        final index = _onkInt(_onkEval(op.args[1], root, slots));
        if (index < 0 || index >= items.length) throw const _OnkFail();
        return items[index];
      }
    case _kOpIndexMap:
      {
        final entries = _onkEval(op.args[0], root, slots);
        final key = _onkStr(_onkEval(op.args[1], root, slots));
        if (entries is! Map<String, Object?> || !entries.containsKey(key)) throw const _OnkFail();
        return entries[key];
      }
    case _kOpSize:
      {
        final v = _onkEval(op.args[0], root, slots);
        if (v is String) return v.runes.length;
        if (v is _OnkBytes) return v.length;
        if (v is List<Object?>) return v.length;
        if (v is Map<String, Object?>) return v.length;
        throw const _OnkFail();
      }
    case _kOpStartsWith:
      return _onkStr(_onkEval(op.args[0], root, slots)).startsWith(_onkStr(_onkEval(op.args[1], root, slots)));
    case _kOpEndsWith:
      return _onkStr(_onkEval(op.args[0], root, slots)).endsWith(_onkStr(_onkEval(op.args[1], root, slots)));
    case _kOpContains:
      return _onkStr(_onkEval(op.args[0], root, slots)).contains(_onkStr(_onkEval(op.args[1], root, slots)));
    case _kOpMatches:
      return _onkRegexes[op.n].hasMatch(_onkStr(_onkEval(op.args[0], root, slots)));
    case _kOpToInt:
      {
        final v = _onkDouble(_onkEval(op.args[0], root, slots));
        if (!v.isFinite || v >= 9223372036854775808.0 || v < -9223372036854775808.0) throw const _OnkFail();
        return v.truncate();
      }
    case _kOpToDouble:
      return _onkInt(_onkEval(op.args[0], root, slots)).toDouble();
    case _kOpHas:
      {
        final base = _onkEval(op.args[0], root, slots);
        if (base is! _OnkMsg) throw const _OnkFail();
        if (!base.fields.containsKey(op.name)) return false;
        if (op.value == true) return true;
        return _onkPresent(base.fields[op.name]);
      }
    case _kOpAll:
    case _kOpExists:
      {
        final items = _onkList(_onkEval(op.args[0], root, slots));
        final wantAll = op.kind == _kOpAll;
        while (slots.length <= op.n) {
          slots.add(null);
        }
        var result = wantAll;
        for (final item in items) {
          slots[op.n] = item;
          final holds = _onkBool(_onkEval(op.args[1], root, slots));
          if (wantAll && !holds) {
            result = false;
            break;
          }
          if (!wantAll && holds) {
            result = true;
            break;
          }
        }
        return result;
      }
    case _kOpNot:
      return !_onkBool(_onkEval(op.args[0], root, slots));
    case _kOpNegInt:
      {
        final v = _onkInt(_onkEval(op.args[0], root, slots));
        if (v == _onkMinInt) throw const _OnkFail();
        return -v;
      }
    case _kOpNegDouble:
      return -_onkDouble(_onkEval(op.args[0], root, slots));
    case _kOpTernary:
      return _onkBool(_onkEval(op.args[0], root, slots))
          ? _onkEval(op.args[1], root, slots)
          : _onkEval(op.args[2], root, slots);
    case _kOpAnd:
      return _onkBool(_onkEval(op.args[0], root, slots)) && _onkBool(_onkEval(op.args[1], root, slots));
    case _kOpOr:
      return _onkBool(_onkEval(op.args[0], root, slots)) || _onkBool(_onkEval(op.args[1], root, slots));
    case _kOpEq:
      return _onkEqual(_onkEval(op.args[0], root, slots), _onkEval(op.args[1], root, slots));
    case _kOpNe:
      return !_onkEqual(_onkEval(op.args[0], root, slots), _onkEval(op.args[1], root, slots));
    case _kOpLt:
      return _onkCompare(_onkEval(op.args[0], root, slots), _onkEval(op.args[1], root, slots)) < 0;
    case _kOpLe:
      return _onkCompare(_onkEval(op.args[0], root, slots), _onkEval(op.args[1], root, slots)) <= 0;
    case _kOpGt:
      return _onkCompare(_onkEval(op.args[0], root, slots), _onkEval(op.args[1], root, slots)) > 0;
    case _kOpGe:
      return _onkCompare(_onkEval(op.args[0], root, slots), _onkEval(op.args[1], root, slots)) >= 0;
    case _kOpIn:
      {
        final needle = _onkEval(op.args[0], root, slots);
        final haystack = _onkEval(op.args[1], root, slots);
        if (haystack is List<Object?>) {
          for (final item in haystack) {
            if (_onkEqual(needle, item)) return true;
          }
          return false;
        }
        if (haystack is Map<String, Object?>) return haystack.containsKey(_onkStr(needle));
        throw const _OnkFail();
      }
    case _kOpAdd:
      return _onkAdd(_onkInt(_onkEval(op.args[0], root, slots)), _onkInt(_onkEval(op.args[1], root, slots)));
    case _kOpSub:
      return _onkSub(_onkInt(_onkEval(op.args[0], root, slots)), _onkInt(_onkEval(op.args[1], root, slots)));
    case _kOpMul:
      return _onkMul(_onkInt(_onkEval(op.args[0], root, slots)), _onkInt(_onkEval(op.args[1], root, slots)));
    case _kOpDiv:
      {
        final a = _onkInt(_onkEval(op.args[0], root, slots));
        final b = _onkInt(_onkEval(op.args[1], root, slots));
        if (b == 0 || (a == _onkMinInt && b == -1)) throw const _OnkFail();
        return a ~/ b;
      }
    case _kOpMod:
      {
        final a = _onkInt(_onkEval(op.args[0], root, slots));
        final b = _onkInt(_onkEval(op.args[1], root, slots));
        if (b == 0 || (a == _onkMinInt && b == -1)) throw const _OnkFail();
        return a.remainder(b);
      }
    case _kOpFAdd:
      return _onkFinite(_onkDouble(_onkEval(op.args[0], root, slots)) + _onkDouble(_onkEval(op.args[1], root, slots)));
    case _kOpFSub:
      return _onkFinite(_onkDouble(_onkEval(op.args[0], root, slots)) - _onkDouble(_onkEval(op.args[1], root, slots)));
    case _kOpFMul:
      return _onkFinite(_onkDouble(_onkEval(op.args[0], root, slots)) * _onkDouble(_onkEval(op.args[1], root, slots)));
    case _kOpFDiv:
      {
        final a = _onkDouble(_onkEval(op.args[0], root, slots));
        final b = _onkDouble(_onkEval(op.args[1], root, slots));
        if (b == 0) throw const _OnkFail();
        return _onkFinite(a / b);
      }
    default:
      throw const _OnkFail();
  }
}
`
