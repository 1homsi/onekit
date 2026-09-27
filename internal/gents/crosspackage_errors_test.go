package gents

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const tsCommonErrorsSrc = `
package common

message NotFound @status(404) {
  code: string
}

message Money {
  amount_cents: int64
  currency: string
}
`

const tsCatalogSrc = `
package catalog

message GetItemRequest {
  id: string
}

service CatalogService {
  base_path: "/catalog/v1"

  getItem(GetItemRequest) -> Money | NotFound @get("/items/{id}")
}
`

// A response or error type declared in another package must be decoded through
// that package's alias; a bare decode<Name> call does not compile.
func TestCrossPackageClientCodecsUseAlias(t *testing.T) {
	commonAST, err := onklang.Parse(tsCommonErrorsSrc)
	if err != nil {
		t.Fatalf("parse common: %v", err)
	}
	catalogAST, err := onklang.Parse(tsCatalogSrc)
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{
		{Path: "common/errors.onk", AST: commonAST},
		{Path: "catalog/v1/service.onk", AST: catalogAST},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	dirByMessage := map[*onkir.Message]string{}
	var catalogFile *onkir.File
	for _, f := range pkg.Files {
		dir := filepath.ToSlash(filepath.Dir(f.Path))
		for _, m := range f.Messages {
			dirByMessage[m] = dir
		}
		if dir == "catalog/v1" {
			catalogFile = f
		}
	}
	if catalogFile == nil {
		t.Fatal("catalog/v1 file not compiled")
	}
	resolver := &tsDirResolver{
		currentDir:   "catalog/v1",
		dirByMessage: dirByMessage,
		packages:     map[string]PackageRef{"common": {Alias: "common", ImportPath: "../../common/types.js"}},
	}
	client := string(GenerateClientWithResolver(catalogFile, resolver))
	for _, want := range []string{"common.decodeNotFound(", "common.decodeMoney("} {
		if !strings.Contains(client, want) {
			t.Fatalf("expected client to call %s, got:\n%s", want, client)
		}
	}
	for _, bad := range []string{" decodeNotFound(", "return decodeMoney("} {
		if strings.Contains(client, bad) {
			t.Fatalf("client calls unqualified %q, got:\n%s", bad, client)
		}
	}
}
