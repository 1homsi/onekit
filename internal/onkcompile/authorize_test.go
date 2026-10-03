package onkcompile

import (
	"errors"
	"strings"
	"testing"
)

const authorizeSchema = `package api

message Principal @principal {
  user_id: string
  roles: string[]
  org: string
}

message DeleteDoc {
  id: string
  owner_org: string
}

message Ack { ok: bool }

service Docs {
  delete(DeleteDoc) -> Ack @post("/docs/delete")
    @authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")
}
`

func TestAuthorizeAcceptsPrincipalAndRequestBindings(t *testing.T) {
	if err := compileRules(t, authorizeSchema); err != nil {
		t.Fatalf("valid authorize rule rejected: %v", err)
	}
}

func TestAuthorizeSetsThePrincipalOnTheMethod(t *testing.T) {
	pkg, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, authorizeSchema)}})
	if err != nil {
		t.Fatal(err)
	}
	method := pkg.Files[0].Services[0].Methods[0]
	if method.Principal == nil || method.Principal.Name != "Principal" {
		t.Fatalf("principal not attached: %+v", method.Principal)
	}
	if len(method.AuthorizeRules()) != 1 {
		t.Fatalf("authorize decorators: %d", len(method.AuthorizeRules()))
	}
}

func TestAuthorizeRepeatsAndCombinesWithRequires(t *testing.T) {
	err := compileRules(t, strings.Replace(authorizeSchema,
		`@authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")`,
		`@requires("docs:write") @authorize("size(auth.user_id) > 0", "sign in") @authorize("auth.org != ''", "join an organization")`, 1))
	if err != nil {
		t.Fatalf("repeated @authorize with @requires rejected: %v", err)
	}
}

func TestAuthorizeDiagnostics(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(string) string
		line   int
		msg    string
	}{
		{"unknown principal field", func(s string) string { return strings.Replace(s, "auth.org == req", "auth.team == req", 1) }, 21, `no field "team"`},
		{"unknown request field", func(s string) string { return strings.Replace(s, "req.owner_org", "req.nope", 1) }, 21, `no field "nope"`},
		{"not a bool", func(s string) string {
			return strings.Replace(s, `"'admin' in auth.roles || auth.org == req.owner_org"`, `"auth.org"`, 1)
		}, 21, "must evaluate to bool"},
		{"self is not bound", func(s string) string { return strings.Replace(s, "auth.org == req.owner_org", "self.id == ''", 1) }, 21, `unknown name "self"`},
		{"missing principal", func(s string) string { return strings.Replace(s, " @principal", "", 1) }, 21, "@principal"},
		{"two principals", func(s string) string {
			return strings.Replace(s, "message Ack {", "message Other @principal {\n  id: string\n}\n\nmessage Ack {", 1)
		}, 14, "only one message"},
		{"wrong arity", func(s string) string { return strings.Replace(s, `, "not allowed to delete this document"`, "", 1) }, 21, "expression and a message"},
		{"websocket", func(s string) string {
			return strings.Replace(s, `@post("/docs/delete")`, `@ws("/docs/delete")`, 1)
		}, 20, "@ws"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, tc.mutate(authorizeSchema))
			var compileErr *Error
			if !errors.As(err, &compileErr) {
				t.Fatalf("want a compile error, got %v", err)
			}
			if !strings.Contains(compileErr.Msg, tc.msg) {
				t.Errorf("message %q does not contain %q", compileErr.Msg, tc.msg)
			}
			if compileErr.Line < 14 || compileErr.Line > 22 {
				t.Errorf("line %d is not inside the schema's declarations (%v)", compileErr.Line, err)
			}
		})
	}
}
