// Package onkcompat compares compiled .onk contracts for breaking changes.
package onkcompat

import (
	"fmt"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

type Finding struct{ Path, Message string }

func Compare(previous, current *onkir.Package) []Finding {
	var findings []Finding
	oldMessages, newMessages := messages(previous), messages(current)
	for name, old := range oldMessages {
		newer, ok := newMessages[name]
		if !ok {
			findings = append(findings, Finding{Path: name, Message: "message was removed"})
			continue
		}
		findings = append(findings, compareMessage(name, old, newer)...)
	}
	oldEnums, newEnums := enums(previous), enums(current)
	for name, old := range oldEnums {
		newer, ok := newEnums[name]
		if !ok {
			findings = append(findings, Finding{Path: name, Message: "enum was removed"})
			continue
		}
		findings = append(findings, compareEnum(name, old, newer)...)
	}
	oldRoutes, newRoutes := routes(previous), routes(current)
	for key, old := range oldRoutes {
		newer, ok := newRoutes[key]
		if !ok {
			findings = append(findings, Finding{Path: key, Message: "HTTP route was removed or changed"})
			continue
		}
		if old != newer {
			findings = append(findings, Finding{Path: key, Message: "HTTP binding, headers, errors, or payload contract changed"})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path == findings[j].Path {
			return findings[i].Message < findings[j].Message
		}
		return findings[i].Path < findings[j].Path
	})
	return findings
}

func compareMessage(name string, old, current *onkir.Message) []Finding {
	var findings []Finding
	oldFields, newFields := fields(old), fields(current)
	for fieldName, oldField := range oldFields {
		path := name + "." + fieldName
		newField, exists := newFields[fieldName]
		if !exists {
			findings = append(findings, Finding{Path: path, Message: "field was removed"})
			continue
		}
		if fieldTypeSignature(oldField) != fieldTypeSignature(newField) {
			findings = append(findings, Finding{Path: path, Message: "field type, cardinality, or oneof contract changed"})
		}
		if fieldContractSignature(oldField) != fieldContractSignature(newField) {
			findings = append(findings, Finding{Path: path, Message: "field validation or JSON mapping changed"})
		}
		if !isRequired(oldField) && isRequired(newField) {
			findings = append(findings, Finding{Path: path, Message: "field became required"})
		}
	}
	for fieldName, newField := range newFields {
		if _, existed := oldFields[fieldName]; !existed && isRequired(newField) {
			findings = append(findings, Finding{Path: name + "." + fieldName, Message: "required field was added"})
		}
	}
	return findings
}

func compareEnum(name string, old, current *onkir.Enum) []Finding {
	var findings []Finding
	newValues := map[string]string{}
	for _, value := range current.Values {
		newValues[value.Name] = value.JSONName()
	}
	for _, value := range old.Values {
		jsonName, exists := newValues[value.Name]
		if !exists {
			findings = append(findings, Finding{Path: name + "." + value.Name, Message: "enum value was removed"})
		} else if jsonName != value.JSONName() {
			findings = append(findings, Finding{Path: name + "." + value.Name, Message: "enum JSON value changed"})
		}
	}
	return findings
}

func messages(pkg *onkir.Package) map[string]*onkir.Message {
	out := map[string]*onkir.Message{}
	if pkg == nil {
		return out
	}
	var add func(*onkir.Message)
	add = func(message *onkir.Message) {
		out[message.FullName()] = message
		for _, nested := range message.Nested {
			add(nested)
		}
	}
	for _, file := range pkg.Files {
		for _, message := range file.Messages {
			add(message)
		}
	}
	return out
}

func enums(pkg *onkir.Package) map[string]*onkir.Enum {
	out := map[string]*onkir.Enum{}
	if pkg == nil {
		return out
	}
	var addMessage func(*onkir.Message)
	addMessage = func(message *onkir.Message) {
		for _, enum := range message.NestedEnums {
			out[enum.FullName()] = enum
		}
		for _, nested := range message.Nested {
			addMessage(nested)
		}
	}
	for _, file := range pkg.Files {
		for _, enum := range file.Enums {
			out[enum.FullName()] = enum
		}
		for _, message := range file.Messages {
			addMessage(message)
		}
	}
	return out
}

func fields(message *onkir.Message) map[string]*onkir.Field {
	out := map[string]*onkir.Field{}
	for _, field := range message.Fields {
		out[field.Name] = field
	}
	return out
}

func fieldTypeSignature(field *onkir.Field) string {
	parts := []string{typeName(field.Type), fmt.Sprintf("optional=%t", field.Optional), fmt.Sprintf("repeated=%t", field.Repeated)}
	if field.Oneof != nil {
		parts = append(parts, "oneof")
		for _, variant := range field.Oneof.Variants {
			parts = append(parts, variant.Name+":"+typeName(variant.Type)+":"+variant.Tag())
		}
		if discriminator, ok := field.Oneof.Discriminator(); ok {
			parts = append(parts, "discriminator="+discriminator)
		}
		parts = append(parts, fmt.Sprintf("flatten=%t", field.Oneof.Flatten()))
	}
	return strings.Join(parts, "|")
}

func fieldContractSignature(field *onkir.Field) string {
	var parts []string
	for _, decorator := range field.Decorators {
		if decorator.Name == "query" || decorator.Name == "required" {
			continue
		}
		parts = append(parts, decoratorSignature(decorator))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func decoratorSignature(decorator onkir.Decorator) string {
	parts := []string{decorator.Name}
	for _, arg := range decorator.Args {
		parts = append(parts, arg.Name+"="+arg.Value)
	}
	return strings.Join(parts, ":")
}

func isRequired(field *onkir.Field) bool {
	return !field.Optional || field.HasDecorator("required")
}

func typeName(typ *onkir.Type) string {
	if typ == nil {
		return ""
	}
	switch typ.Kind {
	case onkir.KindScalar:
		return typ.Scalar.String()
	case onkir.KindMessage:
		return "message:" + typ.Message.FullName()
	case onkir.KindEnum:
		return "enum:" + typ.Enum.FullName()
	case onkir.KindMap:
		return "map:" + typ.MapKey.String() + ":" + typeName(typ.MapValue)
	default:
		return "unknown"
	}
}

func routes(pkg *onkir.Package) map[string]string {
	out := map[string]string{}
	if pkg == nil {
		return out
	}
	for _, file := range pkg.Files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				verb, verbOK := method.Verb()
				methodPath, pathOK := method.Path()
				if verbOK && pathOK {
					out[verb+" "+service.BasePath+methodPath] = methodSignature(service, method)
				}
			}
		}
	}
	return out
}

func methodSignature(service *onkir.Service, method *onkir.Method) string {
	parts := []string{
		"service=" + service.Name,
		"method=" + method.Name,
		"request=" + method.Request.FullName(),
		"response=" + method.Response.FullName(),
		fmt.Sprintf("stream=%t", method.IsStream()),
	}
	if body, ok := method.BodyField(); ok {
		parts = append(parts, "body="+body)
	}
	for _, field := range method.Request.Fields {
		if query, ok := field.Decorator("query"); ok {
			name, _ := query.Value()
			parts = append(parts, "query="+field.Name+":"+name)
		}
	}
	for _, header := range append(append([]*onkir.Header{}, service.Headers...), method.Headers...) {
		parts = append(parts, "header="+headerSignature(header))
	}
	for _, errorType := range method.ErrorTypes {
		status := 500
		if code, ok := errorType.StatusCode(); ok {
			status = code
		}
		parts = append(parts, fmt.Sprintf("error=%s:%d", errorType.FullName(), status))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func headerSignature(header *onkir.Header) string {
	parts := []string{strings.ToLower(header.Name), header.Type.String(), fmt.Sprintf("required=%t", header.Required())}
	for _, decorator := range header.Decorators {
		parts = append(parts, decoratorSignature(decorator))
	}
	sort.Strings(parts)
	return strings.Join(parts, ":")
}
