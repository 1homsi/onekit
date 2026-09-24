package genrust

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func compileRustSchema(t *testing.T, schema string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

func TestRustServerErrorBodiesUseMessageKey(t *testing.T) {
	out := string(GenerateServer(compileRustSchema(t, `
package app
message R { id: string @len(1, 10) }
service S { get(R) -> R @get("/r/{id}") }
`)))
	if strings.Contains(out, `json!({ "error":`) || !strings.Contains(out, `json!({ "message": "internal server error" })`) {
		t.Fatalf("error bodies must use the message key like Go and TypeScript:\n%s", out)
	}
}

<<<<<<< HEAD
func TestRustClientFallsBackWhenTypedErrorBodyDoesNotDecode(t *testing.T) {
	out := string(GenerateClient(compileRustSchema(t, `
package app
message R { id: string }
message NotFound @status(404) { resource: string }
service S { get(R) -> R | NotFound @get("/r/{id}") }
`)))
	if strings.Contains(out, "map_err(SGetError::Decode)?;\nreturn Err(SGetError::NotFound") || !strings.Contains(out, "if let Ok(error) = serde_json::from_slice::<NotFound>(&body) {") {
		t.Fatalf("typed error decode must fall through to UnexpectedStatus:\n%s", out)
=======
func TestRustStreamErrorsDoNotLeakInternalMessages(t *testing.T) {
	out := string(GenerateServer(compileRustSchema(t, `
package app
message R { id: string }
message Gone @status(410) { reason: string }
service S { watch(R) -> R | Gone @get("/w/{id}") @stream }
`)))
	for _, want := range []string{
		`Err(error) => Event::default().event("error").json_data(error.error_body()).unwrap_or_default(),`,
		`Self::Gone(error) => serde_json::to_value(error).unwrap_or_default(),`,
		`Self::Internal(_error) => serde_json::json!({ "message": "internal server error" }),`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
>>>>>>> dff343b (fix(genrust): send typed JSON stream errors without leaking internals)
	}
}
