// Package onkcompat compares compiled .onk contracts for breaking changes.
package onkcompat

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

type Finding struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
}

func Compare(previous, current *onkir.Package) []Finding {
	var findings []Finding
	oldMessages, newMessages := messages(previous), messages(current)
	usage := messageUsage(previous)
	for name, use := range messageUsage(current) {
		usage[name] |= use
	}
	for name, old := range oldMessages {
		newer, ok := newMessages[name]
		if !ok {
			findings = append(findings, Finding{Path: name, Message: "message was removed"})
			continue
		}
		use := usage[name]
		if use == 0 {
			use = usedInRequest | usedInResponse
		}
		findings = append(findings, compareMessage(name, old, newer, use)...)
	}
	oldEnums, newEnums := enums(previous), enums(current)
	numbered := numberEncodedEnums(oldMessages)
	for name, old := range oldEnums {
		newer, ok := newEnums[name]
		if !ok {
			findings = append(findings, Finding{Path: name, Message: "enum was removed"})
			continue
		}
		findings = append(findings, compareEnum(name, old, newer)...)
		if usage[enumUsageKey(name)]&usedInResponse != 0 {
			findings = append(findings, addedEnumValues(name, old, newer)...)
		}
		if numbered[name] {
			findings = append(findings, compareEnumPositions(name, old, newer)...)
		}
	}
	oldRoutes, newRoutes := routes(previous), routes(current)
	for key, old := range oldRoutes {
		newer, ok := newRoutes[key]
		if !ok {
			findings = append(findings, Finding{Path: key, Message: "HTTP route was removed or changed"})
			continue
		}
		findings = append(findings, compareRoute(key, old, newer)...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path == findings[j].Path {
			return findings[i].Message < findings[j].Message
		}
		return findings[i].Path < findings[j].Path
	})
	return findings
}

type direction int

const (
	usedInRequest direction = 1 << iota
	usedInResponse
)

func messageUsage(pkg *onkir.Package) map[string]direction {
	out := map[string]direction{}
	if pkg == nil {
		return out
	}
	var markType func(*onkir.Type, direction)
	var mark func(*onkir.Message, direction)
	markType = func(typ *onkir.Type, use direction) {
		if typ == nil {
			return
		}
		switch typ.Kind {
		case onkir.KindMessage:
			mark(typ.Message, use)
		case onkir.KindEnum:
			out[enumUsageKey(typ.Enum.FullName())] |= use
		case onkir.KindMap:
			markType(typ.MapValue, use)
		}
	}
	mark = func(message *onkir.Message, use direction) {
		if message == nil || out[message.FullName()]&use == use {
			return
		}
		out[message.FullName()] |= use
		for _, field := range message.Fields {
			markType(field.Type, use)
			if field.Oneof != nil {
				for _, variant := range field.Oneof.Variants {
					markType(variant.Type, use)
				}
			}
		}
	}
	for _, file := range pkg.Files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				mark(method.Request, usedInRequest)
				mark(method.Response, usedInResponse)
				for _, errorType := range method.ErrorTypes {
					mark(errorType, usedInResponse)
				}
			}
		}
	}
	return out
}

func compareMessage(name string, old, current *onkir.Message, use direction) []Finding {
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
		if use&usedInRequest != 0 && !isRequired(oldField) && isRequired(newField) {
			findings = append(findings, Finding{Path: path, Message: "field became required"})
		}
		if use&usedInResponse != 0 && isRequired(oldField) && !isRequired(newField) {
			findings = append(findings, Finding{Path: path, Message: "field became optional in a response"})
		}
	}
	for fieldName, newField := range newFields {
		if _, existed := oldFields[fieldName]; !existed && use&usedInRequest != 0 && isRequired(newField) {
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

func enumUsageKey(name string) string {
	return "enum:" + name
}

func addedEnumValues(name string, old, current *onkir.Enum) []Finding {
	existing := map[string]bool{}
	for _, value := range old.Values {
		existing[value.Name] = true
	}
	var findings []Finding
	for _, value := range current.Values {
		if !existing[value.Name] {
			findings = append(findings, Finding{Path: name + "." + value.Name, Message: "enum value was added to a response enum; older clients reject unknown values"})
		}
	}
	return findings
}

func numberEncodedEnums(messages map[string]*onkir.Message) map[string]bool {
	out := map[string]bool{}
	for _, message := range messages {
		for _, field := range message.Fields {
			if field.Type == nil || field.Type.Kind != onkir.KindEnum {
				continue
			}
			if encode, ok := field.Decorator("encode"); ok {
				if value, _ := encode.Value(); value == "number" {
					out[field.Type.Enum.FullName()] = true
				}
			}
		}
	}
	return out
}

func compareEnumPositions(name string, old, current *onkir.Enum) []Finding {
	positions := map[string]int{}
	for index, value := range current.Values {
		positions[value.Name] = index
	}
	var findings []Finding
	for index, value := range old.Values {
		if position, ok := positions[value.Name]; ok && position != index {
			findings = append(findings, Finding{Path: name + "." + value.Name, Message: fmt.Sprintf("enum value moved from %d to %d, changing its @encode(number) wire value", index, position)})
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
	parts := []string{typeName(field.Type), fmt.Sprintf("repeated=%t", field.Repeated)}
	if field.Oneof != nil {
		parts = append(parts, "oneof")
		variants := make([]string, 0, len(field.Oneof.Variants))
		for _, variant := range field.Oneof.Variants {
			decorators := make([]string, 0, len(variant.Decorators))
			for _, decorator := range variant.Decorators {
				decorators = append(decorators, decoratorSignature(decorator))
			}
			sort.Strings(decorators)
			variants = append(variants, variant.Name+":"+typeName(variant.Type)+":"+variant.Tag()+":"+strings.Join(decorators, ","))
		}
		sort.Strings(variants)
		parts = append(parts, "variants="+strings.Join(variants, "|"))
		discriminator, ok := field.Oneof.Discriminator()
		if !ok || discriminator == "" {
			discriminator = "type"
		}
		parts = append(parts, "discriminator="+discriminator)
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

// routes snapshots every RPC's HTTP contract keyed by qualified service
// name plus verb and effective path. Service identity is part of the key so
// that two services whose effective routes collide (possible when compile-
// time uniqueness is scoped by declared base_path/package but comparison
// uses post-inference paths) cannot silently overwrite each other's entry -
// which previously made detection depend on file iteration order.
func routes(pkg *onkir.Package) map[string][]string {
	out := map[string][]string{}
	if pkg == nil {
		return out
	}
	for _, file := range pkg.Files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				if wsPath, ok := method.WebSocketPath(); ok {
					out[routeKey(file.Package, service.Name, "ws", service.BasePath+wsPath)] = methodSignature(service, method)
					continue
				}
				verb, verbOK := method.Verb()
				methodPath, pathOK := method.Path()
				if verbOK && pathOK {
					out[routeKey(file.Package, service.Name, verb, service.BasePath+methodPath)] = methodSignature(service, method)
				}
			}
		}
	}
	return out
}

func routeKey(pkg, service, verb, fullPath string) string {
	if pkg == "" {
		return service + " " + verb + " " + fullPath
	}
	return pkg + "." + service + " " + verb + " " + fullPath
}

var routePartLabels = map[string]string{
	"service":  "service name",
	"method":   "method name",
	"request":  "request message",
	"response": "response message",
	"stream":   "streaming mode",
	"body":     "body field",
	"query":    "query parameters",
	"header":   "headers",
	"error":    "error responses",
}

func compareRoute(key string, old, current []string) []Finding {
	group := func(parts []string) map[string][]string {
		out := map[string][]string{}
		for _, part := range parts {
			kind, value, _ := strings.Cut(part, "=")
			out[kind] = append(out[kind], value)
		}
		return out
	}
	oldParts, newParts := group(old), group(current)
	kinds := map[string]bool{}
	for kind := range oldParts {
		kinds[kind] = true
	}
	for kind := range newParts {
		kinds[kind] = true
	}
	var findings []Finding
	for kind := range kinds {
		before, after := strings.Join(oldParts[kind], ", "), strings.Join(newParts[kind], ", ")
		if before == after {
			continue
		}
		label := routePartLabels[kind]
		if label == "" {
			label = kind
		}
		findings = append(findings, Finding{Path: key, Message: label + " changed", Before: before, After: after})
	}
	return findings
}

func methodSignature(service *onkir.Service, method *onkir.Method) []string {
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
			if name == "" {
				name = field.Name
			}
			parts = append(parts, "query="+field.Name+":"+name)
		}
	}
	for _, header := range slices.Concat(service.Headers, method.Headers) {
		_, hasFormat := header.Format()
		_, hasAuth := header.AuthType()
		if !header.Required() && !hasFormat && !hasAuth {
			continue
		}
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
	return parts
}

func headerSignature(header *onkir.Header) string {
	parts := []string{strings.ToLower(header.Name), header.Type.String(), fmt.Sprintf("required=%t", header.Required())}
	for _, decorator := range header.Decorators {
		switch decorator.Name {
		case "example", "deprecated", "auth_scheme_name":
			continue
		}
		parts = append(parts, decoratorSignature(decorator))
	}
	sort.Strings(parts)
	return strings.Join(parts, ":")
}
