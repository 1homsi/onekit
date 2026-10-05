package genrust

import (
	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

type rustAuthEntry struct {
	method *onkir.Method
	index  int
	rules  []onkexpr.AuthRule
}

func authorizedMethod(m *onkir.Method) bool {
	return !m.IsWebSocket() && len(m.AuthorizeRules()) > 0 && m.Principal != nil
}

func filePrincipal(file *onkir.File) *onkir.Message {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if authorizedMethod(m) {
				return m.Principal
			}
		}
	}
	return nil
}

func authorizeFnName(m *onkir.Method) string {
	return "authorize_" + SnakeCase(m.Service.Name) + "_" + SnakeCase(m.Name)
}

func (p *Printer) prepareAuthorize(file *onkir.File, state *rustRuleState) {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if !authorizedMethod(m) {
				continue
			}
			rules, err := onkexpr.AuthRulesFor(m)
			if err != nil {
				continue
			}
			state.reach(m.Principal)
			state.reach(m.Request)
			state.auth = append(state.auth, rustAuthEntry{method: m, index: len(state.auth), rules: rules})
		}
	}
}

func (p *Printer) writeAuthorizeTables() {
	state := p.rules
	for _, entry := range state.auth {
		p.P("fn onk_auth_rules_", entry.index, "() -> &'static [OnkRule] {")
		p.Indent()
		p.P("static RULES: std::sync::OnceLock<Vec<OnkRule>> = std::sync::OnceLock::new();")
		p.P("RULES.get_or_init(|| vec![")
		p.Indent()
		bound := map[string]bool{onkexpr.AuthBinding: true, onkexpr.RequestBinding: true}
		for _, r := range entry.rules {
			op := onkexpr.LowerBound(r.Expr, &state.regexes, bound)
			p.P("OnkRule { field: \"\", message: ", rustStringLit(r.Message), ", op: ", rustOp(op), " },")
		}
		p.Dedent()
		p.P("])")
		p.Dedent()
		p.P("}")
		p.Blank()
		principal := p.MessageTypeName(entry.method.Principal)
		request := p.MessageTypeName(entry.method.Request)
		p.P("pub fn ", authorizeFnName(entry.method), "(auth: Option<&", principal, ">, req: &", request, ") -> Vec<&'static str> {")
		p.Indent()
		p.P("let mut f: std::collections::HashMap<&'static str, OnkValue<'_>> = std::collections::HashMap::new();")
		p.P("if let Some(auth) = auth { f.insert(\"auth\", onk_value_", state.index[entry.method.Principal], "(auth)); }")
		p.P("f.insert(\"req\", onk_value_", state.index[entry.method.Request], "(req));")
		p.P("let root = OnkValue::Msg(std::rc::Rc::new(f));")
		p.P("onk_auth_rules_", entry.index, "().iter().filter(|rule| !onk_holds(&rule.op, &root)).map(|rule| rule.message).collect()")
		p.Dedent()
		p.P("}")
		p.Blank()
	}
}

func (p *Printer) writeRoutePrincipalLookup(m *onkir.Method) {
	if !authorizedMethod(m) {
		return
	}
	p.P("let Some(principal) = parts.extensions.get::<", p.principalType, ">().cloned() else {")
	p.Indent()
	p.P(`return error_response(StatusCode::UNAUTHORIZED, "unauthorized", "unauthorized".to_string(), Vec::new());`)
	p.Dedent()
	p.P("};")
}

func (p *Printer) writeRouteAuthorizeCall(m *onkir.Method) {
	if !authorizedMethod(m) {
		return
	}
	p.P("let denied = ", authorizeFnName(m), "(Some(&principal), &req);")
	p.P("if !denied.is_empty() {")
	p.Indent()
	p.P(`return error_response(StatusCode::FORBIDDEN, "forbidden", denied[0].to_string(), denied.iter().map(|message| message.to_string()).collect());`)
	p.Dedent()
	p.P("}")
}

func (p *Printer) contextPrincipalField(m *onkir.Method) string {
	if p.principalType == "" {
		return ""
	}
	if authorizedMethod(m) {
		return ", principal: Some(principal)"
	}
	return ", principal: None"
}

func (p *Printer) wsContextPrincipalField() string {
	if p.principalType == "" {
		return ""
	}
	return ", principal: None"
}
