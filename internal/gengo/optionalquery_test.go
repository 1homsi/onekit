package gengo

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

// Reproduces a real bug found migrating voxie: a scalar field that is both
// `optional` (so its Go type is a pointer) and tagged @query generated code
// that assigned/converted a bare value directly into the pointer field,
// which fails to compile.
const optionalQuerySrc = `
package main

message SearchWidgetsRequest {
  status: int32? @query("status")
  owner_id: int64? @query("owner_id")
  q: string @query("q")
}

message SearchWidgetsResponse {
  count: int32
  status_echo: int32? @nullable
  owner_id_echo: int64? @nullable
}

service WidgetService {
  base_path: "/api/v1"

  searchWidgets(SearchWidgetsRequest) -> SearchWidgetsResponse @get("/widgets")
}
`

const optionalQueryHarness = `
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
)

type impl struct{}

func (s *impl) SearchWidgets(ctx context.Context, req *SearchWidgetsRequest) (*SearchWidgetsResponse, error) {
	resp := &SearchWidgetsResponse{Count: 1}
	resp.StatusEcho = req.Status
	resp.OwnerIdEcho = req.OwnerId
	return resp, nil
}

func fail(msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(1)
}

func main() {
	mux := http.NewServeMux()
	if err := RegisterWidgetServiceServer(&impl{}, WithMux(mux)); err != nil {
		fail("register: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	client := NewWidgetServiceClient(srv.URL)

	status := int32(2)
	ownerID := int64(42)
	resp, err := client.SearchWidgets(ctx, &SearchWidgetsRequest{Status: &status, OwnerId: &ownerID, Q: "widg"})
	if err != nil {
		fail("SearchWidgets with optional params set failed: %v", err)
	}
	if resp.StatusEcho == nil || *resp.StatusEcho != 2 {
		fail("expected status echoed back as 2, got %+v", resp.StatusEcho)
	}
	if resp.OwnerIdEcho == nil || *resp.OwnerIdEcho != 42 {
		fail("expected owner_id echoed back as 42, got %+v", resp.OwnerIdEcho)
	}

	resp2, err := client.SearchWidgets(ctx, &SearchWidgetsRequest{Q: "widg"})
	if err != nil {
		fail("SearchWidgets with optional params unset failed: %v", err)
	}
	if resp2.StatusEcho != nil || resp2.OwnerIdEcho != nil {
		fail("expected unset optional query params to stay nil, got %+v", resp2)
	}

	fmt.Println("OK")
}
`

func TestOptionalScalarQueryParamsRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	ast, err := onklang.Parse(optionalQuerySrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]

	typesSrc, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("GenerateTypes error: %v", err)
	}
	validateSrc, err := GenerateValidation(file)
	if err != nil {
		t.Fatalf("GenerateValidation error: %v", err)
	}
	serverSrc, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("GenerateServer error: %v", err)
	}
	clientSrc, err := GenerateClient(file)
	if err != nil {
		t.Fatalf("GenerateClient error: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_gengo_optionalquery_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "types.go"), string(typesSrc))
	if len(validateSrc) > 0 {
		writeFile(t, filepath.Join(dir, "validate.go"), string(validateSrc))
	}
	writeFile(t, filepath.Join(dir, "server.go"), string(serverSrc))
	writeFile(t, filepath.Join(dir, "client.go"), string(clientSrc))
	writeFile(t, filepath.Join(dir, "main.go"), optionalQueryHarness)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}
