package gents

import (
	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

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

func authorizeFuncName(m *onkir.Method) string {
	return "authorize" + PascalCase(m.Service.Name) + PascalCase(m.Name)
}

func authorizeExternalRefs(file *onkir.File, resolver PackageResolver) []PackageRef {
	if resolver == nil {
		return nil
	}
	c := newRefCollector(resolver)
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if authorizedMethod(m) {
				c.addMessage(m.Principal)
			}
		}
	}
	return c.sorted()
}

func writeTSAuthorizeFuncs(p *Printer, file *onkir.File) {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if !authorizedMethod(m) {
				continue
			}
			rules, err := onkexpr.AuthRulesFor(m)
			if err != nil {
				p.P("export function ", authorizeFuncName(m), "(_auth: unknown, _req: unknown): string[] {")
				p.P("throw new Error(", jsString("invalid @authorize: "+err.Error()), ");")
				p.P("}")
				p.P()
				continue
			}
			p.P("export function ", authorizeFuncName(m), "(auth: ", p.MessageTypeName(m.Principal), " | undefined, req: ", p.MessageTypeName(m.Request), "): string[] {")
			p.P("const violations: string[] = [];")
			for _, r := range rules {
				c := &tsRuleCompiler{state: &p.rules, bindings: map[string]string{onkexpr.AuthBinding: "auth", onkexpr.RequestBinding: "req"}}
				code := c.expr(r.Expr)
				p.rules.used = true
				p.P("if (!Onk.ruleHolds(() => ", code, ")) violations.push(", jsString(r.Message), ");")
			}
			p.P("return violations;")
			p.P("}")
			p.P()
		}
	}
}

func writeRoutePrincipalFlag(p *Printer, m *onkir.Method) {
	if authorizedMethod(m) {
		p.P("authorize: true,")
	}
}

func writeRouteAuthorizeCall(p *Printer, m *onkir.Method) {
	if !authorizedMethod(m) {
		return
	}
	p.P("const principal = principalOf(req) as ", p.MessageTypeName(m.Principal), " | undefined;")
	p.P("const denied = ", authorizeFuncName(m), "(principal, decoded);")
	p.P(`if (denied.length > 0) throw new HttpError(403, { message: denied[0], violations: denied });`)
}

func routeContextLiteral(m *onkir.Method) string {
	if authorizedMethod(m) {
		return "{ request: req, headers: req.headers, principal }"
	}
	return "{ request: req, headers: req.headers }"
}

func authorizeImportNames(file *onkir.File) []string {
	var names []string
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if authorizedMethod(m) {
				names = append(names, authorizeFuncName(m))
			}
		}
	}
	return names
}
