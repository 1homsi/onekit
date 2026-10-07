package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const jsonObjectSchema = `package app

message Doc {
  id: string
  settings: json @object
  any: json
}
`

func TestGoJSONObjectKeepsBytesAndRejectsNonObjects(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, jsonObjectSchema)
	types, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := GenerateValidationWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/obj\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"
	"os"

	app "example.com/obj/app"
)

func main() {
	in := []byte(`+"`"+`{"id":"1","settings":{"z":1,"a":{"y":2,"b":3}},"any":[1,2]}`+"`"+`)
	var doc app.Doc
	if err := json.Unmarshal(in, &doc); err != nil { panic(err) }
	if err := doc.Validate(); err != nil { fmt.Println("valid object rejected:", err); os.Exit(1) }
	out, err := json.Marshal(&doc)
	if err != nil { panic(err) }
	if string(doc.Settings) != `+"`"+`{"z":1,"a":{"y":2,"b":3}}`+"`"+` { fmt.Println("bytes changed:", string(doc.Settings)); os.Exit(1) }
	if !contains(string(out), `+"`"+`"settings":{"z":1,"a":{"y":2,"b":3}}`+"`"+`) { fmt.Println("key order changed:", string(out)); os.Exit(1) }
	for _, bad := range []string{`+"`"+`{"settings":[1]}`+"`"+`, `+"`"+`{"settings":"x"}`+"`"+`, `+"`"+`{"settings":5}`+"`"+`} {
		var d app.Doc
		if err := json.Unmarshal([]byte(bad), &d); err != nil { panic(err) }
		if d.Validate() == nil { fmt.Println("accepted", bad); os.Exit(1) }
	}
	for _, good := range []string{`+"`"+`{}`+"`"+`, `+"`"+`{"settings":null}`+"`"+`, `+"`"+`{"settings":{}}`+"`"+`} {
		var d app.Doc
		if err := json.Unmarshal([]byte(good), &d); err != nil { panic(err) }
		if err := d.Validate(); err != nil { fmt.Println("rejected", good, err); os.Exit(1) }
	}
	fmt.Println("OK")
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (func() bool { for i := 0; i+len(sub) <= len(s); i++ { if s[i:i+len(sub)] == sub { return true } }; return false })() }
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
