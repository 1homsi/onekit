package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const nullableResponseSchema = `package app

message App { id: int64 @encode("number") }
message Folder {
  id: int64 @encode("number")
  parent_id: int64? @nullable @encode("number")
  app: App? @nullable
  note: string?
}
message Shared {
  id: int64 @encode("number")
  parent_id: int64? @nullable @encode("number")
}
message Empty {}
service Folders {
  base_path: "/v1"
  list(Empty) -> Folder @get("/folders")
  get(Shared) -> Shared @get("/shared/{id}")
  save(Shared) -> Shared @post("/shared")
}
`

func TestGoWritesNullForUnsetNullableFieldsOfResponseOnlyMessages(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, nullableResponseSchema)
	types, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nullable\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"
	"os"

	app "example.com/nullable/app"
)

func main() {
	id := int64(4)
	out, _ := json.Marshal(&app.Folder{Id: 1})
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil { panic(err) }
	for _, key := range []string{"parent_id", "app"} {
		if v, ok := decoded[key]; !ok || v != nil { fmt.Println("unset nullable fields must be written as null:", string(out)); os.Exit(1) }
	}
	out, _ = json.Marshal(&app.Folder{Id: 1, ParentId: &id})
	if !contains(string(out), `+"`"+`"parent_id":4`+"`"+`) { fmt.Println("a set value must be written:", string(out)); os.Exit(1) }
	out, _ = json.Marshal(&app.Shared{Id: 1})
	if contains(string(out), "parent_id") { fmt.Println("a shared message keeps the omitted form:", string(out)); os.Exit(1) }
	out, _ = json.Marshal(&app.Shared{Id: 1, ParentIdNull: true})
	if !contains(string(out), `+"`"+`"parent_id":null`+"`"+`) { fmt.Println("the Null flag still writes null:", string(out)); os.Exit(1) }
	fmt.Println("OK")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub { return true }
	}
	return false
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
