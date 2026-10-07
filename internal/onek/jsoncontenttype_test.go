package onek

import (
	"path/filepath"
	"strings"
	"testing"
)

const charsetJSON = "application/json; charset=utf-8"

func TestGoServerJSONContentTypeIsConfigurable(t *testing.T) {
	dir, err := buildFiltered(t, "[generate.go-server]\nout = \"gen/go\"\njson_content_type = \""+charsetJSON+"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	server := read(t, filepath.Join(dir, "gen", "go", "server.gen.go"))
	if !strings.Contains(server, `Header().Set("Content-Type", "`+charsetJSON+`")`) || strings.Contains(server, `"Content-Type", "application/json")`) {
		t.Fatalf("the server must write the configured content type:\n%s", server)
	}

	dir, err = buildFiltered(t, "[generate.go-server]\nout = \"gen/go\"\nruntime = \"shared\"\njson_content_type = \""+charsetJSON+"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	runtime := read(t, filepath.Join(dir, "gen", "go", "onekitrt", "runtime.gen.go"))
	if !strings.Contains(runtime, `"Content-Type", "`+charsetJSON+`"`) || strings.Contains(runtime, `"Content-Type", "application/json")`) {
		t.Fatalf("the shared runtime must write the configured content type:\n%s", runtime)
	}
}

func TestGoServerJSONContentTypeDefaultsAndValidates(t *testing.T) {
	dir, err := buildFiltered(t, "[generate.go-server]\nout = \"gen/go\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if server := read(t, filepath.Join(dir, "gen", "go", "server.gen.go")); !strings.Contains(server, `"Content-Type", "application/json")`) {
		t.Fatalf("the default stays application/json:\n%s", server)
	}
	for _, bad := range []string{"text/plain", "application/json\\r\\nX: y", "nonsense"} {
		if _, err := buildFiltered(t, "[generate.go-server]\nout = \"gen/go\"\njson_content_type = \""+bad+"\"\n"); err == nil || !strings.Contains(err.Error(), "json_content_type") {
			t.Errorf("%q must be rejected, got %v", bad, err)
		}
	}
}
