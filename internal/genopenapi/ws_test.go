package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestOpenAPIDocumentsWebSocketMethods(t *testing.T) {
	ast, err := onktest.Parse(`namespace rt;

model Call {
  @Onkt.wsId
  id: string;
  method: string;
}

model Result {
  @Onkt.wsId
  id: string;
  @Onkt.raw
  output: string;
}

model Cancel {
  @Onkt.wsId
  id: string;
}

@Onkt.discriminator("type")
union FramePayload {
  @Onkt.tag("call") call: Call,
  @Onkt.tag("result") result: Result,
  @Onkt.tag("cancel") @Onkt.wsCancel cancel: Cancel,
}

model Frame {
  payload: FramePayload;
}

@route("/v1")
interface Runtime {
  /** Runs code interactively. */
  @Onkt.ws
  @route("/execute")
  execute(...Frame): Frame;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "rt.tsp", AST: ast}})
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
