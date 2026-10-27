package gents

import (
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onktest"
)

func compileForTest(src string) (*onkir.File, error) {
	ast, err := onktest.Parse(src)
	if err != nil {
		return nil, err
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "fixture.tsp", AST: ast}})
	if err != nil {
		return nil, err
	}
	return pkg.Files[0], nil
}
