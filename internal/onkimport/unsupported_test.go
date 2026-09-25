package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportMapsEventStreamsAndWarnsOnDroppedConstructs(t *testing.T) {
	spec := `{"openapi":"3.1.0","info":{"title":"Feed","version":"1"},
"webhooks":{"newPet":{"post":{"responses":{"200":{"description":"ok"}}}}},
"components":{"schemas":{"Event":{"type":"object","properties":{"id":{"type":"string"}}}}},
"paths":{
  "/events":{
    "head":{"responses":{"200":{"description":"ok"}}},
    "get":{"operationId":"watch","responses":{"200":{"description":"ok","content":{"text/event-stream":{"schema":{"$ref":"#/components/schemas/Event"}}}}}}
  },
  "/items":{
    "get":{"operationId":"items","callbacks":{"done":{}},
      "responses":{"200":{"description":"ok","content":{"text/event-stream":{"itemSchema":{"type":"object","properties":{"data":{"type":"string","contentMediaType":"application/json","contentSchema":{"$ref":"#/components/schemas/Event"}}}}}}}}}
  }
}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "feed.onk", AST: ast}}); err != nil {
		t.Fatalf("import does not compile: %v\n%s", err, src)
	}
	for _, want := range []string{`Watch(WatchRequest) -> Event @get("/events") @stream`, `Items(ItemsRequest) -> Event @get("/items") @stream`} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	warnings := strings.Join(result.Warnings, "\n")
	for _, want := range []string{"webhooks", "HEAD /events", "callbacks"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("missing warning %q in:\n%s", want, warnings)
		}
	}
}
