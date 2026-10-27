package gengo

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGoClientRejectsDotSegmentPathParameters(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, `namespace app;

model Req {
  id: string;
}

model Res {
  id: string;
}

model Count {
  n: int32;
}

interface Items {
  @get
  @route("/items/{id}")
  get(...Req): Res;

  @get
  @route("/watch/{id}")
  @Onkt.stream
  watch(...Req): Res;

  @get
  @route("/total/{n}")
  total(...Count): Res;
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
	client, err := GenerateClient(file)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/dots\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "types.gen.go"), string(types))
	writeFile(t, filepath.Join(dir, "validate.gen.go"), string(validation))
	writeFile(t, filepath.Join(dir, "client.gen.go"), string(client))
	writeFile(t, filepath.Join(dir, "dots_test.go"), `package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDotSegments(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	c := NewItemsClient(srv.URL)
	for _, id := range []string{".", ".."} {
		if _, err := c.Get(context.Background(), &Req{Id: id}); err == nil || !strings.Contains(err.Error(), "dot segments") {
			t.Fatalf("Get(%q) must be rejected, got %v", id, err)
		}
		if _, err := c.Watch(context.Background(), &Req{Id: id}); err == nil || !strings.Contains(err.Error(), "dot segments") {
			t.Fatalf("Watch(%q) must be rejected, got %v", id, err)
		}
	}
	if len(paths) != 0 {
		t.Fatalf("a rejected id must not reach the server: %v", paths)
	}
	for _, id := range []string{"a", "a.b", "...", "a/b", ".a"} {
		if _, err := c.Get(context.Background(), &Req{Id: id}); err != nil {
			t.Fatalf("Get(%q): %v", id, err)
		}
	}
	if _, err := c.Total(context.Background(), &Count{N: 0}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, " ") != "/items/a /items/a.b /items/... /items/a%2Fb /items/.a /total/0" {
		t.Fatalf("paths: %v", paths)
	}
}
`)
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
