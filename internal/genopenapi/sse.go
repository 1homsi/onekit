package genopenapi

import (
	"fmt"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"

	"github.com/1homsi/onekit/internal/onkir"
)

// sseResponse describes a streaming (@stream) method's success response as an
// SSE endpoint: a "text/event-stream" body plus an x-sse-event-schema vendor
// extension pointing at the schema of each individual event, since OpenAPI
// itself has no native representation for Server-Sent Events.
func sseResponse(m *onkir.Method) *v3.Response {
	content := orderedmap.New[string, *v3.MediaType]()
	content.Set("text/event-stream", &v3.MediaType{
		Schema: base.CreateSchemaProxy(&base.Schema{
			Type: []string{"string"},
			Description: fmt.Sprintf(
				"SSE stream. Each event contains a JSON-encoded %s in the data field.", m.Response.Name,
			),
		}),
	})

	ext := orderedmap.New[string, *yaml.Node]()
	ext.Set("x-sse-event-schema", schemaRefNode(m.Response.Name))

	return &v3.Response{
		Description: "Server-Sent Events stream",
		Content:     content,
		Extensions:  ext,
	}
}

func schemaRefNode(name string) *yaml.Node {
	return &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "$ref"},
			{Kind: yaml.ScalarNode, Value: "#/components/schemas/" + name},
		},
	}
}
