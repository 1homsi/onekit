package genpy

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestPythonClientKeepsBuiltinsWhenErrorsShadowThem(t *testing.T) {
	ast, err := onktest.Parse(`namespace app;

model Msg {
  text: string;
}

@error
model TimeoutError {
  @statusCode _: 504;
  message: string;
}

@error
model ConnectionError {
  @statusCode _: 502;
  message: string;
}

interface Chat {
  @post
  @route("/send")
  send(...Msg): Msg | TimeoutError | ConnectionError;

  @Onkt.ws
  @route("/chat")
  stream(...Msg): Msg;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	out := string(GenerateClient(pkg.Files[0], "models"))
	for _, bad := range []string{"(ConnectionError):", "(TimeoutError):", "except TimeoutError"} {
		if strings.Contains(out, bad) {
			t.Fatalf("runtime refers to a name the schema shadows: %q", bad)
		}
	}
	if !strings.Contains(out, "import builtins as _builtins") {
		t.Fatal("builtins import missing")
	}
}
