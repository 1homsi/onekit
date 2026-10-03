package genrust

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

type rustRuleState struct {
	regexes    onkexpr.RegexTable
	index      map[*onkir.Message]int
	order      []*onkir.Message
	hasRules   map[*onkir.Message][]onkexpr.Rule
	prepareErr error
	auth       []rustAuthEntry
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
	state := &rustRuleState{index: map[*onkir.Message]int{}, hasRules: map[*onkir.Message][]onkexpr.Rule{}}
	p.rules = state
	for _, m := range fileMessagesDeep(file) {
		if !messageHasRules(m) {
			continue
		}
		rules, err := onkexpr.RulesFor(m)
		if err != nil {
			state.prepareErr = err
			continue
		}
		state.hasRules[m] = rules
		state.reach(m)
	}
	p.prepareAuthorize(file, state)
}

func (s *rustRuleState) reach(m *onkir.Message) {
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

func rustStringLit(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029:
			fmt.Fprintf(&b, `\u{%x}`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func rustFloatLit(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnN") {
		s += ".0"
	}
	return s
}

var rustZeroNames = map[onkexpr.Zero]string{
	onkexpr.ZeroStr: "Str", onkexpr.ZeroInt: "Int", onkexpr.ZeroDouble: "Double", onkexpr.ZeroBool: "Bool",
	onkexpr.ZeroBytes: "Bytes", onkexpr.ZeroList: "List", onkexpr.ZeroMap: "Map", onkexpr.ZeroMsg: "Msg",
}

var rustOpNames = map[onkexpr.OpKind]string{
	onkexpr.OpSize: "Size", onkexpr.OpStartsWith: "StartsWith", onkexpr.OpEndsWith: "EndsWith", onkexpr.OpContains: "Contains",
	onkexpr.OpToInt: "ToInt", onkexpr.OpToDouble: "ToDouble", onkexpr.OpNot: "Not", onkexpr.OpNegInt: "NegInt",
	onkexpr.OpNegDouble: "NegDouble", onkexpr.OpTernary: "Ternary", onkexpr.OpAnd: "And", onkexpr.OpOr: "Or",
	onkexpr.OpEq: "Eq", onkexpr.OpNe: "Ne", onkexpr.OpLt: "Lt", onkexpr.OpLe: "Le", onkexpr.OpGt: "Gt", onkexpr.OpGe: "Ge",
	onkexpr.OpIn: "In", onkexpr.OpAdd: "Add", onkexpr.OpSub: "Sub", onkexpr.OpMul: "Mul", onkexpr.OpDiv: "Div",
	onkexpr.OpMod: "Mod", onkexpr.OpFAdd: "FAdd", onkexpr.OpFSub: "FSub", onkexpr.OpFMul: "FMul", onkexpr.OpFDiv: "FDiv",
	onkexpr.OpIndexList: "IndexList", onkexpr.OpIndexMap: "IndexMap",
}

func rustBox(op *onkexpr.Op) string {
	return "Box::new(" + rustOp(op) + ")"
}

func rustOp(op *onkexpr.Op) string {
	switch op.Kind {
	case onkexpr.OpInt:
		if op.Int == -1<<63 {
			return "OnkOp::Int(i64::MIN)"
		}
		return fmt.Sprintf("OnkOp::Int(%d)", op.Int)
	case onkexpr.OpDouble:
		return "OnkOp::Double(" + rustFloatLit(op.Double) + ")"
	case onkexpr.OpStr:
		return "OnkOp::Str(" + rustStringLit(op.Str) + ")"
	case onkexpr.OpBool:
		return "OnkOp::Bool(" + strconv.FormatBool(op.Bool) + ")"
	case onkexpr.OpList:
		items := make([]string, len(op.Args))
		for i, a := range op.Args {
			items[i] = rustOp(a)
		}
		return "OnkOp::List(vec![" + strings.Join(items, ", ") + "])"
	case onkexpr.OpSelf:
		return "OnkOp::SelfRef"
	case onkexpr.OpSlot:
		return fmt.Sprintf("OnkOp::Slot(%d)", op.Slot)
	case onkexpr.OpSelect:
		return "OnkOp::Select(" + rustBox(op.Args[0]) + ", " + rustStringLit(op.Name) + ", OnkZero::" + rustZeroNames[op.Zero] + ")"
	case onkexpr.OpHas:
		return "OnkOp::Has(" + rustBox(op.Args[0]) + ", " + rustStringLit(op.Name) + ", " + strconv.FormatBool(op.Ptr) + ")"
	case onkexpr.OpMatches:
		return fmt.Sprintf("OnkOp::Matches(%s, %d)", rustBox(op.Args[0]), op.Regex)
	case onkexpr.OpAll, onkexpr.OpExists:
		name := "All"
		if op.Kind == onkexpr.OpExists {
			name = "Exists"
		}
		return fmt.Sprintf("OnkOp::%s(%s, %s, %d)", name, rustBox(op.Args[0]), rustBox(op.Args[1]), op.Slot)
	}
	args := make([]string, len(op.Args))
	for i, a := range op.Args {
		args[i] = rustBox(a)
	}
	return "OnkOp::" + rustOpNames[op.Kind] + "(" + strings.Join(args, ", ") + ")"
}

func (p *Printer) writeRuleMethods(m *onkir.Message) {
	rules := p.rulesFor(m)
	if len(rules) == 0 {
		return
	}
	k := p.rules.index[m]
	p.P("pub fn rule_violations(&self) -> Vec<", p.validationError, "> {")
	p.Indent()
	p.P("let root = onk_value_", k, "(self);")
	p.P("onk_rules_", k, "().iter().filter(|rule| !onk_holds(&rule.op, &root)).map(|rule| ", p.validationError, " { field: rule.field, message: rule.message.to_string() }).collect()")
	p.Dedent()
	p.P("}")
}

func (p *Printer) writeRuleValidateHook(m *onkir.Message) {
	if len(p.rulesFor(m)) == 0 {
		return
	}
	p.P("if let Some(error) = self.rule_violations().into_iter().next() {")
	p.Indent()
	p.P("return Err(error);")
	p.Dedent()
	p.P("}")
}

func (p *Printer) writeRuleSupport() {
	state := p.rules
	if state == nil || len(state.order) == 0 {
		return
	}
	p.writeRuleTables()
	p.writeRuleConverters()
	p.writeAuthorizeTables()
	p.writeRuleRegexes()
	p.P(rustRuleRuntimeSource)
}

func (p *Printer) writeRuleTables() {
	state := p.rules
	for _, m := range state.order {
		rules := state.hasRules[m]
		if len(rules) == 0 {
			continue
		}
		k := state.index[m]
		p.P("fn onk_rules_", k, "() -> &'static [OnkRule] {")
		p.Indent()
		p.P("static RULES: std::sync::OnceLock<Vec<OnkRule>> = std::sync::OnceLock::new();")
		p.P("RULES.get_or_init(|| vec![")
		p.Indent()
		for _, r := range rules {
			field := ""
			if r.Field != nil {
				field = r.Field.Name
			}
			op := onkexpr.Lower(r.Expr, r.Field, &state.regexes)
			p.P("OnkRule { field: ", rustStringLit(field), ", message: ", rustStringLit(r.Message), ", op: ", rustOp(op), " },")
		}
		p.Dedent()
		p.P("])")
		p.Dedent()
		p.P("}")
		p.Blank()
	}
}

func (p *Printer) rustValueOf(t *onkir.Type) string {
	switch t.Kind {
	case onkir.KindScalar:
		switch t.Scalar {
		case onkir.ScalarString:
			return "OnkValue::Str(v.as_str())"
		case onkir.ScalarBool:
			return "OnkValue::Bool(*v)"
		case onkir.ScalarInt32, onkir.ScalarUint32, onkir.ScalarInt64:
			return "OnkValue::Int(*v as i64)"
		case onkir.ScalarFloat32, onkir.ScalarFloat64:
			return "OnkValue::Double(*v as f64)"
		case onkir.ScalarBytes:
			return "OnkValue::Bytes(v.len())"
		}
	case onkir.KindEnum:
		return "OnkValue::Int(*v as i64)"
	case onkir.KindMessage:
		return fmt.Sprintf("onk_value_%d(v)", p.rules.index[t.Message])
	}
	return ""
}

func (p *Printer) writeRuleConverters() {
	state := p.rules
	for _, m := range state.order {
		k := state.index[m]
		p.P("fn onk_value_", k, "(m: &", p.MessageTypeName(m), ") -> OnkValue<'_> {")
		p.Indent()
		p.P("let mut f: std::collections::HashMap<&'static str, OnkValue<'_>> = std::collections::HashMap::new();")
		for _, f := range m.Fields {
			p.writeRuleFieldConverter(f)
		}
		p.P("OnkValue::Msg(std::rc::Rc::new(f))")
		p.Dedent()
		p.P("}")
		p.Blank()
	}
}

func (p *Printer) writeRuleFieldConverter(f *onkir.Field) {
	if f.Oneof != nil || f.Type == nil {
		return
	}
	ident := "m." + RustIdent(f.Name)
	key := rustStringLit(f.Name)
	switch {
	case f.Repeated:
		item := p.rustValueOf(f.Type)
		if item == "" {
			return
		}
		p.P("f.insert(", key, ", OnkValue::List(std::rc::Rc::new(", ident, ".iter().map(|v| ", item, ").collect())));")
	case f.Type.Kind == onkir.KindMap:
		if f.Type.MapKey != onkir.ScalarString || f.Type.MapValue == nil {
			return
		}
		item := p.rustValueOf(f.Type.MapValue)
		if item == "" {
			return
		}
		p.P("f.insert(", key, ", OnkValue::Map(std::rc::Rc::new(", ident, ".iter().map(|(k, v)| (k.as_str(), ", item, ")).collect())));")
	case f.Optional || f.Type.Kind == onkir.KindMessage:
		item := p.rustValueOf(f.Type)
		if item == "" {
			return
		}
		p.P("if let Some(v) = &", ident, " { f.insert(", key, ", ", item, "); }")
	default:
		item := p.rustValueOf(f.Type)
		if item == "" {
			return
		}
		p.P("{ let v = &", ident, "; f.insert(", key, ", ", item, "); }")
	}
}

func (p *Printer) writeRuleRegexes() {
	patterns := p.rules.regexes.Patterns
	if len(patterns) == 0 {
		p.P("fn onk_match(_index: usize, _text: &str) -> bool { false }")
		p.Blank()
		return
	}
	p.P("const ONK_PATTERNS: &[&str] = &[")
	p.Indent()
	for _, pattern := range patterns {
		p.P(rustStringLit(`\A(?:`+pattern+`)\z`), ",")
	}
	p.Dedent()
	p.P("];")
	p.Blank()
	p.P("fn onk_match(index: usize, text: &str) -> bool {")
	p.Indent()
	p.P("static REGEXES: std::sync::OnceLock<Vec<regex::Regex>> = std::sync::OnceLock::new();")
	p.P("let regexes = REGEXES.get_or_init(|| ONK_PATTERNS.iter().map(|pattern| regex::Regex::new(pattern).expect(\"schema pattern was validated\")).collect());")
	p.P("regexes[index].is_match(text)")
	p.Dedent()
	p.P("}")
	p.Blank()
}

const rustRuleRuntimeSource = `#[derive(Clone)]
enum OnkValue<'a> {
    Bool(bool),
    Int(i64),
    Double(f64),
    Str(&'a str),
    Bytes(usize),
    List(std::rc::Rc<Vec<OnkValue<'a>>>),
    Map(std::rc::Rc<std::collections::HashMap<&'a str, OnkValue<'a>>>),
    Msg(std::rc::Rc<std::collections::HashMap<&'static str, OnkValue<'a>>>),
}

#[derive(Clone, Copy)]
enum OnkZero {
    Str,
    Int,
    Double,
    Bool,
    Bytes,
    List,
    Map,
    Msg,
}

enum OnkOp {
    Int(i64),
    Double(f64),
    Str(&'static str),
    Bool(bool),
    List(Vec<OnkOp>),
    SelfRef,
    Slot(usize),
    Select(Box<OnkOp>, &'static str, OnkZero),
    IndexList(Box<OnkOp>, Box<OnkOp>),
    IndexMap(Box<OnkOp>, Box<OnkOp>),
    Size(Box<OnkOp>),
    StartsWith(Box<OnkOp>, Box<OnkOp>),
    EndsWith(Box<OnkOp>, Box<OnkOp>),
    Contains(Box<OnkOp>, Box<OnkOp>),
    Matches(Box<OnkOp>, usize),
    ToInt(Box<OnkOp>),
    ToDouble(Box<OnkOp>),
    Has(Box<OnkOp>, &'static str, bool),
    All(Box<OnkOp>, Box<OnkOp>, usize),
    Exists(Box<OnkOp>, Box<OnkOp>, usize),
    Not(Box<OnkOp>),
    NegInt(Box<OnkOp>),
    NegDouble(Box<OnkOp>),
    Ternary(Box<OnkOp>, Box<OnkOp>, Box<OnkOp>),
    And(Box<OnkOp>, Box<OnkOp>),
    Or(Box<OnkOp>, Box<OnkOp>),
    Eq(Box<OnkOp>, Box<OnkOp>),
    Ne(Box<OnkOp>, Box<OnkOp>),
    Lt(Box<OnkOp>, Box<OnkOp>),
    Le(Box<OnkOp>, Box<OnkOp>),
    Gt(Box<OnkOp>, Box<OnkOp>),
    Ge(Box<OnkOp>, Box<OnkOp>),
    In(Box<OnkOp>, Box<OnkOp>),
    Add(Box<OnkOp>, Box<OnkOp>),
    Sub(Box<OnkOp>, Box<OnkOp>),
    Mul(Box<OnkOp>, Box<OnkOp>),
    Div(Box<OnkOp>, Box<OnkOp>),
    Mod(Box<OnkOp>, Box<OnkOp>),
    FAdd(Box<OnkOp>, Box<OnkOp>),
    FSub(Box<OnkOp>, Box<OnkOp>),
    FMul(Box<OnkOp>, Box<OnkOp>),
    FDiv(Box<OnkOp>, Box<OnkOp>),
}

struct OnkRule {
    field: &'static str,
    message: &'static str,
    op: OnkOp,
}

struct OnkFail;

type OnkResult<'a> = Result<OnkValue<'a>, OnkFail>;

fn onk_zero<'a>(zero: OnkZero) -> OnkValue<'a> {
    match zero {
        OnkZero::Str => OnkValue::Str(""),
        OnkZero::Int => OnkValue::Int(0),
        OnkZero::Double => OnkValue::Double(0.0),
        OnkZero::Bool => OnkValue::Bool(false),
        OnkZero::Bytes => OnkValue::Bytes(0),
        OnkZero::List => OnkValue::List(std::rc::Rc::new(Vec::new())),
        OnkZero::Map => OnkValue::Map(std::rc::Rc::new(std::collections::HashMap::new())),
        OnkZero::Msg => OnkValue::Msg(std::rc::Rc::new(std::collections::HashMap::new())),
    }
}

fn onk_holds(op: &OnkOp, root: &OnkValue<'_>) -> bool {
    matches!(onk_eval(op, root, &mut Vec::new()), Ok(OnkValue::Bool(true)))
}

fn onk_int(value: OnkResult<'_>) -> Result<i64, OnkFail> {
    match value? {
        OnkValue::Int(v) => Ok(v),
        _ => Err(OnkFail),
    }
}

fn onk_double(value: OnkResult<'_>) -> Result<f64, OnkFail> {
    match value? {
        OnkValue::Double(v) => Ok(v),
        _ => Err(OnkFail),
    }
}

fn onk_bool(value: OnkResult<'_>) -> Result<bool, OnkFail> {
    match value? {
        OnkValue::Bool(v) => Ok(v),
        _ => Err(OnkFail),
    }
}

fn onk_str<'a>(value: OnkResult<'a>) -> Result<&'a str, OnkFail> {
    match value? {
        OnkValue::Str(v) => Ok(v),
        _ => Err(OnkFail),
    }
}

fn onk_list<'a>(value: OnkResult<'a>) -> Result<std::rc::Rc<Vec<OnkValue<'a>>>, OnkFail> {
    match value? {
        OnkValue::List(v) => Ok(v),
        _ => Err(OnkFail),
    }
}

fn onk_finite(value: f64) -> OnkResult<'static> {
    if value.is_finite() {
        Ok(OnkValue::Double(value))
    } else {
        Err(OnkFail)
    }
}

fn onk_equal(a: &OnkValue<'_>, b: &OnkValue<'_>) -> Result<bool, OnkFail> {
    match (a, b) {
        (OnkValue::Int(x), OnkValue::Int(y)) => Ok(x == y),
        (OnkValue::Double(x), OnkValue::Double(y)) => Ok(x == y),
        (OnkValue::Str(x), OnkValue::Str(y)) => Ok(x == y),
        (OnkValue::Bool(x), OnkValue::Bool(y)) => Ok(x == y),
        _ => Err(OnkFail),
    }
}

fn onk_order(a: &OnkValue<'_>, b: &OnkValue<'_>) -> Result<std::cmp::Ordering, OnkFail> {
    match (a, b) {
        (OnkValue::Int(x), OnkValue::Int(y)) => Ok(x.cmp(y)),
        (OnkValue::Double(x), OnkValue::Double(y)) => x.partial_cmp(y).ok_or(OnkFail),
        _ => Err(OnkFail),
    }
}

fn onk_present(value: &OnkValue<'_>) -> bool {
    match value {
        OnkValue::Str(v) => !v.is_empty(),
        OnkValue::Int(v) => *v != 0,
        OnkValue::Double(v) => *v != 0.0,
        OnkValue::Bool(v) => *v,
        OnkValue::Bytes(v) => *v > 0,
        OnkValue::List(v) => !v.is_empty(),
        OnkValue::Map(v) => !v.is_empty(),
        OnkValue::Msg(_) => true,
    }
}

fn onk_eval<'a>(op: &OnkOp, root: &OnkValue<'a>, slots: &mut Vec<OnkValue<'a>>) -> OnkResult<'a> {
    Ok(match op {
        OnkOp::Int(v) => OnkValue::Int(*v),
        OnkOp::Double(v) => OnkValue::Double(*v),
        OnkOp::Str(v) => OnkValue::Str(v),
        OnkOp::Bool(v) => OnkValue::Bool(*v),
        OnkOp::List(items) => {
            let mut out = Vec::with_capacity(items.len());
            for item in items {
                out.push(onk_eval(item, root, slots)?);
            }
            OnkValue::List(std::rc::Rc::new(out))
        }
        OnkOp::SelfRef => root.clone(),
        OnkOp::Slot(index) => slots[*index].clone(),
        OnkOp::Select(base, name, zero) => match onk_eval(base, root, slots)? {
            OnkValue::Msg(fields) => match fields.get(name) {
                Some(value) => value.clone(),
                None => onk_zero(*zero),
            },
            _ => return Err(OnkFail),
        },
        OnkOp::IndexList(list, index) => {
            let items = onk_list(onk_eval(list, root, slots))?;
            let position = onk_int(onk_eval(index, root, slots))?;
            if position < 0 || position as usize >= items.len() {
                return Err(OnkFail);
            }
            items[position as usize].clone()
        }
        OnkOp::IndexMap(map, key) => {
            let container = onk_eval(map, root, slots)?;
            let key = onk_str(onk_eval(key, root, slots))?;
            match container {
                OnkValue::Map(entries) => entries.get(key).cloned().ok_or(OnkFail)?,
                _ => return Err(OnkFail),
            }
        }
        OnkOp::Size(value) => match onk_eval(value, root, slots)? {
            OnkValue::Str(v) => OnkValue::Int(v.chars().count() as i64),
            OnkValue::Bytes(v) => OnkValue::Int(v as i64),
            OnkValue::List(v) => OnkValue::Int(v.len() as i64),
            OnkValue::Map(v) => OnkValue::Int(v.len() as i64),
            _ => return Err(OnkFail),
        },
        OnkOp::StartsWith(a, b) => {
            let (a, b) = (onk_str(onk_eval(a, root, slots))?, onk_str(onk_eval(b, root, slots))?);
            OnkValue::Bool(a.starts_with(b))
        }
        OnkOp::EndsWith(a, b) => {
            let (a, b) = (onk_str(onk_eval(a, root, slots))?, onk_str(onk_eval(b, root, slots))?);
            OnkValue::Bool(a.ends_with(b))
        }
        OnkOp::Contains(a, b) => {
            let (a, b) = (onk_str(onk_eval(a, root, slots))?, onk_str(onk_eval(b, root, slots))?);
            OnkValue::Bool(a.contains(b))
        }
        OnkOp::Matches(value, index) => OnkValue::Bool(onk_match(*index, onk_str(onk_eval(value, root, slots))?)),
        OnkOp::ToInt(value) => {
            let v = onk_double(onk_eval(value, root, slots))?;
            if !v.is_finite() || v >= 9223372036854775808.0 || v < -9223372036854775808.0 {
                return Err(OnkFail);
            }
            OnkValue::Int(v as i64)
        }
        OnkOp::ToDouble(value) => OnkValue::Double(onk_int(onk_eval(value, root, slots))? as f64),
        OnkOp::Has(base, name, pointer) => match onk_eval(base, root, slots)? {
            OnkValue::Msg(fields) => OnkValue::Bool(match fields.get(name) {
                None => false,
                Some(_) if *pointer => true,
                Some(value) => onk_present(value),
            }),
            _ => return Err(OnkFail),
        },
        OnkOp::All(list, body, slot) | OnkOp::Exists(list, body, slot) => {
            let items = onk_list(onk_eval(list, root, slots))?;
            let want_all = matches!(op, OnkOp::All(..));
            if slots.len() <= *slot {
                slots.resize(*slot + 1, OnkValue::Bool(false));
            }
            let mut result = want_all;
            for item in items.iter() {
                slots[*slot] = item.clone();
                let holds = onk_bool(onk_eval(body, root, slots))?;
                if want_all && !holds {
                    result = false;
                    break;
                }
                if !want_all && holds {
                    result = true;
                    break;
                }
            }
            OnkValue::Bool(result)
        }
        OnkOp::Not(value) => OnkValue::Bool(!onk_bool(onk_eval(value, root, slots))?),
        OnkOp::NegInt(value) => OnkValue::Int(onk_int(onk_eval(value, root, slots))?.checked_neg().ok_or(OnkFail)?),
        OnkOp::NegDouble(value) => OnkValue::Double(-onk_double(onk_eval(value, root, slots))?),
        OnkOp::Ternary(cond, a, b) => {
            if onk_bool(onk_eval(cond, root, slots))? {
                onk_eval(a, root, slots)?
            } else {
                onk_eval(b, root, slots)?
            }
        }
        OnkOp::And(a, b) => OnkValue::Bool(onk_bool(onk_eval(a, root, slots))? && onk_bool(onk_eval(b, root, slots))?),
        OnkOp::Or(a, b) => OnkValue::Bool(onk_bool(onk_eval(a, root, slots))? || onk_bool(onk_eval(b, root, slots))?),
        OnkOp::Eq(a, b) => OnkValue::Bool(onk_equal(&onk_eval(a, root, slots)?, &onk_eval(b, root, slots)?)?),
        OnkOp::Ne(a, b) => OnkValue::Bool(!onk_equal(&onk_eval(a, root, slots)?, &onk_eval(b, root, slots)?)?),
        OnkOp::Lt(a, b) => OnkValue::Bool(onk_order(&onk_eval(a, root, slots)?, &onk_eval(b, root, slots)?)?.is_lt()),
        OnkOp::Le(a, b) => OnkValue::Bool(onk_order(&onk_eval(a, root, slots)?, &onk_eval(b, root, slots)?)?.is_le()),
        OnkOp::Gt(a, b) => OnkValue::Bool(onk_order(&onk_eval(a, root, slots)?, &onk_eval(b, root, slots)?)?.is_gt()),
        OnkOp::Ge(a, b) => OnkValue::Bool(onk_order(&onk_eval(a, root, slots)?, &onk_eval(b, root, slots)?)?.is_ge()),
        OnkOp::In(a, b) => {
            let needle = onk_eval(a, root, slots)?;
            match onk_eval(b, root, slots)? {
                OnkValue::List(items) => {
                    let mut found = false;
                    for item in items.iter() {
                        if onk_equal(&needle, item)? {
                            found = true;
                            break;
                        }
                    }
                    OnkValue::Bool(found)
                }
                OnkValue::Map(entries) => match needle {
                    OnkValue::Str(key) => OnkValue::Bool(entries.contains_key(key)),
                    _ => return Err(OnkFail),
                },
                _ => return Err(OnkFail),
            }
        }
        OnkOp::Add(a, b) => OnkValue::Int(onk_int(onk_eval(a, root, slots))?.checked_add(onk_int(onk_eval(b, root, slots))?).ok_or(OnkFail)?),
        OnkOp::Sub(a, b) => OnkValue::Int(onk_int(onk_eval(a, root, slots))?.checked_sub(onk_int(onk_eval(b, root, slots))?).ok_or(OnkFail)?),
        OnkOp::Mul(a, b) => OnkValue::Int(onk_int(onk_eval(a, root, slots))?.checked_mul(onk_int(onk_eval(b, root, slots))?).ok_or(OnkFail)?),
        OnkOp::Div(a, b) => {
            let (x, y) = (onk_int(onk_eval(a, root, slots))?, onk_int(onk_eval(b, root, slots))?);
            OnkValue::Int(x.checked_div(y).ok_or(OnkFail)?)
        }
        OnkOp::Mod(a, b) => {
            let (x, y) = (onk_int(onk_eval(a, root, slots))?, onk_int(onk_eval(b, root, slots))?);
            OnkValue::Int(x.checked_rem(y).ok_or(OnkFail)?)
        }
        OnkOp::FAdd(a, b) => onk_finite(onk_double(onk_eval(a, root, slots))? + onk_double(onk_eval(b, root, slots))?)?,
        OnkOp::FSub(a, b) => onk_finite(onk_double(onk_eval(a, root, slots))? - onk_double(onk_eval(b, root, slots))?)?,
        OnkOp::FMul(a, b) => onk_finite(onk_double(onk_eval(a, root, slots))? * onk_double(onk_eval(b, root, slots))?)?,
        OnkOp::FDiv(a, b) => {
            let (x, y) = (onk_double(onk_eval(a, root, slots))?, onk_double(onk_eval(b, root, slots))?);
            if y == 0.0 {
                return Err(OnkFail);
            }
            onk_finite(x / y)?
        }
    })
}
`
