package genrust

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onktest"
)

func compileRustSchema(t *testing.T, schema string) *onkir.File {
	t.Helper()
	ast, err := onktest.Parse(schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

func TestRustServerErrorBodiesUseMessageKey(t *testing.T) {
	out := string(GenerateServer(compileRustSchema(t, `namespace app;

model R {
  @minLength(1)
  @maxLength(10)
  id: string;
}

interface S {
  @get
  @route("/r/{id}")
  get(...R): R;
}
`)))
	if strings.Contains(out, `json!({ "error":`) || !strings.Contains(out, `json!({ "message": "internal server error" })`) {
		t.Fatalf("error bodies must use the message key like Go and TypeScript:\n%s", out)
	}
}

func TestRustClientFallsBackWhenTypedErrorBodyDoesNotDecode(t *testing.T) {
	out := string(GenerateClient(compileRustSchema(t, `namespace app;

model R {
  id: string;
}

@error
model NotFound {
  @statusCode _: 404;
  resource: string;
}

interface S {
  @get
  @route("/r/{id}")
  get(...R): R | NotFound;
}
`)))
	if strings.Contains(out, "map_err(SGetError::Decode)?;\nreturn Err(SGetError::NotFound") || !strings.Contains(out, "if let Ok(error) = serde_json::from_slice::<NotFound>(&body) {") {
		t.Fatalf("typed error decode must fall through to UnexpectedStatus:\n%s", out)
	}
}

func TestRustStreamErrorsDoNotLeakInternalMessages(t *testing.T) {
	out := string(GenerateServer(compileRustSchema(t, `namespace app;

model R {
  id: string;
}

@error
model Gone {
  @statusCode _: 410;
  reason: string;
}

interface S {
  @get
  @route("/w/{id}")
  @Onkt.stream
  watch(...R): R | Gone;
}
`)))
	for _, want := range []string{
		`Err(error) => stream_error_event(error.into_response(), error_writer.as_ref(), &request_headers).await,`,
		`Self::Gone(error) => serde_json::to_value(error).unwrap_or_default(),`,
		`Self::Internal(_error) => serde_json::json!({ "message": "internal server error" }),`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}
