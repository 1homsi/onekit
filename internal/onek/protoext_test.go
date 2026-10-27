package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const protoNamedOnkSchema = `package app

message Item { id: string }

service Items {
  base_path: "/v1"
  get(Item) -> Item @get("/items/{id}")
}
`

const realProtobuf = `syntax = "proto3";
package other;
message Thing { string id = 1; int32 n = 2; }
`

func protoProject(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/x\"\n"+config+"\n[generate.go-server]\nout = \"gen\"\n")
	writeTestFile(t, filepath.Join(dir, "api", "items.proto"), protoNamedOnkSchema)
	return dir
}

func TestLegacyProtoSchemasArePointedAtMigrate(t *testing.T) {
	onkSchemasAccepted = false
	t.Cleanup(func() { onkSchemasAccepted = true })
	dir := protoProject(t, "schema_extensions = [\".onk\", \".proto\"]\n")
	err := Check(dir)
	if err == nil || !strings.Contains(err.Error(), "items.proto") || !strings.Contains(err.Error(), "onkt migrate") {
		t.Fatalf("err = %v, want items.proto pointed at onkt migrate", err)
	}
}

func TestMigrateConvertsLegacyProtoSchemas(t *testing.T) {
	dir := protoProject(t, "schema_extensions = [\".onk\", \".proto\"]\n")
	writeTestFile(t, filepath.Join(dir, "vendor-api", "thing.proto"), realProtobuf)
	if _, err := Migrate(dir, MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api", "items.tsp")); err != nil {
		t.Fatalf("items.proto was not converted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api", "items.proto")); !os.IsNotExist(err) {
		t.Fatal("the converted .proto schema must be removed")
	}
	if got := read(t, filepath.Join(dir, "vendor-api", "thing.proto")); got != realProtobuf {
		t.Fatal("a real protobuf file must be left alone")
	}
	if _, err := os.Stat(filepath.Join(dir, "vendor-api", "thing.tsp")); err == nil {
		t.Fatal("a real protobuf file must not be converted")
	}
}

func TestProtoFilesAreIgnoredByDefault(t *testing.T) {
	dir := protoProject(t, "")
	if err := Build(dir); err == nil || !strings.Contains(err.Error(), "no schema files") {
		t.Fatalf("without schema_extensions a .proto file is not a schema, got %v", err)
	}
}

func TestSchemaExtensionsRejectsUnknownValues(t *testing.T) {
	dir := protoProject(t, "schema_extensions = [\".yaml\"]\n")
	if err := Check(dir); err == nil || !strings.Contains(err.Error(), "schema_extensions") {
		t.Fatalf("expected a schema_extensions error, got %v", err)
	}
}
