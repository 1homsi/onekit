package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
)

func TestWildcardPathValidation(t *testing.T) {
	cases := []struct {
		name    string
		service string
		want    string
	}{
		{"last segment accepted", `
interface S {
  @get
  @route("/files/{+path}")
  read(...Req): Res;
}
`, ""},
		{"middle segment rejected", `
interface S {
  @get
  @route("/files/{+path}/x")
  read(...Req): Res;
}
`, "must be the last segment"},
		{"partial segment rejected", `
interface S {
  @get
  @route("/files/a{+path}")
  read(...Req): Res;
}
`, "whole segment"},
		{"non-string field rejected", `
interface S {
  @get
  @route("/files/{+n}")
  read(...NReq): Res;
}
`, "requires a string request field"},
		{"websocket rejected", `
interface S {
  @Onkt.ws
  @route("/files/{+path}")
  open(...Req): Res;
}
`, "wildcard"},
		{"empty route without base path rejected", `
interface S {
  @get
  read(...Empty): Res;
}
`, "base_path"},
		{"empty route with base path accepted", `
@route("/files")
interface S {
  @get
  read(...Empty): Res;
}
`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, `namespace api;

model Req {
  path: string;
}

model NReq {
  n: int32;
}

model Empty {}

model Res {
  ok: boolean;
}
`+tc.service+"\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestMaxBodyDecoratorValidation(t *testing.T) {
	cases := []struct {
		name       string
		decorators string
		want       string
	}{
		{"mebibytes accepted", `@post @route("/x") @Onkt.maxBody("64MiB")`, ""},
		{"byte count accepted", `@post @route("/x") @Onkt.maxBody("1048576")`, ""},
		{"bad size rejected", `@post @route("/x") @Onkt.maxBody("lots")`, "positive byte count"},
		{"zero rejected", `@post @route("/x") @Onkt.maxBody("0")`, "positive byte count"},
		{"get rejected", `@get @route("/x") @Onkt.maxBody("1MiB")`, "body-bearing"},
		{"ws rejected", `@Onkt.ws @route("/x") @Onkt.maxBody("1MiB")`, "@max_body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, "namespace api;\nmodel Req { a: string; }\nmodel Res { ok: boolean; }\ninterface S { "+tc.decorators+" run(...Req): Res; }\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func compileRulesPackage(t *testing.T, src string) (*onkir.Package, error) {
	t.Helper()
	return Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, src)}})
}

func TestBodylessScalarRequestFieldsBindToTheQueryString(t *testing.T) {
	pkg, err := compileRulesPackage(t, `namespace api;

model Req {
  id: string;
  filter: string;
  limit: int32;
}

model Res {
  ok: boolean;
}

interface S {
  @get
  @route("/x/{id}")
  read(...Req): Res;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]*onkir.Field{}
	for _, field := range pkg.Files[0].Messages[0].Fields {
		fields[field.Name] = field
	}
	if !fields["filter"].HasDecorator("query") || !fields["limit"].HasDecorator("query") {
		t.Fatal("scalar fields that are not path parameters must bind to the query string on a GET")
	}
	if fields["id"].HasDecorator("query") {
		t.Fatal("a path parameter must stay a path parameter")
	}
}

func TestBodylessRequestFieldsThatCannotBindAreRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"message field", `
model Inner {
  a: string;
}

model Req {
  inner: Inner;
}

model Res {
  ok: boolean;
}

interface S {
  @get
  @route("/x")
  read(...Req): Res;
}
`},
		{"repeated field", `
model Req {
  tags: string[];
}

model Res {
  ok: boolean;
}

interface S {
  @delete
  @route("/x")
  read(...Req): Res;
}
`},
		{"enum field", `
enum Kind {
  A,
  B,
}

model Req {
  kind: Kind;
}

model Res {
  ok: boolean;
}

interface S {
  @get
  @route("/x")
  read(...Req): Res;
}
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileRulesPackage(t, `namespace api;
`+tc.src+"\n")
			if err == nil || !strings.Contains(err.Error(), "never sent") || !strings.Contains(err.Error(), "@query") {
				t.Fatalf("error = %v, want a never-sent error that mentions @query", err)
			}
		})
	}
}

func TestResourceMessageSharedWithABodyVerbIsAccepted(t *testing.T) {
	_, err := compileRulesPackage(t, `namespace api;

model Note {
  id: string;
  text: string;
}

interface S {
  @get
  @route("/notes/{id}")
  get(...Note): Note;

  @put
  @route("/notes/{id}")
  update(...Note): Note;
}
`)
	if err != nil {
		t.Fatalf("a resource message used by a GET and a PUT is a common shape: %v", err)
	}
}

func TestGuardPlaceholdersMustBePathParameters(t *testing.T) {
	cases := []struct {
		name       string
		decorators string
		want       string
	}{
		{"path placeholder accepted", `@get @route("/x/{id}") @Onkt.guard("obj/read/:id", "audit")`, ""},
		{"unknown placeholder rejected", `@get @route("/x/{id}") @Onkt.guard("obj/read/:other")`, "not a path parameter"},
		{"empty guard rejected", `@get @route("/x/{id}") @Onkt.guard`, "at least one pattern"},
		{"websocket rejected", `@Onkt.ws @route("/x") @Onkt.guard("a")`, "@guard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, "namespace api;\nmodel Req { id: string; }\nmodel Res { ok: boolean; }\ninterface S { "+tc.decorators+" get(...Req): Res; }\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
