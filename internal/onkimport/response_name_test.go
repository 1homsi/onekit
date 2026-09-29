package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

// An operation whose success response has no JSON body (or a scalar/array/map
// body) synthesizes a `<Op>Response` message. The name was reserved by
// uniqueName before registerMessage ran, so registerMessage disambiguated to
// `<Op>Response_2` and queued *that* name while the fields were written under
// the reserved name. The declared response type was therefore never emitted and
// the generated schema failed to compile.
const scalarBodySpec = `
openapi: 3.0.3
info:
  title: Ping
  version: 1.0.0
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: { type: string }
`

const noBodySpec = `
openapi: 3.0.3
info:
  title: Ping
  version: 1.0.0
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "204":
          description: ok
`

const arrayBodySpec = `
openapi: 3.0.3
info:
  title: Ping
  version: 1.0.0
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: { type: array, items: { type: string } }
`

// compileImported fails if the imported source does not parse and compile.
func compileImported(t *testing.T, spec string) {
	t.Helper()
	result, err := Import([]byte(spec), Options{Package: "demo", Service: "DemoService"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	ast, err := onklang.Parse(string(result.Source))
	if err != nil {
		t.Fatalf("imported source does not parse: %v\n%s", err, result.Source)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: result.Package + ".onk", AST: ast}}); err != nil {
		t.Fatalf("imported .onk does not compile: %v\n%s", err, result.Source)
	}
}

func TestImportDeclaresSynthesizedScalarResponse(t *testing.T) {
	compileImported(t, scalarBodySpec)
}

func TestImportDeclaresSynthesizedNoBodyResponse(t *testing.T) {
	compileImported(t, noBodySpec)
}

func TestImportDeclaresSynthesizedArrayResponse(t *testing.T) {
	compileImported(t, arrayBodySpec)
}

// The synthesized message must be declared under the name the RPC actually
// references, with no stray "_2" declaration left behind.
func TestImportDoesNotEmitOrphanDisambiguatedResponse(t *testing.T) {
	result, err := Import([]byte(scalarBodySpec), Options{Package: "demo", Service: "DemoService"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	src := string(result.Source)
	if !strings.Contains(src, "message PingResponse {") {
		t.Errorf("expected a `message PingResponse` declaration, got:\n%s", src)
	}
	if strings.Contains(src, "PingResponse_2") {
		t.Errorf("expected no orphaned PingResponse_2 declaration, got:\n%s", src)
	}
}
