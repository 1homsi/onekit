package gents

import (
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func compileForTest(src string) (*onkir.File, error) {
	ast, err := onklang.Parse(src)
	if err != nil {
		return nil, err
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "fixture.onk", AST: ast}})
	if err != nil {
		return nil, err
	}
	return pkg.Files[0], nil
}
