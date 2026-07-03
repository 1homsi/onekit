package httpgen

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/stackxio/onekit/internal/annotations"
)

// generateMockFile generates a mock server implementation file.
func (g *Generator) generateMockFile(file *protogen.File) error {
	filename := file.GeneratedFilenamePrefix + "_http_mock.pb.go"
	gf := g.plugin.NewGeneratedFile(filename, file.GoImportPath)

	g.writeHeader(gf, file)

	// Imports
	gf.P("import (")
	gf.P(`"context"`)
	gf.P(`cryptorand "crypto/rand"`)
	gf.P(`"fmt"`)
	gf.P(`"math/rand"`)
	gf.P(`"strconv"`)
	gf.P(`"time"`)
	gf.P()
	gf.P(`"google.golang.org/protobuf/proto"`)
	gf.P(`"google.golang.org/protobuf/reflect/protoreflect"`)
	gf.P(")")
	gf.P()

	// Generate field examples storage
	if err := g.generateFieldExamplesStorage(gf, file); err != nil {
		return err
	}

	// Generate mock servers for each service
	for _, service := range file.Services {
		if err := g.generateMockService(gf, file, service); err != nil {
			return err
		}
	}

	// Generate helper functions
	g.generateMockHelpers(gf)

	return nil
}

// generateFieldExamplesStorage generates storage for field examples.
func (g *Generator) generateFieldExamplesStorage(gf *protogen.GeneratedFile, file *protogen.File) error {
	gf.P("// Field examples extracted from proto definitions")
	gf.P("var fieldExamples = map[string][]string{")

	// Collect all field examples from all messages
	for _, message := range file.Messages {
		g.collectMessageFieldExamples(gf, message, "")
	}

	gf.P("}")
	gf.P()

	return nil
}

// collectMessageFieldExamples recursively collects field examples.
func (g *Generator) collectMessageFieldExamples(gf *protogen.GeneratedFile, message *protogen.Message, prefix string) {
	messagePath := prefix + string(message.Desc.Name())

	for _, field := range message.Fields {
		examples := annotations.GetFieldExamples(field)
		if len(examples) > 0 {
			fieldPath := messagePath + "." + string(field.Desc.Name())
			gf.P(`"`, fieldPath, `": {`)
			for _, example := range examples {
				gf.P(`"`, example, `",`)
			}
			gf.P("},")
		}
	}

	// Process nested messages
	for _, nested := range message.Messages {
		g.collectMessageFieldExamples(gf, nested, messagePath+".")
	}
}

// generateMockService generates a mock implementation for a service.
func (g *Generator) generateMockService(
	gf *protogen.GeneratedFile,
	_ *protogen.File,
	service *protogen.Service,
) error {
	serviceName := service.GoName

	// Mock server struct
	gf.P("// Mock", serviceName, "Server is a mock implementation of ", serviceName, "Server.")
	gf.P("type Mock", serviceName, "Server struct {")
	gf.P("// Add any mock-specific fields here")
	gf.P("}")
	gf.P()

	// Constructor
	gf.P("// NewMock", serviceName, "Server creates a new mock server for ", serviceName, ".")
	gf.P("func NewMock", serviceName, "Server() *Mock", serviceName, "Server {")
	gf.P("return &Mock", serviceName, "Server{}")
	gf.P("}")
	gf.P()

	// Generate mock methods
	for _, method := range service.Methods {
		if err := g.generateMockMethod(gf, service, method); err != nil {
			return err
		}
	}

	return nil
}

// generateMockMethod generates a mock implementation for an RPC method.
func (g *Generator) generateMockMethod(
	gf *protogen.GeneratedFile,
	service *protogen.Service,
	method *protogen.Method,
) error {
	methodName := method.GoName
	inputType := method.Input.GoIdent
	outputType := method.Output.GoIdent

	gf.P("// ", methodName, " is a mock implementation of ", service.GoName, "Server.", methodName, ".")
	gf.P(
		"func (m *Mock",
		service.GoName,
		"Server) ",
		methodName,
		"(ctx context.Context, req *",
		inputType,
		") (*",
		outputType,
		", error) {",
	)

	// Validate request
	gf.P("// Validate the request")
	gf.P("if msg, ok := any(req).(proto.Message); ok {")
	gf.P("if err := ValidateMessage(msg); err != nil {")
	gf.P("return nil, err")
	gf.P("}")
	gf.P("}")
	gf.P()

	// Generate response
	gf.P("// Generate mock response")
	gf.P("resp := &", outputType, "{}")
	gf.P()

	// Fill response fields
	g.generateMockFieldAssignments(gf, method.Output, "resp")

	gf.P("return resp, nil")
	gf.P("}")
	gf.P()

	return nil
}

// generateMockFieldAssignments generates field assignments for a message.
func (g *Generator) generateMockFieldAssignments(
	gf *protogen.GeneratedFile,
	message *protogen.Message,
	varName string,
) {
	messageName := string(message.Desc.Name())

	for _, field := range message.Fields {
		fieldName := field.GoName
		fieldPath := messageName + "." + string(field.Desc.Name())

		if field.Desc.IsList() && !field.Desc.IsMap() {
			g.generateMockRepeatedFieldAssignment(gf, field, varName, fieldPath)
			continue
		}

		// Generate assignment based on field type
		switch field.Desc.Kind() {
		case protoreflect.StringKind:
			g.generateMockReflectFieldAssignment(gf, field, varName, fieldPath)
		case protoreflect.Int32Kind,
			protoreflect.Int64Kind,
			protoreflect.Sint32Kind,
			protoreflect.Uint32Kind,
			protoreflect.Sint64Kind,
			protoreflect.Uint64Kind,
			protoreflect.Sfixed32Kind,
			protoreflect.Fixed32Kind,
			protoreflect.Sfixed64Kind,
			protoreflect.Fixed64Kind:
			g.generateMockReflectFieldAssignment(gf, field, varName, fieldPath)
		case protoreflect.BoolKind:
			g.generateMockReflectFieldAssignment(gf, field, varName, fieldPath)
		case protoreflect.FloatKind, protoreflect.DoubleKind:
			g.generateMockReflectFieldAssignment(gf, field, varName, fieldPath)
		case protoreflect.MessageKind:
			if field.Desc.IsMap() {
				// Handle map fields
				g.generateMockMapFieldAssignment(gf, field, varName)
			} else {
				gf.P(varName, ".", fieldName, " = &", field.Message.GoIdent, "{}")
				g.generateMockFieldAssignments(gf, field.Message, varName+"."+fieldName)
			}
		case protoreflect.EnumKind, protoreflect.BytesKind:
			g.generateMockReflectFieldAssignment(gf, field, varName, fieldPath)
		case protoreflect.GroupKind:
			gf.P("// Skipping unsupported group field ", fieldName)
		default:
			gf.P("// Skipping unsupported field ", fieldName, " of type ", field.Desc.Kind())
		}
	}
}

func (g *Generator) generateMockReflectFieldAssignment(
	gf *protogen.GeneratedFile,
	field *protogen.Field,
	varName string,
	fieldPath string,
) {
	gf.P(
		`setMockField(`,
		varName,
		`, "`,
		field.Desc.Name(),
		`", `,
		g.getMockReflectValueExpr(field, fieldPath),
		`)`,
	)
}

// generateMockRepeatedFieldAssignment generates sample values for repeated fields.
func (g *Generator) generateMockRepeatedFieldAssignment(
	gf *protogen.GeneratedFile,
	field *protogen.Field,
	varName string,
	fieldPath string,
) {
	fieldName := field.GoName

	if field.Desc.Kind() == protoreflect.MessageKind {
		gf.P(varName, ".", fieldName, " = []*", gf.QualifiedGoIdent(field.Message.GoIdent), "{{}}")
		elemVar := varName + "." + fieldName + "[0]"
		g.generateMockFieldAssignments(gf, field.Message, elemVar)
		return
	}

	elemType := g.getGoTypeScalar(gf, field)
	valueExpr := g.getMockScalarValueExpr(gf, field, fieldPath)
	gf.P(varName, ".", fieldName, " = []", elemType, "{", valueExpr, "}")
}

// generateMockMapFieldAssignment generates code to populate a map field with sample data.
func (g *Generator) generateMockMapFieldAssignment(
	gf *protogen.GeneratedFile,
	field *protogen.Field,
	varName string,
) {
	fieldName := field.GoName

	// Get the map's key and value types
	keyField := field.Message.Fields[0]
	valueField := field.Message.Fields[1]

	// Initialize the map with properly qualified types
	keyType := g.getGoTypeScalar(gf, keyField)

	// Generate a sample key
	sampleKey := g.getSampleMapKey(keyField)

	// Generate map entry based on value type
	if valueField.Desc.Kind() == protoreflect.MessageKind {
		// Value is a message type - use QualifiedGoIdent for proper imports
		gf.P(
			varName,
			".",
			fieldName,
			" = make(map[",
			keyType,
			"]*",
			gf.QualifiedGoIdent(valueField.Message.GoIdent),
			")",
		)
		gf.P(varName, ".", fieldName, "[", sampleKey, "] = &", valueField.Message.GoIdent, "{}")
		// Populate the value message fields
		mapValueVar := varName + "." + fieldName + "[" + sampleKey + "]"
		g.generateMockFieldAssignments(gf, valueField.Message, mapValueVar)
	} else {
		// Value is a scalar type
		valueType := g.getGoTypeScalar(gf, valueField)
		gf.P(varName, ".", fieldName, " = make(map[", keyType, "]", valueType, ")")
		valueExpr := g.getMockScalarValueExpr(gf, valueField, string(field.Desc.Name())+".value")
		gf.P(varName, ".", fieldName, "[", sampleKey, "] = ", valueExpr)
	}
}

// getGoTypeScalar returns the Go type string for scalar fields only.
func (g *Generator) getGoTypeScalar(gf *protogen.GeneratedFile, field *protogen.Field) string {
	switch field.Desc.Kind() {
	case protoreflect.StringKind:
		return kindString
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return kindInt32
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return kindInt64
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return kindUint32
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return kindUint64
	case protoreflect.BoolKind:
		return kindBool
	case protoreflect.FloatKind:
		return "float32"
	case protoreflect.DoubleKind:
		return "float64"
	case protoreflect.BytesKind:
		return "[]byte"
	case protoreflect.MessageKind:
		return "*" + gf.QualifiedGoIdent(field.Message.GoIdent)
	case protoreflect.EnumKind:
		return gf.QualifiedGoIdent(field.Enum.GoIdent)
	case protoreflect.GroupKind:
		return kindInterface
	default:
		return kindInterface
	}
}

func (g *Generator) getMockScalarValueExpr(
	gf *protogen.GeneratedFile,
	field *protogen.Field,
	fieldPath string,
) string {
	switch field.Desc.Kind() {
	case protoreflect.StringKind:
		return `selectStringExample("` + fieldPath + `", ` + g.getDefaultGenerator(field) + `)`
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return g.getGoTypeScalar(gf, field) + `(selectIntExample("` + fieldPath + `", 42))`
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return g.getGoTypeScalar(gf, field) + `(selectIntExample("` + fieldPath + `", 42))`
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return g.getGoTypeScalar(gf, field) + `(selectUintExample("` + fieldPath + `", 42))`
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return g.getGoTypeScalar(gf, field) + `(selectUintExample("` + fieldPath + `", 42))`
	case protoreflect.BoolKind:
		return `selectBoolExample("` + fieldPath + `", true)`
	case protoreflect.FloatKind:
		return `float32(selectFloatExample("` + fieldPath + `", 3.14))`
	case protoreflect.DoubleKind:
		return `selectFloatExample("` + fieldPath + `", 3.14)`
	case protoreflect.BytesKind:
		return `[]byte(selectStringExample("` + fieldPath + `", generateString))`
	case protoreflect.EnumKind:
		return gf.QualifiedGoIdent(field.Enum.GoIdent) + `(selectIntExample("` + fieldPath + `", 0))`
	default:
		return `nil`
	}
}

func (g *Generator) getMockReflectValueExpr(field *protogen.Field, fieldPath string) string {
	switch field.Desc.Kind() {
	case protoreflect.StringKind:
		return `protoreflect.ValueOfString(selectStringExample("` + fieldPath + `", ` + g.getDefaultGenerator(field) + `))`
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return `protoreflect.ValueOfInt32(int32(selectIntExample("` + fieldPath + `", 42)))`
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return `protoreflect.ValueOfInt64(selectIntExample("` + fieldPath + `", 42))`
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return `protoreflect.ValueOfUint32(uint32(selectUintExample("` + fieldPath + `", 42)))`
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return `protoreflect.ValueOfUint64(selectUintExample("` + fieldPath + `", 42))`
	case protoreflect.BoolKind:
		return `protoreflect.ValueOfBool(selectBoolExample("` + fieldPath + `", true))`
	case protoreflect.FloatKind:
		return `protoreflect.ValueOfFloat32(float32(selectFloatExample("` + fieldPath + `", 3.14)))`
	case protoreflect.DoubleKind:
		return `protoreflect.ValueOfFloat64(selectFloatExample("` + fieldPath + `", 3.14))`
	case protoreflect.BytesKind:
		return `protoreflect.ValueOfBytes([]byte(selectStringExample("` + fieldPath + `", generateString)))`
	case protoreflect.EnumKind:
		return `protoreflect.ValueOfEnum(protoreflect.EnumNumber(selectIntExample("` + fieldPath + `", 0)))`
	default:
		return `protoreflect.Value{}`
	}
}

// getSampleMapKey returns a sample key value for a map field.
func (g *Generator) getSampleMapKey(keyField *protogen.Field) string {
	switch keyField.Desc.Kind() {
	case protoreflect.StringKind:
		return `"sample_key"`
	case protoreflect.Int32Kind, protoreflect.Int64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		return "1"
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return "1"
	case protoreflect.BoolKind:
		return "true"
	case protoreflect.EnumKind:
		return "0"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "1.0"
	case protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind:
		return `"key"`
	default:
		return `"key"`
	}
}

// getDefaultGenerator returns a function name for generating default values.
func (g *Generator) getDefaultGenerator(field *protogen.Field) string {
	fieldName := strings.ToLower(string(field.Desc.Name()))

	// Check for common field names
	switch {
	case strings.Contains(fieldName, "id"):
		return "generateUUID"
	case strings.Contains(fieldName, "email"):
		return "generateEmail"
	case strings.Contains(fieldName, "name"):
		return "generateName"
	case strings.Contains(fieldName, "phone"):
		return "generatePhone"
	case strings.Contains(fieldName, "address"):
		return "generateAddress"
	case strings.Contains(fieldName, "url"):
		return "generateURL"
	default:
		return "generateString"
	}
}

// getDefaultValue returns a default value for a field.
func (g *Generator) getDefaultValue(field *protogen.Field) string {
	switch field.Desc.Kind() {
	case protoreflect.Int32Kind, protoreflect.Int64Kind:
		return "42"
	case protoreflect.BoolKind:
		return "true"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "3.14"
	case protoreflect.EnumKind,
		protoreflect.Sint32Kind,
		protoreflect.Uint32Kind,
		protoreflect.Sint64Kind,
		protoreflect.Uint64Kind,
		protoreflect.Sfixed32Kind,
		protoreflect.Fixed32Kind,
		protoreflect.Sfixed64Kind,
		protoreflect.Fixed64Kind,
		protoreflect.StringKind,
		protoreflect.BytesKind,
		protoreflect.MessageKind,
		protoreflect.GroupKind:
		return `""`
	default:
		return `""`
	}
}

// generateMockHelpers generates helper functions for mock data generation.
func (g *Generator) generateMockHelpers(gf *protogen.GeneratedFile) {
	g.generateSetMockFieldHelper(gf)
	g.generateExampleSelectors(gf)
	g.generateDefaultGenerators(gf)
	g.generateInitFunction(gf)
}

func (g *Generator) generateSetMockFieldHelper(gf *protogen.GeneratedFile) {
	gf.P("func setMockField(msg proto.Message, fieldName string, value protoreflect.Value) {")
	gf.P("fields := msg.ProtoReflect().Descriptor().Fields()")
	gf.P("field := fields.ByName(protoreflect.Name(fieldName))")
	gf.P("if field == nil {")
	gf.P("return")
	gf.P("}")
	gf.P("msg.ProtoReflect().Set(field, value)")
	gf.P("}")
	gf.P()
}

// generateExampleSelectors generates functions to select examples from predefined values.
func (g *Generator) generateExampleSelectors(gf *protogen.GeneratedFile) {
	// String example selector
	gf.P("// selectStringExample selects a random example or generates a default value.")
	gf.P("func selectStringExample(fieldPath string, defaultGenerator func() string) string {")
	gf.P("if examples, ok := fieldExamples[fieldPath]; ok && len(examples) > 0 {")
	gf.P("return examples[rand.Intn(len(examples))]")
	gf.P("}")
	gf.P("return defaultGenerator()")
	gf.P("}")
	gf.P()

	// Int example selector
	gf.P("// selectIntExample selects a random example or returns a default value.")
	gf.P("func selectIntExample(fieldPath string, defaultValue int64) int64 {")
	gf.P("if examples, ok := fieldExamples[fieldPath]; ok && len(examples) > 0 {")
	gf.P("example := examples[rand.Intn(len(examples))]")
	gf.P("if v, err := strconv.ParseInt(example, 10, 64); err == nil {")
	gf.P("return v")
	gf.P("}")
	gf.P("}")
	gf.P("return defaultValue")
	gf.P("}")
	gf.P()

	// Uint example selector
	gf.P("// selectUintExample selects a random example or returns a default value.")
	gf.P("func selectUintExample(fieldPath string, defaultValue uint64) uint64 {")
	gf.P("if examples, ok := fieldExamples[fieldPath]; ok && len(examples) > 0 {")
	gf.P("example := examples[rand.Intn(len(examples))]")
	gf.P("if v, err := strconv.ParseUint(example, 10, 64); err == nil {")
	gf.P("return v")
	gf.P("}")
	gf.P("}")
	gf.P("return defaultValue")
	gf.P("}")
	gf.P()

	// Bool example selector
	gf.P("// selectBoolExample selects a random example or returns a default value.")
	gf.P("func selectBoolExample(fieldPath string, defaultValue bool) bool {")
	gf.P("if examples, ok := fieldExamples[fieldPath]; ok && len(examples) > 0 {")
	gf.P("example := examples[rand.Intn(len(examples))]")
	gf.P("if v, err := strconv.ParseBool(example); err == nil {")
	gf.P("return v")
	gf.P("}")
	gf.P("}")
	gf.P("return defaultValue")
	gf.P("}")
	gf.P()

	// Float example selector
	gf.P("// selectFloatExample selects a random example or returns a default value.")
	gf.P("func selectFloatExample(fieldPath string, defaultValue float64) float64 {")
	gf.P("if examples, ok := fieldExamples[fieldPath]; ok && len(examples) > 0 {")
	gf.P("example := examples[rand.Intn(len(examples))]")
	gf.P("if v, err := strconv.ParseFloat(example, 64); err == nil {")
	gf.P("return v")
	gf.P("}")
	gf.P("}")
	gf.P("return defaultValue")
	gf.P("}")
	gf.P()
}

// generateDefaultGenerators generates default value generator functions.
func (g *Generator) generateDefaultGenerators(gf *protogen.GeneratedFile) {
	gf.P("// Default value generators")
	gf.P("func generateUUID() string {")
	gf.P("var b [16]byte")
	gf.P("_, err := cryptorand.Read(b[:])")
	gf.P("if err != nil {")
	gf.P(`return "550e8400-e29b-41d4-a716-446655440000" // fallback`)
	gf.P("}")
	gf.P("b[6] = (b[6] & 0x0f) | 0x40 // Version 4")
	gf.P("b[8] = (b[8] & 0x3f) | 0x80 // Variant bits")
	gf.P("return fmt.Sprintf(\"%x-%x-%x-%x-%x\", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])")
	gf.P("}")
	gf.P()

	gf.P("func generateEmail() string {")
	gf.P(`return "user@example.com"`)
	gf.P("}")
	gf.P()

	gf.P("func generateName() string {")
	gf.P(`names := []string{"Alice Johnson", "Bob Smith", "Charlie Davis", "Diana Wilson"}`)
	gf.P("return names[rand.Intn(len(names))]")
	gf.P("}")
	gf.P()

	gf.P("func generatePhone() string {")
	gf.P(`return "+1-555-0123"`)
	gf.P("}")
	gf.P()

	gf.P("func generateAddress() string {")
	gf.P(`return "123 Main Street, Anytown, USA"`)
	gf.P("}")
	gf.P()

	gf.P("func generateURL() string {")
	gf.P(`return "https://example.com"`)
	gf.P("}")
	gf.P()

	gf.P("func generateString() string {")
	gf.P(`return "example string"`)
	gf.P("}")
	gf.P()
}

// generateInitFunction generates the init function for seeding random number generator.
func (g *Generator) generateInitFunction(gf *protogen.GeneratedFile) {
	gf.P("func init() {")
	gf.P("rand.Seed(time.Now().UnixNano())")
	gf.P("}")
}
