package onkcompile

import (
	"errors"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onktest"
)

const authorizeSchema = `namespace api;

@Onkt.principal
model Principal {
  user_id: string;
  roles: string[];
  org: string;
}

model DeleteDoc {
  id: string;
  owner_org: string;
}

model Ack {
  ok: boolean;
}

interface Docs {
  @post
  @route("/docs/delete")
  @Onkt.authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")
  delete(...DeleteDoc): Ack;
}
`

func TestAuthorizeAcceptsPrincipalAndRequestBindings(t *testing.T) {
	if err := compileRules(t, authorizeSchema); err != nil {
		t.Fatalf("valid authorize rule rejected: %v", err)
	}
}

func TestAuthorizeSetsThePrincipalOnTheMethod(t *testing.T) {
	pkg, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, authorizeSchema)}})
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
		`@Onkt.authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")`,
		`@Onkt.requires("docs:write") @Onkt.authorize("size(auth.user_id) > 0", "sign in") @Onkt.authorize("auth.org != ''", "join an organization")`, 1))
	if err != nil {
		t.Fatalf("repeated @authorize with @requires rejected: %v", err)
	}
}

func TestAuthorizeDiagnostics(t *testing.T) {
	// Lines are those of authorizeSchema after the edit: @Onkt.authorize is
	// on line 22, the method on line 23, and the inserted second principal's
	// decorator on line 15.
	cases := []struct {
		name   string
		mutate func(string) string
		line   int
		msg    string
	}{
		{"unknown principal field", func(s string) string { return strings.Replace(s, "auth.org == req", "auth.team == req", 1) }, 22, `no field "team"`},
		{"unknown request field", func(s string) string { return strings.Replace(s, "req.owner_org", "req.nope", 1) }, 22, `no field "nope"`},
		{"not a bool", func(s string) string {
			return strings.Replace(s, `"'admin' in auth.roles || auth.org == req.owner_org"`, `"auth.org"`, 1)
		}, 22, "must evaluate to bool"},
		{"self is not bound", func(s string) string { return strings.Replace(s, "auth.org == req.owner_org", "self.id == ''", 1) }, 22, `unknown name "self"`},
		{"missing principal", func(s string) string { return strings.Replace(s, "@Onkt.principal\n", "", 1) }, 21, "@principal"},
		{"two principals", func(s string) string {
			return strings.Replace(s, "model Ack {", "@Onkt.principal\nmodel Other {\n  id: string;\n}\n\nmodel Ack {", 1)
		}, 15, "only one message"},
		{"websocket", func(s string) string {
			return strings.Replace(s, "@post\n  @route(\"/docs/delete\")", "@Onkt.ws\n  @route(\"/docs/delete\")", 1)
		}, 23, "@ws"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.mutate(authorizeSchema)
			if src == authorizeSchema {
				t.Fatal("the edit did not apply")
			}
			err := compileRules(t, src)
			var compileErr *Error
			if !errors.As(err, &compileErr) {
				t.Fatalf("want a compile error, got %v", err)
			}
			if !strings.Contains(compileErr.Msg, tc.msg) {
				t.Errorf("message %q does not contain %q", compileErr.Msg, tc.msg)
			}
			if compileErr.Line != tc.line {
				t.Errorf("line %d, want %d (%v)", compileErr.Line, tc.line, err)
			}
		})
	}
}

// TypeSpec's decorator signature already rejects @Onkt.authorize with one
// argument; the compiler still rejects such a document from any frontend.
func TestAuthorizeNeedsAnExpressionAndAMessage(t *testing.T) {
	q, dec := onktest.Q, onktest.Dec
	file := onktest.File("api",
		onktest.Message("Principal", onktest.Field("org", "string"), dec("principal")),
		onktest.Message("DeleteDoc", onktest.Field("id", "string")),
		onktest.Message("Ack"),
		onktest.Service("Docs", "", onktest.RPC("delete", "DeleteDoc", "Ack", dec("post", q("/docs/delete")), dec("authorize", q("auth.org != ''")))),
	)
	_, err := Compile([]Source{{Path: "api.tsp", AST: file}})
	if err == nil || !strings.Contains(err.Error(), "expression and a message") {
		t.Fatalf("err = %v", err)
	}
}
