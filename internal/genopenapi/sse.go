package genopenapi

import (
	"fmt"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"

	"github.com/1homsi/onekit/internal/onkir"
)

func sseResponse(m *onkir.Method) *v3.Response {
	content := orderedmap.New[string, *v3.MediaType]()
	content.Set("text/event-stream", &v3.MediaType{
		Schema: base.CreateSchemaProxy(&base.Schema{
			Type: []string{"string"},
			Description: fmt.Sprintf(
				"SSE stream. Each event contains a JSON-encoded %s in the data field. A failure after the stream starts arrives as an \"error\" event whose data matches one of x-sse-error-schemas.", m.Response.Name,
			),
		}),
	})

	ext := orderedmap.New[string, *yaml.Node]()
	ext.Set("x-sse-event-schema", schemaRefNode(componentName(m.Response.FullName())))
	errorSchemas := &yaml.Node{Kind: yaml.SequenceNode}
	for _, errorType := range m.ErrorTypes {
		errorSchemas.Content = append(errorSchemas.Content, schemaRefNode(componentName(errorType.FullName())))
	}
	errorSchemas.Content = append(errorSchemas.Content, schemaRefNode(errorMessageComponent))
	ext.Set("x-sse-error-schemas", errorSchemas)

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
