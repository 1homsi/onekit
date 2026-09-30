package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const importSpec = `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets":{"get":{"operationId":"listPets","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"name":{"type":"string"}}}}}}}}}}}`

func TestImportRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(spec, []byte(importSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "schemas")
	if err := run([]string{"import", "--out", out, spec}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	target := filepath.Join(out, "pets.onk")
	info, err := os.Stat(target)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Fatalf("imported file mode: %v %v", info, err)
	}
	if err := os.WriteFile(target, []byte("// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"import", "--out", out, spec}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second import overwrote edits: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "// edited\n" {
		t.Fatalf("edited file changed: %q", data)
	}
	if err := run([]string{"import", "--out", out, "--force", spec}); err != nil {
		t.Fatalf("forced import: %v", err)
	}
}

func TestImportRejectsSchemasThatFailCheck(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	broken := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"components":{"schemas":{"Interface":{"type":"object","properties":{"a":{"type":"string"}}}}},"paths":{"/pets":{"get":{"operationId":"getPet","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Interface"}}}}}}}}}`
	if err := os.WriteFile(spec, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "schemas")
	err := run([]string{"import", "--out", out, spec})
	if err == nil || !strings.Contains(err.Error(), "does not pass onek check") {
		t.Fatalf("want check failure, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "pets.onk")); !os.IsNotExist(statErr) {
		t.Fatalf("broken schema was written: %v", statErr)
	}
}

const importProtoSource = `syntax = "proto3";
package shop.v1;

message Item { string sku = 1; int32 qty = 2; }
message GetItemRequest { string sku = 1; }
message ListItemsRequest { string filter = 1; }
message ListItemsResponse { repeated Item items = 1; }

service Shop {
  rpc GetItem(GetItemRequest) returns (Item) {
    option (google.api.http) = { get: "/v1/items/{sku}" };
  }
  rpc ListItems(ListItemsRequest) returns (ListItemsResponse) {
    option (google.api.http) = { get: "/v1/items" };
  }
}
`

func TestImportProtoProducesAProjectThatBuilds(t *testing.T) {
	dir := t.TempDir()
	proto := filepath.Join(dir, "shop.proto")
	if err := os.WriteFile(proto, []byte(importProtoSource), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "project")
	if err := run([]string{"import", "--out", out, proto}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "shop.v1.onk")); err != nil {
		t.Fatalf("expected the converted schema: %v", err)
	}
	config := "module = \"example.com/shop\"\n\n[generate.go-server]\nout = \"./gen\"\n\n[generate.ts-client]\nout = \"./web\"\n"
	if err := os.WriteFile(filepath.Join(out, "onekit.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"build", "--dir", out}); err != nil {
		t.Fatalf("a schema imported from .proto must build: %v", err)
	}
	for _, name := range []string{"gen/server.gen.go", "web/client.ts"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestImportRejectsAnUnreadableProto(t *testing.T) {
	dir := t.TempDir()
	proto := filepath.Join(dir, "bad.proto")
	if err := os.WriteFile(proto, []byte(`syntax = "proto3"; message M { string a = 1;`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"import", "--out", filepath.Join(dir, "x"), proto}); err == nil || !strings.Contains(err.Error(), "unterminated message M") {
		t.Fatalf("expected a parse error, got %v", err)
	}
}
