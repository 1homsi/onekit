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

func TestProtoFilesHoldingOnekitSyntaxAreSchemasWhenEnabled(t *testing.T) {
	dir := protoProject(t, "schema_extensions = [\".onk\", \".proto\"]\n")
	writeTestFile(t, filepath.Join(dir, "vendor-api", "thing.proto"), realProtobuf)
	if err := Build(dir); err != nil {
		t.Fatalf("a .proto file written in onekit syntax must build: %v", err)
	}
	server := read(t, filepath.Join(dir, "gen", "api", "server.gen.go"))
	if !strings.Contains(server, "ItemsServer") {
		t.Fatalf("the service in items.proto must be generated:\n%s", server)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "vendor-api")); err == nil {
		t.Fatal("a real protobuf file must be skipped, not parsed")
	}
}

func TestProtoFilesAreIgnoredByDefault(t *testing.T) {
	dir := protoProject(t, "")
	if err := Build(dir); err == nil || !strings.Contains(err.Error(), "no .onk files") {
		t.Fatalf("without schema_extensions a .proto file is not a schema, got %v", err)
	}
}

func TestSchemaExtensionsRejectsUnknownValues(t *testing.T) {
	dir := protoProject(t, "schema_extensions = [\".yaml\"]\n")
	if err := Check(dir); err == nil || !strings.Contains(err.Error(), "schema_extensions") {
		t.Fatalf("expected a schema_extensions error, got %v", err)
	}
}

func TestProtoNamedSchemasAreFormattedAndSeenByTheLanguageTools(t *testing.T) {
	dir := protoProject(t, "schema_extensions = [\".onk\", \".proto\"]\n")
	messy := strings.Replace(protoNamedOnkSchema, "message Item { id: string }", "message   Item {   id:string   }", 1)
	writeTestFile(t, filepath.Join(dir, "api", "items.proto"), messy)
	if err := Format(dir, true); err == nil {
		t.Fatal("fmt --check must flag the unformatted .proto schema")
	}
	if err := Format(dir, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "api", "items.proto")); got == messy || !strings.Contains(got, "id: string") {
		t.Fatalf("fmt must rewrite the .proto schema:\n%s", got)
	}
	if err := Format(dir, true); err != nil {
		t.Fatalf("a formatted .proto schema must pass fmt --check: %v", err)
	}
	snapshot, err := AnalyzeLanguage(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range snapshot.Files {
		if strings.HasSuffix(file.Path, "items.proto") {
			found = true
		}
	}
	if !found || len(snapshot.Diagnostics) > 0 {
		t.Fatalf("the language tools must include items.proto without diagnostics: %+v %+v", snapshot.Files, snapshot.Diagnostics)
	}
}
