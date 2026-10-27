package gents

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onktest"
)

const tsCommonErrorsSrc = `namespace common;

@error
model NotFound {
  @statusCode _: 404;
  code: string;
}

model Money {
  @encode(string)
  amount_cents: int64;
  currency: string;
}
`

const tsCatalogSrc = `using common;


namespace catalog;

model GetItemRequest {
  id: string;
}

@route("/catalog/v1")
interface CatalogService {
  @get
  @route("/items/{id}")
  getItem(...GetItemRequest): Money | NotFound;
}
`

// A response or error type declared in another package must be decoded through
// that package's alias; a bare decode<Name> call does not compile.
func TestCrossPackageClientCodecsUseAlias(t *testing.T) {
	// The files compile together, as one TypeSpec project.
	var sources []onkcompile.Source
	for _, file := range onktest.Files(t, map[string]string{"common/errors.tsp": tsCommonErrorsSrc, "catalog/v1/service.tsp": tsCatalogSrc}) {
		sources = append(sources, onkcompile.Source{Path: file.Path, AST: &file.File})
	}
	pkg, err := onkcompile.Compile(sources)
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
