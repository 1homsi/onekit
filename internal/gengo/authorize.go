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
	p.P(`return func(o *ServerOptions) {`)
	p.P(`if resolve == nil {`)
	p.P(`o.Principal = nil`)
	p.P(`return`)
	p.P(`}`)
	p.P(`o.Principal = func(ctx context.Context, r *http.Request) (any, error) {`)
	p.P(`principal, err := resolve(ctx, r)`)
	p.P(`if err != nil {`)
	p.P(`return nil, err`)
	p.P(`}`)
	p.P(`return principal, nil`)
	p.P(`}`)
	p.P(`}`)
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
	p.P("if o.Principal == nil {")
	p.P(`o.Fail(w, r, &ServerError{Status: http.StatusInternalServerError, Code: "internal", Message: "authorization is not configured"})`)
	p.P("return")
	p.P("}")
	p.P("principalValue, principalErr := o.Principal(r.Context(), r)")
	p.P("if principalErr != nil {")
	p.P(`var statusErr interface{ HTTPStatusCode() int }`)
	p.P(`if errors.As(principalErr, &statusErr) { o.WriteHandlerError(w, r, principalErr) } else { o.Fail(w, r, &ServerError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "unauthorized", Cause: principalErr}) }`)
	p.P("return")
	p.P("}")
	p.P("principal, principalOK := principalValue.(*", p.principalType, ")")
	p.P("if !principalOK || principal == nil {")
	p.P(`o.Fail(w, r, &ServerError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "unauthorized"})`)
	p.P("return")
	p.P("}")
	p.P("r = r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal))")
}

func writeAuthorizeCall(p *Printer, m *onkir.Method) {
	if len(m.AuthorizeRules()) == 0 || m.Principal == nil {
		return
	}
	p.P("if failed := ", authorizeFuncName(m), "(principal, req); len(failed) > 0 {")
	p.P(`o.Fail(w, r, &ServerError{Status: http.StatusForbidden, Code: "forbidden", Message: failed[0], Violations: failed})`)
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
