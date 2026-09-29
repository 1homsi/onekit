package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `onek import` used to gate only on onklang.Parse, so specs that parse but
// do not compile were written to disk with exit code 0 and "0 warnings",
// leaving the user to discover the problem via `onek check`. An enum-valued
// query parameter is the mainstream form of this.
const enumQuerySpec = `openapi: 3.0.3
info: { title: T, version: 1.0.0 }
paths:
  /pets:
    get:
      operationId: listPets
      parameters:
        - { name: status, in: query, required: true, schema: { type: string, enum: [available, pending, sold] } }
      responses:
        '200': { content: { application/json: { schema: { type: object, properties: { ok: { type: boolean } } } } } }
`

func runImportSpec(t *testing.T, dir, spec string) error {
	t.Helper()
	_ = filepath.Join(dir, "spec.yaml")
	path := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(path, []byte(spec), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	t.Chdir(dir)

	runErr := runImport([]string{"--out", ".", "--package", "p", "--service", "S", path})
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".onk") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return runErr
}

func TestImportRefusesSchemaThatDoesNotCompile(t *testing.T) {
	err := runImportSpec(t, t.TempDir(), enumQuerySpec)
	if err == nil {
		t.Fatal("expected import to fail for a schema the compiler rejects")
	}
	if !strings.Contains(err.Error(), "does not compile") {
		t.Errorf("error should identify the compile gate, got: %v", err)
	}
	// The diagnostic must name the actual compiler complaint, not just fail.
	if !strings.Contains(err.Error(), "@query") {
		t.Errorf("error should carry the compiler's message, got: %v", err)
	}
}

// A @body on GET parses fine but the compiler rejects it.
func TestImportRefusesBodyOnVerblessVerb(t *testing.T) {
	const spec = `openapi: 3.0.3
info: { title: T, version: 1.0.0 }
paths:
  /_search:
    get:
      operationId: search
      requestBody:
        content:
          application/json:
            schema: { type: object, properties: { query: { type: string } } }
      responses:
        '200': { content: { application/json: { schema: { type: object, properties: { ok: { type: boolean } } } } } }
`
	err := runImportSpec(t, t.TempDir(), spec)
	if err == nil || !strings.Contains(err.Error(), "does not compile") {
		t.Errorf("expected the compile gate to reject @body on GET, got: %v", err)
	}
}

// Two property names that collide only after target-language conversion
// (Name / name) parse cleanly and are rejected by the compiler.
func TestImportRefusesNameCollision(t *testing.T) {
	const spec = `openapi: 3.0.3
info: { title: T, version: 1.0.0 }
paths:
  /x:
    get:
      operationId: getX
      responses:
        '200':
          content:
            application/json:
              schema:
                type: object
                properties:
                  Name: { type: string }
                  name: { type: string }
`
	err := runImportSpec(t, t.TempDir(), spec)
	if err == nil || !strings.Contains(err.Error(), "does not compile") {
		t.Errorf("expected the compile gate to reject colliding names, got: %v", err)
	}
}

// Regression: a spec that produces a valid schema must still import cleanly.
func TestImportStillAcceptsValidSpec(t *testing.T) {
	const spec = `openapi: 3.0.3
info: { title: T, version: 1.0.0 }
paths:
  /pets:
    get:
      operationId: listPets
      parameters:
        - { name: limit, in: query, schema: { type: integer } }
      responses:
        '200':
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  name: { type: string }
`
	if err := runImportSpec(t, t.TempDir(), spec); err != nil {
		t.Fatalf("valid spec must still import: %v", err)
	}
}
