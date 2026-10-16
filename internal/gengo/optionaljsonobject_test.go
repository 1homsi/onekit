package gengo

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOptionalJSONObjectFieldCompilesAndValidates(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, `package app

message M {
  cfg: json? @object
  plain: json @object
}

service S {
  op(M) -> M @post("/m")
}
`)
	types, err := GenerateTypes(file)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := GenerateValidation(file)
	if err != nil {
		t.Fatal(err)
	}
	server, err := GenerateServerWithOptions(file, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := GenerateClient(file)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/oj\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "types.gen.go"), string(types))
	writeFile(t, filepath.Join(dir, "validate.gen.go"), string(validation))
	writeFile(t, filepath.Join(dir, "server.gen.go"), string(server))
	writeFile(t, filepath.Join(dir, "client.gen.go"), string(client))
	writeFile(t, filepath.Join(dir, "oj_test.go"), `package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOptionalObject(t *testing.T) {
	object, array := json.RawMessage("{\"a\":1}"), json.RawMessage("[1]")
	if err := (&M{}).Validate(); err != nil {
		t.Fatalf("an unset optional object is valid: %v", err)
	}
	if err := (&M{Cfg: &object}).Validate(); err != nil {
		t.Fatalf("an object is valid: %v", err)
	}
	err := (&M{Cfg: &array}).Validate()
	if err == nil || !strings.Contains(err.Error(), "cfg must be a JSON object") {
		t.Fatalf("an array in an optional @object field must fail: %v", err)
	}
	var m M
	if err := json.Unmarshal([]byte("{\"cfg\":{\"x\":true}}"), &m); err != nil || m.Cfg == nil || string(*m.Cfg) != "{\"x\":true}" {
		t.Fatalf("round trip: %v %v", err, m.Cfg)
	}
}
`)
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
