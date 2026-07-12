package genopenapi

import (
	"testing"

	"github.com/pb33f/libopenapi"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const sseFixtureSrc = `
package app

message StreamEventsRequest {
  channel: string @query("channel")
}

message Event {
  id: string
  payload: string
}

message StreamError @status(400) {
  reason: string
}

service SSEService {
  base_path: "/api/v1"

  streamEvents(StreamEventsRequest) -> Event | StreamError @get("/events") @stream
}
`

func TestGenerateSSEEndpointHasEventStreamResponse(t *testing.T) {
	ast, err := onklang.Parse(sseFixtureSrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]

	out, err := Generate(file, Options{Title: "SSE Test", Version: "1.0.0"})
	if err != nil {
		t.Fatalf("Generate error: %v\n%s", err, out)
	}

	doc, err := libopenapi.NewDocument(out)
	if err != nil {
		t.Fatalf("libopenapi.NewDocument failed to parse generated spec: %v\n%s", err, out)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("BuildV3Model error: %v\n%s", err, out)
	}

	path, ok := model.Model.Paths.PathItems.Get("/api/v1/events")
	if !ok {
		t.Fatalf("expected /api/v1/events path item\n%s", out)
	}
	if path.Get == nil {
		t.Fatalf("expected GET operation on /api/v1/events\n%s", out)
	}

	resp, ok := path.Get.Responses.Codes.Get("200")
	if !ok {
		t.Fatalf("expected 200 response on streamEvents\n%s", out)
	}

	mediaType, ok := resp.Content.Get("text/event-stream")
	if !ok {
		t.Fatalf("expected text/event-stream content on streamEvents 200 response, got %+v\n%s", resp.Content, out)
	}
	if mediaType.Schema.Schema().Type[0] != "string" {
		t.Fatalf("expected string schema for SSE body, got %+v", mediaType.Schema.Schema().Type)
	}

	if resp.Extensions == nil {
		t.Fatalf("expected x-sse-event-schema extension on streamEvents 200 response\n%s", out)
	}
	extNode, ok := resp.Extensions.Get("x-sse-event-schema")
	if !ok {
		t.Fatalf("expected x-sse-event-schema key in extensions, got %+v\n%s", resp.Extensions, out)
	}
	var extBody struct {
		Ref string `yaml:"$ref"`
	}
	if decodeErr := extNode.Decode(&extBody); decodeErr != nil {
		t.Fatalf("decode x-sse-event-schema: %v", decodeErr)
	}
	if extBody.Ref != "#/components/schemas/Event" {
		t.Fatalf("expected x-sse-event-schema to ref Event schema, got %q", extBody.Ref)
	}

	if _, has400 := path.Get.Responses.Codes.Get("400"); !has400 {
		t.Fatalf("expected 400 response for StreamError on streamEvents\n%s", out)
	}

	foundQueryParam := false
	for _, p := range path.Get.Parameters {
		if p.Name == "channel" && p.In == "query" {
			foundQueryParam = true
		}
	}
	if !foundQueryParam {
		t.Fatalf("expected query parameter %q on streamEvents, got %+v", "channel", path.Get.Parameters)
	}
}
