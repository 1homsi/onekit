package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIMarksDeprecatedFieldsAndOperations(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model Note {
  #deprecated ""
  title: string;
}

interface Notes {
  #deprecated "use fetch"
  @get
  @route("/notes")
  get(...Note): Note;
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{"$ref: '#/components/schemas/onekit.ErrorMessage' deprecated: true", "title: type: string deprecated: true"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}

func TestOpenAPIMarksDeprecatedWebSocketOperations(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model Frame {
  text: string;
}

interface Chat {
  #deprecated ""
  @Onkt.ws
  @route("/chat")
  join(...Frame): Frame;
}
`)
	if !strings.Contains(strings.Join(strings.Fields(doc), " "), "deprecated: true") {
		t.Fatalf("websocket operation not deprecated:\n%s", doc)
	}
}
