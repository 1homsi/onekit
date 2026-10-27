package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestGoErrorMessageMayHaveAnErrorField(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onktest.Parse(`namespace app;

model ErrorBody {
  code: string;
  message: string;
  request_id: string;
}

@error
model ApiFailure {
  @statusCode _: 500;
  error: ErrorBody;
}

model Ping {
  validate: string;
}

model Pong {
  ok: boolean;
}

interface S {
  @get
  @route("/ping")
  ping(...Ping): Pong | ApiFailure;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	types, err := GenerateTypesWithResolver(pkg.Files[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := GenerateValidation(pkg.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/err\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"

	app "example.com/err/app"
)

func main() {
	failure := &app.ApiFailure{Error_: &app.ErrorBody{Code: "bad", Message: "nope", RequestId: "r1"}}
	out, err := json.Marshal(failure)
	if err != nil { panic(err) }
	fmt.Println(string(out), failure.GetError().Code, failure.Error())
	var back app.ApiFailure
	if err := json.Unmarshal(out, &back); err != nil { panic(err) }
	fmt.Println(back.Error_.RequestId, (&app.Ping{Validate_: "x"}).GetValidate())
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(out), `{"error":{"code":"bad","message":"nope","request_id":"r1"}}`) || !strings.Contains(string(out), "r1 x") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
