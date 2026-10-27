package genpy

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestPythonStreamAndSocketPathParamsFormatBooleans(t *testing.T) {
	ast, err := onktest.Parse(`namespace app;

model R {
  live: boolean;
}

model E {
  v: string;
}

interface S {
  @get
  @route("/w/{live}")
  @Onkt.stream
  watch(...R): E;

  @Onkt.ws
  @route("/c/{live}")
  chat(...R): E;
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
	if strings.Contains(out, "quote(str(req.live)") || strings.Count(out, `("true" if req.live else "false")`) != 2 {
		t.Fatalf("bool path params not formatted as true/false:\n%s", out)
	}
}
