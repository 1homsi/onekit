package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestOpenAPIDocumentsWebSocketMethods(t *testing.T) {
	ast, err := onklang.Parse(`
package rt
message Call { id: string @ws_id
method: string }
message Result { id: string @ws_id
output: string @raw }
message Cancel { id: string @ws_id }
message Frame { payload: oneof(discriminator: "type") { call: Call @tag("call")
result: Result @tag("result")
cancel: Cancel @tag("cancel") @ws_cancel } }
service Runtime {
  base_path: "/v1"
  /// Runs code interactively.
  execute(Frame) -> Frame @ws("/execute")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "rt.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Generate(pkg.Files[0], Options{Title: "rt", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{"/v1/execute:", "operationId: Runtime_execute", "\"101\":", "x-onekit-websocket:", "clientFrame:", "correlationType: string", "clientCancelVariant: cancel", "- Result.output", "Runs code interactively."} {
		if !strings.Contains(text, want) {
			t.Fatalf("OpenAPI output missing %q:\n%s", want, text)
		}
	}
}
