package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsRoutesTheRouterCannotTellApart(t *testing.T) {
	tests := []struct{ name, schema string }{
		{"renamed path parameter", `
model R {
  id: string;
  user_id: string;
}

interface API {
  @get
  @route("/users/{id}")
  a(...R): R;

  @get
  @route("/users/{user_id}")
  b(...R): R;
}
`},
		{"websocket over get", `
model R {
  id: string;
}

interface API {
  @get
  @route("/live")
  a(...R): R;

  @Onkt.ws
  @route("/live")
  b(...R): R;
}
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, tt.schema)}})
			if err == nil || !strings.Contains(err.Error(), "duplicate HTTP route") {
				t.Fatalf("want duplicate route error, got %v", err)
			}
		})
	}
}
