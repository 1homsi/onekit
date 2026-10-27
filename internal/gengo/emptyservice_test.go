package gengo

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestServiceWithoutRPCsGeneratesCompilableGo(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, "namespace app;\n\nmodel R {\n  a: string;\n}\n\ninterface Empty {\n}\n\ninterface Real {\n  @post\n  @route(\"/op\")\n  `op`(...R): R;\n}\n")
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
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/empty\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "types.gen.go"), string(types))
	writeFile(t, filepath.Join(dir, "validate.gen.go"), string(validation))
	writeFile(t, filepath.Join(dir, "server.gen.go"), string(server))
	writeFile(t, filepath.Join(dir, "client.gen.go"), string(client))
	cmd := exec.Command("go", "vet", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	only := compileFixtureSource(t, `namespace app;

interface Empty {
}
`)
	if out, err := GenerateServerWithOptions(only, nil, Options{}); err != nil || out != nil {
		t.Fatalf("a file whose only service is empty has no server: %q %v", out, err)
	}
	if out, err := GenerateClient(only); err != nil || out != nil {
		t.Fatalf("a file whose only service is empty has no client: %q %v", out, err)
	}
}
