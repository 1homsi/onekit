package genswift

import (
	"strings"
	"testing"
)

const mixedSchema = `
package app
message Ask { id: string }
message Reply { text: string }
service Chat {
  ping(Ask) -> Reply @get("/ping/{id}")
  live(Ask) -> Reply @ws("/live")
}
`

const socketOnlySchema = `
package app
message Ask { id: string }
message Reply { text: string }
service Live {
  live(Ask) -> Reply @ws("/live")
}
`

func TestClientSkipsWebSocketMethods(t *testing.T) {
	out := string(GenerateClient(compileSwiftSchema(t, mixedSchema)))
	if !strings.Contains(out, "public func ping(") {
		t.Fatalf("HTTP method missing:\n%s", out)
	}
	if strings.Contains(out, "func live(") {
		t.Fatalf("WebSocket methods are not generated for Swift yet:\n%s", out)
	}
}

func TestSocketOnlyServiceGeneratesNoClient(t *testing.T) {
	if client := GenerateClient(compileSwiftSchema(t, socketOnlySchema)); client != nil {
		t.Fatalf("a schema without HTTP methods must not produce a client file:\n%s", client)
	}
}

func TestIdentifiersAvoidSwiftKeywordsAndGeneratedMembers(t *testing.T) {
	for name, want := range map[string]string{
		"class": "class_", "switch": "switch_", "default": "default_", "description": "description_",
		"json": "json_", "validate": "validate_", "type": "type_", "self": "self_", "hash_value": "hashValue_",
		"plain": "plain", "user_id": "userId",
	} {
		if got := Ident(name); got != want {
			t.Errorf("Ident(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTypesAreFinalClassesSoMessagesCanBeRecursive(t *testing.T) {
	out := string(GenerateTypes(compileSwiftSchema(t, `
package app
message Node { name: string parent: Node? children: Node[] }
`)))
	for _, want := range []string{
		"public final class Node: OnekitMessage, Equatable, CustomStringConvertible {",
		"public var parent: Node?",
		"public var children: [Node]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestErrorMessagesConformToError(t *testing.T) {
	out := string(GenerateTypes(compileSwiftSchema(t, `
package app
message Ask { id: string }
message Reply { text: string }
message Missing @status(404) { code: string }
service Lookup { find(Ask) -> Reply | Missing @get("/find/{id}") }
`)))
	if !strings.Contains(out, "public final class Missing: OnekitMessage, Equatable, CustomStringConvertible, Error, @unchecked Sendable {") {
		t.Fatalf("declared error types must be throwable:\n%s", out)
	}
}

func TestDeprecatedFieldsKeepTheirWarningForCallersOnly(t *testing.T) {
	out := string(GenerateTypes(compileSwiftSchema(t, `
package app
message Legacy { old: string @deprecated("use new_name") new_name: string }
`)))
	for _, want := range []string{
		"private var _old: String",
		`@available(*, deprecated, message: "use new_name")`,
		"public var old: String {",
		"self._old = old",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}
