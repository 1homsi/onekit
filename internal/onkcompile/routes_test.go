package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsRoutesTheRouterCannotTellApart(t *testing.T) {
	tests := []struct{ name, schema string }{
		{"renamed path parameter", `
message R { id: string  user_id: string }
service API {
  a(R) -> R @get("/users/{id}")
  b(R) -> R @get("/users/{user_id}")
}
`},
		{"websocket over get", `
message R { id: string }
service API {
  a(R) -> R @get("/live")
  b(R) -> R @ws("/live")
}
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, tt.schema)}})
			if err == nil || !strings.Contains(err.Error(), "duplicate HTTP route") {
				t.Fatalf("want duplicate route error, got %v", err)
			}
		})
	}
}
