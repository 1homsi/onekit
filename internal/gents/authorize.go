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
			// The rules are compiled before the signature is written, so a
			// parameter no rule reads can be named for noUnusedParameters.
			c := &tsRuleCompiler{state: &p.rules, bindings: map[string]string{onkexpr.AuthBinding: "auth", onkexpr.RequestBinding: "req"}}
			codes := make([]string, len(rules))
			for i, r := range rules {
				codes[i] = c.expr(r.Expr)
				p.rules.used = true
			}
			p.P("export function ", authorizeFuncName(m), "(", authorizeParam(c, onkexpr.AuthBinding), ": ", p.MessageTypeName(m.Principal), " | undefined, ", authorizeParam(c, onkexpr.RequestBinding), ": ", p.MessageTypeName(m.Request), "): string[] {")
			p.P("const violations: string[] = [];")
			for i, r := range rules {
				p.P("if (!Onk.ruleHolds(() => ", codes[i], ")) violations.push(", jsString(r.Message), ");")
			}
			p.P("return violations;")
			p.P("}")
			p.P()
		}
	}
}

// authorizeParam names the parameter of an authorize function that holds
// binding, with a leading underscore when no rule reads it.
func authorizeParam(c *tsRuleCompiler, binding string) string {
	if c.read[binding] {
		return c.bindings[binding]
	}
	return "_" + c.bindings[binding]
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
	p.P(`if (denied.length > 0) throw requestError(403, "forbidden", denied[0] ?? "forbidden", { violations: denied });`)
}

func routeContextLiteral(m *onkir.Method) string {
	fields := "request: req, headers: req.headers"
	if len(m.Meta()) > 0 {
		fields += ", meta: " + routeMetaName(m)
	}
	if authorizedMethod(m) {
		// Leave the key out rather than set it to undefined: RequestContext
		// declares principal?: P, which exactOptionalPropertyTypes reads as
		// "absent or a P".
		fields += ", ...(principal !== undefined ? { principal } : {})"
	}
	return "{ " + fields + " }"
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
