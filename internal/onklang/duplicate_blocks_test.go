package onklang

import (
	"strings"
	"testing"
)

func TestParseRejectsDuplicateServiceBlocks(t *testing.T) {
	tests := map[string]string{
		"duplicate base_path in service API": `service API { base_path: "/a" base_path: "/b" }`,
		"duplicate headers block in service API": `service API {
  headers: { "X-A": string @required }
  headers: { "X-B": string }
}`,
		"duplicate headers block in rpc get": `service API {
  get(R) -> R @get("/x") {
    headers: { "X-A": string }
    headers: { "X-B": string }
  }
}`,
	}
	for want, src := range tests {
		if _, err := Parse(src); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
}
