package genswift

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
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

type testNamespaces map[string]string

func (r testNamespaces) ResolveMessage(m *onkir.Message) (string, bool) {
	namespace, ok := r[m.Name]
	return namespace, ok
}

func (r testNamespaces) ResolveEnum(e *onkir.Enum) (string, bool) {
	namespace, ok := r[e.Name]
	return namespace, ok
}

func TestNamespacedTypesWrapDeclarationsAndQualifyForeignReferences(t *testing.T) {
	file := compileSwiftSchema(t, `
package app
enum Kind { A B }
message Money { cents: int64 }
message Order { total: Money kind: Kind history: Money[] by_kind: map[string, Money] }
message Get { id: string }
message NotFound @status(404) { id: string }
service Orders { get(Get) -> Order | NotFound @get("/orders/{id}") }
`)
	resolver := testNamespaces{"Money": "CommonV1", "Kind": "CommonV1"}
	types := string(GenerateTypesInNamespace(file, "ShopV1", resolver))
	for _, want := range []string{
		"public enum ShopV1 {}",
		"extension ShopV1 {",
		"public var total: CommonV1.Money?",
		"public var kind: CommonV1.Kind",
		"public var history: [CommonV1.Money]",
		"public var byKind: [String: CommonV1.Money]",
		"CommonV1.Money(json:",
		"CommonV1.Kind.fromWire(",
		"public final class Order:",
	} {
		if !strings.Contains(types, want) {
			t.Fatalf("missing %q in:\n%s", want, types)
		}
	}
	client := string(GenerateClientInNamespace(file, "ShopV1", resolver))
	for _, want := range []string{"extension ShopV1 {", "public final class OrdersClient", "NotFound(json: payload)"} {
		if !strings.Contains(client, want) {
			t.Fatalf("client missing %q:\n%s", want, client)
		}
	}
}

func TestFlatGenerationIsUnchangedWithoutANamespace(t *testing.T) {
	file := compileSwiftSchema(t, "package app\nmessage M { id: string }\n")
	out := string(GenerateTypes(file))
	if strings.Contains(out, "extension ") || strings.Contains(out, "public enum ") && strings.Contains(out, "{}") {
		t.Fatalf("the schema root stays at module level:\n%s", out)
	}
	if !strings.Contains(out, "public final class M:") {
		t.Fatalf("missing the class:\n%s", out)
	}
}

func TestNamespaceFollowsTheDirectory(t *testing.T) {
	for dir, want := range map[string]string{
		".": "", "": "", "common": "Common", "hub/business/v1": "HubBusinessV1", "crm/dashboard/v1": "CrmDashboardV1",
		"my-pkg/v2": "MyPkgV2", "2fa": "N2fa", "type": "Type_",
	} {
		if got := Namespace(dir); got != want {
			t.Errorf("Namespace(%q) = %q, want %q", dir, got, want)
		}
	}
}
