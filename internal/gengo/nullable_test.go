package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const nullableSchema = `package app

message Item { name: string }
message Patch {
  id: int32
  folder_id: int64? @nullable @encode("number")
  note: string? @nullable
  item: Item? @nullable
}

service Patches {
  patch(Patch) -> Patch @patch("/patch")
}
`

const nullableHarness = `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	app "example.com/nullable/app"
)

type impl struct{}

func (impl) Patch(ctx context.Context, req *app.Patch) (*app.Patch, error) { return req, nil }

func marshal(p *app.Patch) string {
	b, err := json.Marshal(p)
	if err != nil { panic(err) }
	return string(b)
}

func main() {
	folder := int64(7)
	note := "n"
	cases := map[string]*app.Patch{
		"{\"id\":1}":                                     {Id: 1},
		"{\"folder_id\":null,\"id\":1}":                  {Id: 1, FolderIdNull: true},
		"{\"folder_id\":7,\"id\":1,\"note\":\"n\"}":      {Id: 1, FolderId: &folder, Note: &note},
		"{\"folder_id\":7,\"id\":1}":                     {Id: 1, FolderId: &folder, FolderIdNull: true},
		"{\"id\":1,\"item\":null,\"note\":null}":         {Id: 1, NoteNull: true, ItemNull: true},
	}
	for want, value := range cases {
		var normalized map[string]any
		if err := json.Unmarshal([]byte(marshal(value)), &normalized); err != nil { panic(err) }
		canonical, _ := json.Marshal(normalized)
		var expected map[string]any
		if err := json.Unmarshal([]byte(want), &expected); err != nil { panic(err) }
		expectedCanonical, _ := json.Marshal(expected)
		if string(canonical) != string(expectedCanonical) {
			panic("marshal: " + string(canonical) + " want " + string(expectedCanonical))
		}
	}

	var decoded app.Patch
	if err := json.Unmarshal([]byte("{\"id\":1,\"folder_id\":null,\"note\":\"x\"}"), &decoded); err != nil { panic(err) }
	if !decoded.FolderIdNull || decoded.FolderId != nil || decoded.NoteNull || decoded.Note == nil || *decoded.Note != "x" || decoded.ItemNull {
		panic(fmt.Sprintf("decode tri-state: %+v", decoded))
	}
	var absent app.Patch
	if err := json.Unmarshal([]byte("{\"id\":1}"), &absent); err != nil { panic(err) }
	if absent.FolderIdNull || absent.NoteNull || absent.ItemNull { panic("absent must not read as null") }

	mux := http.NewServeMux()
	if err := app.RegisterPatchesServer(mux, impl{}); err != nil { panic(err) }
	server := httptest.NewServer(mux)
	defer server.Close()
	client := app.NewPatchesClient(server.URL)
	got, err := client.Patch(context.Background(), &app.Patch{Id: 2, FolderIdNull: true})
	if err != nil { panic(err) }
	if !got.FolderIdNull || got.FolderId != nil || got.NoteNull {
		panic(fmt.Sprintf("round trip: %+v", got))
	}
	fmt.Println("OK")
}
`

func TestGoNullableFieldsRoundTripNullAbsentAndValue(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, nullableSchema)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nullable\n\ngo 1.26\n")
	for name, generate := range map[string]func() ([]byte, error){
		"types.go":    func() ([]byte, error) { return GenerateTypesWithResolver(file, nil) },
		"validate.go": func() ([]byte, error) { return GenerateValidationWithResolver(file, nil) },
		"server.go":   func() ([]byte, error) { return GenerateServerWithResolver(file, nil) },
		"client.go":   func() ([]byte, error) { return GenerateClientWithResolver(file, nil) },
	} {
		out, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "app", name), string(out))
	}
	writeFile(t, filepath.Join(dir, "main.go"), nullableHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
