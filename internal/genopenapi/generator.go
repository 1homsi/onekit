package genopenapi

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

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
		return base.CreateSchemaProxyRef("#/components/schemas/" + componentName(t.Message.FullName()))
	case onkir.KindEnum:
		return base.CreateSchemaProxyRef("#/components/schemas/" + componentName(t.Enum.FullName()))
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
		schema := &base.Schema{OneOf: variants}
		if discriminator, ok := f.Oneof.Discriminator(); ok && discriminator != "" {
			mapping := orderedmap.New[string, string]()
			for _, variant := range f.Oneof.Variants {
				if variant.Type.Kind == onkir.KindMessage {
					mapping.Set(variant.Tag(), "#/components/schemas/"+componentName(variant.Type.Message.FullName()))
				}
			}
			schema.Discriminator = &base.Discriminator{PropertyName: discriminator, Mapping: mapping}
		}
		return base.CreateSchemaProxy(schema)
	}
	if f.Repeated {
		item := typeSchemaProxy(f.Type)
		schema := &base.Schema{
			Type:  []string{"array"},
			Items: &base.DynamicValue[*base.SchemaProxy, bool]{A: item},
		}
		applyFieldValidation(schema, f)
		return base.CreateSchemaProxy(schema)
	}
	schema := concreteTypeSchema(f)
	applyFieldValidation(schema, f)
	return base.CreateSchemaProxy(schema)
}

func messageSchema(m *onkir.Message) *base.Schema {
	if len(m.Fields) == 1 && m.Fields[0].HasDecorator("unwrap") {
		proxy := fieldSchemaProxy(m.Fields[0])
		if schema := proxy.Schema(); schema != nil {
			return schema
		}
	}
	props := orderedmap.New[string, *base.SchemaProxy]()
	var required []string
	for _, f := range m.Fields {
		if prefix, ok := flattenPrefix(f); ok && f.Type != nil && f.Type.Kind == onkir.KindMessage {
			child := messageSchema(f.Type.Message)
			if child.Properties != nil {
				for name, schema := range child.Properties.FromOldest() {
					props.Set(prefix+name, schema)
				}
			}
			for _, name := range child.Required {
				required = append(required, prefix+name)
			}
			continue
		}
		props.Set(f.Name, fieldSchemaProxy(f))
		if f.HasDecorator("required") {
			required = append(required, f.Name)
		}
	}
	return &base.Schema{
		Type:        []string{"object"},
		Properties:  props,
		Required:    required,
		Description: m.Doc,
	}
}

func concreteTypeSchema(field *onkir.Field) *base.Schema {
	if field.Type == nil {
		return &base.Schema{}
	}
	encode, hasEncode := field.Decorator("encode")
	encodeValue, _ := encode.Value()
	switch field.Type.Kind {
	case onkir.KindScalar:
		if hasEncode {
			switch field.Type.Scalar {
			case onkir.ScalarInt64, onkir.ScalarUint64:
				if encodeValue == "number" {
					return &base.Schema{Type: []string{"integer"}, Format: "int64"}
				}
			case onkir.ScalarTimestamp:
				if encodeValue == "unix_seconds" || encodeValue == "unix_millis" {
					return &base.Schema{Type: []string{"integer"}, Format: "int64"}
				}
				if encodeValue == "date" {
					return &base.Schema{Type: []string{"string"}, Format: "date"}
				}
			case onkir.ScalarBytes:
				schema := &base.Schema{Type: []string{"string"}}
				if encodeValue == "hex" {
					schema.Pattern = "^[0-9a-fA-F]*$"
				} else {
					schema.ContentEncoding = encodeValue
				}
				return schema
			}
		}
		return scalarSchema(field.Type.Scalar)
	case onkir.KindEnum:
		if hasEncode && encodeValue == "number" {
			values := make([]*yaml.Node, len(field.Type.Enum.Values))
			for index := range field.Type.Enum.Values {
				values[index] = &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(index), Tag: "!!int"}
			}
			return &base.Schema{Type: []string{"integer"}, Enum: values}
		}
		return &base.Schema{AllOf: []*base.SchemaProxy{typeSchemaProxy(field.Type)}}
	case onkir.KindMessage:
		if empty, ok := field.Decorator("empty"); ok {
			value, _ := empty.Value()
			if value == "null" {
				return &base.Schema{AnyOf: []*base.SchemaProxy{
					typeSchemaProxy(field.Type),
					base.CreateSchemaProxy(&base.Schema{Type: []string{"null"}}),
				}}
			}
		}
		return &base.Schema{AllOf: []*base.SchemaProxy{typeSchemaProxy(field.Type)}}
	case onkir.KindMap:
		return &base.Schema{Type: []string{"object"}, AdditionalProperties: &base.DynamicValue[*base.SchemaProxy, bool]{A: typeSchemaProxy(field.Type.MapValue)}}
	default:
		return &base.Schema{}
	}
}

func applyFieldValidation(schema *base.Schema, field *onkir.Field) {
	if field.Doc != "" {
		schema.Description = field.Doc
	}
	for _, decorator := range field.Decorators {
		value, _ := decorator.Value()
		switch decorator.Name {
		case "email", "uuid", "uri":
			schema.Format = decorator.Name
		case "pattern":
			schema.Pattern = value
		case "len":
			minimum, _ := strconv.ParseInt(decorator.Args[0].Value, 10, 64)
			maximum, _ := strconv.ParseInt(decorator.Args[1].Value, 10, 64)
			schema.MinLength, schema.MaxLength = &minimum, &maximum
		case "min_items":
			minimum, _ := strconv.ParseInt(value, 10, 64)
			schema.MinItems = &minimum
		case "max_items":
			maximum, _ := strconv.ParseInt(value, 10, 64)
			schema.MaxItems = &maximum
		case "gt", "gte", "lt", "lte", "range":
			applyNumericValidation(schema, decorator)
		case "in":
			for _, arg := range decorator.Args {
				schema.Enum = append(schema.Enum, &yaml.Node{Kind: yaml.ScalarNode, Value: arg.Value})
			}
		}
	}
}

func applyNumericValidation(schema *base.Schema, decorator onkir.Decorator) {
	parse := func(index int) float64 {
		value, _ := strconv.ParseFloat(decorator.Args[index].Value, 64)
		return value
	}
	switch decorator.Name {
	case "gt":
		value := parse(0)
		schema.ExclusiveMinimum = &base.DynamicValue[bool, float64]{B: value, N: 1}
	case "gte":
		value := parse(0)
		schema.Minimum = &value
	case "lt":
		value := parse(0)
		schema.ExclusiveMaximum = &base.DynamicValue[bool, float64]{B: value, N: 1}
	case "lte":
		value := parse(0)
		schema.Maximum = &value
	case "range":
		minimum, maximum := parse(0), parse(1)
		schema.Minimum, schema.Maximum = &minimum, &maximum
	}
}

func flattenPrefix(field *onkir.Field) (string, bool) {
	decorator, ok := field.Decorator("flatten")
	if !ok {
		return "", false
	}
	prefix, _ := decorator.NamedArg("prefix")
	return prefix, true
}

func componentName(fullName string) string {
	return strings.Trim(fullName, ".")
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

func collectSchemas(schemas *orderedmap.Map[string, *base.SchemaProxy], m *onkir.Message) error {
	name := componentName(m.FullName())
	if _, exists := schemas.Get(name); exists {
		return fmt.Errorf("duplicate OpenAPI component name %q; declare unique packages", name)
	}
	schemas.Set(name, base.CreateSchemaProxy(messageSchema(m)))
	for _, nested := range m.Nested {
		if err := collectSchemas(schemas, nested); err != nil {
			return err
		}
	}
	for _, nested := range m.NestedEnums {
		name := componentName(nested.FullName())
		if _, exists := schemas.Get(name); exists {
			return fmt.Errorf("duplicate OpenAPI component name %q; declare unique packages", name)
		}
		schemas.Set(name, base.CreateSchemaProxy(enumSchema(nested)))
	}
	return nil
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
	if example, ok := h.Example(); ok {
		p.Example = &yaml.Node{Kind: yaml.ScalarNode, Value: example}
	}
	if _, deprecated := h.Deprecated(); deprecated {
		p.Deprecated = true
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
		OperationId: s.Name + "_" + m.Name,
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
		if _, auth := h.AuthType(); !auth {
			params = append(params, headerParameter(h))
		}
	}
	for _, h := range m.Headers {
		if _, auth := h.AuthType(); !auth {
			params = append(params, headerParameter(h))
		}
	}
	if !bodyBearing {
		params = append(params, queryParameters(m.Request)...)
	}
	if len(params) > 0 {
		op.Parameters = params
	}

	if bodyBearing {
		content := orderedmap.New[string, *v3.MediaType]()
		requestSchema := base.CreateSchemaProxyRef("#/components/schemas/" + componentName(m.Request.FullName()))
		if bodyField, ok := m.BodyField(); ok {
			if field := findField(m.Request, bodyField); field != nil {
				requestSchema = fieldSchemaProxy(field)
			}
		}
		content.Set("application/json", &v3.MediaType{
			Schema: requestSchema,
		})
		op.RequestBody = &v3.RequestBody{Required: new(true), Content: content}
	}

	responses := &v3.Responses{Codes: orderedmap.New[string, *v3.Response]()}
	if m.IsStream() {
		responses.Codes.Set("200", sseResponse(m))
	} else {
		successContent := orderedmap.New[string, *v3.MediaType]()
		successContent.Set("application/json", &v3.MediaType{
			Schema: base.CreateSchemaProxyRef("#/components/schemas/" + componentName(m.Response.FullName())),
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
			Schema: base.CreateSchemaProxyRef("#/components/schemas/" + componentName(errType.FullName())),
		})
		responses.Codes.Set(strconv.Itoa(status), &v3.Response{Description: errType.Name, Content: errContent})
	}
	op.Responses = responses
	security := orderedmap.New[string, []string]()
	for _, header := range append(append([]*onkir.Header{}, s.Headers...), m.Headers...) {
		if _, ok := header.AuthType(); ok {
			security.Set(authSchemeName(header), []string{})
		}
	}
	if orderedmap.Len(security) > 0 {
		op.Security = []*base.SecurityRequirement{{Requirements: security}}
	}

	return op
}

func findField(message *onkir.Message, name string) *onkir.Field {
	for _, field := range message.Fields {
		if field.Name == name {
			return field
		}
	}
	return nil
}

func authSchemeName(header *onkir.Header) string {
	if name, ok := header.AuthSchemeName(); ok && name != "" {
		return name
	}
	name := regexp.MustCompile(`[^A-Za-z0-9_.-]+`).ReplaceAllString(header.Name, "")
	if name == "" {
		name = "Header"
	}
	return name + "Auth"
}

func collectSecuritySchemes(file *onkir.File) (*orderedmap.Map[string, *v3.SecurityScheme], error) {
	schemes := orderedmap.New[string, *v3.SecurityScheme]()
	add := func(header *onkir.Header) error {
		authType, ok := header.AuthType()
		if !ok {
			return nil
		}
		name := authSchemeName(header)
		scheme := &v3.SecurityScheme{}
		switch authType {
		case "api_key":
			scheme.Type, scheme.Name, scheme.In = "apiKey", header.Name, "header"
		case "bearer", "basic":
			scheme.Type, scheme.Scheme = "http", authType
		default:
			return fmt.Errorf("unsupported auth type %q", authType)
		}
		if existing, exists := schemes.Get(name); exists {
			if existing.Type != scheme.Type || existing.Name != scheme.Name || existing.Scheme != scheme.Scheme {
				return fmt.Errorf("auth scheme name %q has conflicting definitions", name)
			}
			return nil
		}
		schemes.Set(name, scheme)
		return nil
	}
	for _, service := range file.Services {
		for _, header := range service.Headers {
			if err := add(header); err != nil {
				return nil, err
			}
		}
		for _, method := range service.Methods {
			for _, header := range method.Headers {
				if err := add(header); err != nil {
					return nil, err
				}
			}
		}
	}
	return schemes, nil
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
		if err := collectSchemas(schemas, m); err != nil {
			return nil, err
		}
	}
	for _, e := range file.Enums {
		name := componentName(e.FullName())
		if _, exists := schemas.Get(name); exists {
			return nil, fmt.Errorf("duplicate OpenAPI component name %q; declare unique packages", name)
		}
		schemas.Set(name, base.CreateSchemaProxy(enumSchema(e)))
	}
	securitySchemes, err := collectSecuritySchemes(file)
	if err != nil {
		return nil, err
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
		Components: &v3.Components{Schemas: schemas, SecuritySchemes: securitySchemes},
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
