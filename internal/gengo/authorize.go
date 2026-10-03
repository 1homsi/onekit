package gengo

import (
	"strconv"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
)

func filePrincipal(file *onkir.File) *onkir.Message {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if m.Principal != nil && len(m.AuthorizeRules()) > 0 && !m.IsWebSocket() {
				return m.Principal
			}
		}
	}
	return nil
}

func authorizeFuncName(m *onkir.Method) string {
	return "authorize" + PascalCase(m.Service.Name) + PascalCase(m.Name)
}

func writePrincipalOption(p *Printer) {
	if p.principalType == "" {
		return
	}
	p.P()
	p.P(`// WithPrincipal tells the server who the caller is, for the methods that declare @authorize.`)
	p.P(`// Return an error to reject the request (an error with HTTPStatusCode() controls the status;`)
	p.P(`// any other error is a 401). Handlers can read the same value with PrincipalFromContext.`)
	p.P(`func WithPrincipal(resolve func(context.Context, *http.Request) (*`, p.principalType, `, error)) ServerOption {`)
	p.P(`return func(o *serverOptions) { o.principal = resolve }`)
	p.P(`}`)
	p.P()
	p.P(`type principalContextKey struct{}`)
	p.P()
	p.P(`func PrincipalFromContext(ctx context.Context) (*`, p.principalType, `, bool) {`)
	p.P(`principal, ok := ctx.Value(principalContextKey{}).(*`, p.principalType, `)`)
	p.P(`return principal, ok && principal != nil`)
	p.P(`}`)
	p.P()
}

func writePrincipalLookup(p *Printer, m *onkir.Method) {
	if len(m.AuthorizeRules()) == 0 || m.Principal == nil {
		return
	}
	p.P("if o.principal == nil {")
	p.P(`writeJSONError(w, http.StatusInternalServerError, "authorization is not configured")`)
	p.P("return")
	p.P("}")
	p.P("principal, principalErr := o.principal(r.Context(), r)")
	p.P("if principalErr != nil {")
	p.P(`var statusErr interface{ HTTPStatusCode() int }`)
	p.P(`if errors.As(principalErr, &statusErr) { writeHandlerError(w, principalErr) } else { writeJSONError(w, http.StatusUnauthorized, "unauthorized") }`)
	p.P("return")
	p.P("}")
	p.P("r = r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal))")
}

func writeAuthorizeCall(p *Printer, m *onkir.Method) {
	if len(m.AuthorizeRules()) == 0 || m.Principal == nil {
		return
	}
	p.P("if failed := ", authorizeFuncName(m), "(principal, req); len(failed) > 0 {")
	p.P(`writeJSON(w, http.StatusForbidden, map[string]any{"message": failed[0], "violations": failed})`)
	p.P("return")
	p.P("}")
}

func writeAuthorizeFuncs(p *Printer, rules *ruleFile, file *onkir.File) error {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if m.IsWebSocket() || len(m.AuthorizeRules()) == 0 || m.Principal == nil {
				continue
			}
			parsed, err := onkexpr.AuthRulesFor(m)
			if err != nil {
				return err
			}
			p.P("func ", authorizeFuncName(m), "(auth *", p.MessageTypeName(m.Principal), ", req *", p.MessageTypeName(m.Request), ") []string {")
			p.P("var violations []string")
			for _, r := range parsed {
				c := &ruleCompiler{file: rules, bindings: map[string]string{onkexpr.AuthBinding: "auth", onkexpr.RequestBinding: "req"}}
				code := c.expr(r.Expr)
				rules.used = true
				p.P("if !onkRuleHolds(func() bool { return ", code, " }) { violations = append(violations, ", strconv.Quote(r.Message), ") }")
			}
			p.P("return violations")
			p.P("}")
			p.P()
		}
	}
	return nil
}

func fileUsesAuthorize(file *onkir.File) bool {
	return filePrincipal(file) != nil
}

func authorizeExternalRefs(file *onkir.File, resolver PackageResolver) []PackageRef {
	if resolver == nil {
		return nil
	}
	c := newRefCollector(resolver)
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if m.IsWebSocket() || len(m.AuthorizeRules()) == 0 || m.Principal == nil {
				continue
			}
			c.addMessage(m.Principal)
			c.addMessage(m.Request)
		}
	}
	return c.sorted()
}
