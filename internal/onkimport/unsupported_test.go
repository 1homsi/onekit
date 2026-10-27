package onkimport

import (
	"strings"
	"testing"
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
	requireFaithfulTypeSpec(t, result)
	for _, want := range []string{"  @get\n  @route(\"/events\")\n  @Onkt.stream\n  Watch(...WatchRequest): Event;\n", "  @get\n  @route(\"/items\")\n  @Onkt.stream\n  Items(...ItemsRequest): Event;\n"} {
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
