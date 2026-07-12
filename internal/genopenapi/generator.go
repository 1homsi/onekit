package genopenapi

import (
	"fmt"
	"strconv"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/1homsi/onekit/internal/onkir"
)

type Options struct {
	Title       string
	Version     string
	Description string
}

func scalarSchema(k onkir.ScalarKind) *base.Schema {
	switch k {
	case onkir.ScalarString:
		return &base.Schema{Type: []string{"string"}}
	case onkir.ScalarBool:
		return &base.Schema{Type: []string{"boolean"}}
	case onkir.ScalarInt32:
		return &base.Schema{Type: []string{"integer"}, Format: "int32"}
	case onkir.ScalarUint32:
		return &base.Schema{Type: []string{"integer"}, Format: "int32"}
	case onkir.ScalarInt64, onkir.ScalarUint64:
		return &base.Schema{Type: []string{"string"}, Format: "int64"}
	case onkir.ScalarFloat32:
		return &base.Schema{Type: []string{"number"}, Format: "float"}
	case onkir.ScalarFloat64:
		return &base.Schema{Type: []string{"number"}, Format: "double"}
	case onkir.ScalarBytes:
		return &base.Schema{Type: []string{"string"}, Format: "byte"}
	case onkir.ScalarTimestamp:
		return &base.Schema{Type: []string{"string"}, Format: "date-time"}
	default:
		return &base.Schema{}
	}
}

func typeSchemaProxy(t *onkir.Type) *base.SchemaProxy {
	switch t.Kind {
	case onkir.KindScalar:
		return base.CreateSchemaProxy(scalarSchema(t.Scalar))
	case onkir.KindMessage:
		return base.CreateSchemaProxyRef("#/components/schemas/" + t.Message.Name)
	case onkir.KindEnum:
		return base.CreateSchemaProxyRef("#/components/schemas/" + t.Enum.Name)
	case onkir.KindMap:
		s := &base.Schema{Type: []string{"object"}}
		s.AdditionalProperties = &base.DynamicValue[*base.SchemaProxy, bool]{A: typeSchemaProxy(t.MapValue)}
		return base.CreateSchemaProxy(s)
	default:
		return base.CreateSchemaProxy(&base.Schema{})
	}
}

func fieldSchemaProxy(f *onkir.Field) *base.SchemaProxy {
	if f.Oneof != nil {
		var variants []*base.SchemaProxy
		for _, v := range f.Oneof.Variants {
			variants = append(variants, typeSchemaProxy(v.Type))
		}
		return base.CreateSchemaProxy(&base.Schema{OneOf: variants})
	}
	if f.Repeated {
		item := typeSchemaProxy(f.Type)
		return base.CreateSchemaProxy(&base.Schema{
			Type:  []string{"array"},
			Items: &base.DynamicValue[*base.SchemaProxy, bool]{A: item},
		})
	}
	return typeSchemaProxy(f.Type)
}

func messageSchema(m *onkir.Message) *base.Schema {
	props := orderedmap.New[string, *base.SchemaProxy]()
	for _, f := range m.Fields {
		props.Set(f.Name, fieldSchemaProxy(f))
	}
	return &base.Schema{
		Type:       []string{"object"},
		Properties: props,
	}
}

func enumSchema(e *onkir.Enum) *base.Schema {
	var nodes []*yaml.Node
	for _, v := range e.Values {
		nodes = append(nodes, &yaml.Node{Kind: yaml.ScalarNode, Value: v.JSONName()})
	}
	return &base.Schema{
		Type: []string{"string"},
		Enum: nodes,
	}
}

func collectSchemas(schemas *orderedmap.Map[string, *base.SchemaProxy], m *onkir.Message) {
	schemas.Set(m.Name, base.CreateSchemaProxy(messageSchema(m)))
	for _, nested := range m.Nested {
		collectSchemas(schemas, nested)
	}
	for _, nested := range m.NestedEnums {
		schemas.Set(nested.Name, base.CreateSchemaProxy(enumSchema(nested)))
	}
}

func headerParameter(h *onkir.Header) *v3.Parameter {
	p := &v3.Parameter{
		Name:     h.Name,
		In:       "header",
		Required: new(h.Required()),
		Schema:   base.CreateSchemaProxy(scalarSchema(h.Type)),
	}
	if format, ok := h.Format(); ok {
		s := scalarSchema(h.Type)
		s.Format = format
		p.Schema = base.CreateSchemaProxy(s)
	}
	return p
}

func pathParameter(name string, req *onkir.Message) *v3.Parameter {
	kind := onkir.ScalarString
	for _, f := range req.Fields {
		if f.Name == name && f.Type != nil && f.Type.Kind == onkir.KindScalar {
			kind = f.Type.Scalar
		}
	}
	return &v3.Parameter{
		Name:     name,
		In:       "path",
		Required: new(true),
		Schema:   base.CreateSchemaProxy(scalarSchema(kind)),
	}
}

func queryParameters(req *onkir.Message) []*v3.Parameter {
	var params []*v3.Parameter
	for _, f := range req.Fields {
		d, ok := f.Decorator("query")
		if !ok || f.Type == nil || f.Type.Kind != onkir.KindScalar {
			continue
		}
		name, _ := d.Value()
		if name == "" {
			name = f.Name
		}
		params = append(params, &v3.Parameter{
			Name:     name,
			In:       "query",
			Required: new(f.HasDecorator("required")),
			Schema:   base.CreateSchemaProxy(scalarSchema(f.Type.Scalar)),
		})
	}
	return params
}

func pathParamNames(path string) []string {
	var names []string
	start := -1
	for i, c := range path {
		if c == '{' {
			start = i + 1
		} else if c == '}' && start >= 0 {
			names = append(names, path[start:i])
			start = -1
		}
	}
	return names
}

func isBodyBearingVerb(verb string) bool {
	return verb == "post" || verb == "put" || verb == "patch" || verb == "query"
}

func buildOperation(s *onkir.Service, m *onkir.Method) *v3.Operation {
	verb, _ := m.Verb()
	path, _ := m.Path()
	bodyBearing := isBodyBearingVerb(verb)

	op := &v3.Operation{
		OperationId: m.Name,
		Summary:     m.Name,
		Tags:        []string{s.Name},
	}
	if m.Doc != "" {
		op.Description = m.Doc
	}

	var params []*v3.Parameter
	for _, name := range pathParamNames(path) {
		params = append(params, pathParameter(name, m.Request))
	}
	for _, h := range s.Headers {
		params = append(params, headerParameter(h))
	}
	for _, h := range m.Headers {
		params = append(params, headerParameter(h))
	}
	if !bodyBearing {
		params = append(params, queryParameters(m.Request)...)
	}
	if len(params) > 0 {
		op.Parameters = params
	}

	if bodyBearing {
		content := orderedmap.New[string, *v3.MediaType]()
		content.Set("application/json", &v3.MediaType{
			Schema: base.CreateSchemaProxyRef("#/components/schemas/" + m.Request.Name),
		})
		op.RequestBody = &v3.RequestBody{Required: new(true), Content: content}
	}

	responses := &v3.Responses{Codes: orderedmap.New[string, *v3.Response]()}
	if m.IsStream() {
		responses.Codes.Set("200", sseResponse(m))
	} else {
		successContent := orderedmap.New[string, *v3.MediaType]()
		successContent.Set("application/json", &v3.MediaType{
			Schema: base.CreateSchemaProxyRef("#/components/schemas/" + m.Response.Name),
		})
		responses.Codes.Set("200", &v3.Response{Description: "OK", Content: successContent})
	}

	for _, errType := range m.ErrorTypes {
		status := 500
		if code, ok := errType.StatusCode(); ok {
			status = code
		}
		errContent := orderedmap.New[string, *v3.MediaType]()
		errContent.Set("application/json", &v3.MediaType{
			Schema: base.CreateSchemaProxyRef("#/components/schemas/" + errType.Name),
		})
		responses.Codes.Set(strconv.Itoa(status), &v3.Response{Description: errType.Name, Content: errContent})
	}
	op.Responses = responses

	return op
}

func assignOperation(item *v3.PathItem, verb string, op *v3.Operation) {
	switch verb {
	case "get":
		item.Get = op
	case "put":
		item.Put = op
	case "delete":
		item.Delete = op
	case "patch":
		item.Patch = op
	case "query":
		item.Query = op
	default:
		item.Post = op
	}
}

func Generate(file *onkir.File, opts Options) ([]byte, error) {
	if opts.Title == "" {
		opts.Title = "Generated API"
	}
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}

	schemas := orderedmap.New[string, *base.SchemaProxy]()
	for _, m := range file.Messages {
		collectSchemas(schemas, m)
	}
	for _, e := range file.Enums {
		schemas.Set(e.Name, base.CreateSchemaProxy(enumSchema(e)))
	}

	paths := orderedmap.New[string, *v3.PathItem]()
	for _, s := range file.Services {
		for _, m := range s.Methods {
			verb, _ := m.Verb()
			path, _ := m.Path()
			fullPath := s.BasePath + path
			item, ok := paths.Get(fullPath)
			if !ok {
				item = &v3.PathItem{}
				paths.Set(fullPath, item)
			}
			assignOperation(item, verb, buildOperation(s, m))
		}
	}

	doc := &v3.Document{
		Version: "3.1.0",
		Info: &base.Info{
			Title:       opts.Title,
			Version:     opts.Version,
			Description: opts.Description,
		},
		Paths:      &v3.Paths{PathItems: paths},
		Components: &v3.Components{Schemas: schemas},
	}

	yamlData, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal openapi document: %w", err)
	}
	return yamlData, nil
}

func GenerateJSON(file *onkir.File, opts Options) ([]byte, error) {
	yamlData, err := Generate(file, opts)
	if err != nil {
		return nil, err
	}
	jsonData, err := k8syaml.YAMLToJSON(yamlData)
	if err != nil {
		return nil, fmt.Errorf("convert to json: %w", err)
	}
	return jsonData, nil
}
