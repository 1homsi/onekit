package genopenapi

import (
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"

	"github.com/1homsi/onekit/internal/onkir"
)

func buildWSOperation(s *onkir.Service, m *onkir.Method) *v3.Operation {
	path, _ := m.WebSocketPath()
	op := &v3.Operation{
		OperationId: s.Name + "_" + m.Name,
		Summary:     m.Name,
		Description: m.Doc,
		Tags:        []string{s.Name},
	}
	if _, deprecated := m.Deprecated(); deprecated {
		op.Deprecated = new(true)
	}
	var params []*v3.Parameter
	for _, name := range onkir.PathParamNames(path) {
		params = append(params, pathParameter(name, m.Request))
	}
	for _, h := range append(append([]*onkir.Header{}, s.Headers...), m.Headers...) {
		if _, auth := h.AuthType(); !auth {
			params = append(params, headerParameter(h))
		}
	}
	params = append(params, queryParameters(m.Request)...)
	if len(params) > 0 {
		op.Parameters = params
	}
	responses := &v3.Responses{Codes: orderedmap.New[string, *v3.Response]()}
	responses.Codes.Set("101", &v3.Response{Description: "Switching Protocols: a WebSocket carrying JSON frames (see x-onekit-websocket)"})
	op.Responses = responses
	op.Extensions = orderedmap.New[string, *yaml.Node]()
	op.Extensions.Set("x-onekit-websocket", wsExtensionNode(m))
	return op
}

func wsExtensionNode(m *onkir.Method) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, value *yaml.Node) {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
	}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }
	add("clientFrame", schemaRefNode(componentName(m.Request.FullName())))
	add("serverFrame", schemaRefNode(componentName(m.Response.FullName())))
	if f, ok := m.WSIDField(); ok {
		add("correlationType", str(f.Type.Scalar.String()))
	}
	for _, frame := range []struct {
		key     string
		message *onkir.Message
	}{{"clientCancelVariant", m.Request}, {"serverCancelVariant", m.Response}} {
		if _, v, _, ok := onkir.WSCancelVariant(frame.message); ok {
			add(frame.key, str(v.Tag()))
		}
	}
	raw := &yaml.Node{Kind: yaml.SequenceNode}
	seen := map[string]bool{}
	visited := map[*onkir.Message]bool{}
	var walk func(*onkir.Message)
	walk = func(msg *onkir.Message) {
		if visited[msg] {
			return
		}
		visited[msg] = true
		for _, step := range onkir.RawSteps(msg, nil) {
			if step.Child != nil {
				walk(step.Child)
				continue
			}
			name := msg.Name + "." + step.Field.Name
			if !seen[name] {
				seen[name] = true
				raw.Content = append(raw.Content, str(name))
			}
		}
	}
	walk(m.Request)
	walk(m.Response)
	if len(raw.Content) > 0 {
		add("rawFields", raw)
	}
	return node
}
