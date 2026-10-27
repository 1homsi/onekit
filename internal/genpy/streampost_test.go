package genpy

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const streamPostSchema = `namespace app;

model TurnRequest {
  prompt: string;
}

model Text {
  text: string;
}

@Onkt.discriminator("type")
union TurnEventPayload {
  @Onkt.tag("text") text: Text,
}

model TurnEvent {
  payload: TurnEventPayload;
}

interface Agent {
  @post
  @route("/turn")
  @Onkt.stream
  turn(...TurnRequest): TurnEvent;
}
`

func TestPostStreamSendsBody(t *testing.T) {
	ast, err := onktest.Parse(streamPostSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	out := string(GenerateClient(file, "models"))
	for _, want := range []string{`"Content-Type", "application/json"`, `data=body`, `text/event-stream`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
