package onkcompile

import (
	"errors"
	"testing"

	"github.com/1homsi/onekit/internal/onktest"
)

// A diagnostic about a decorator points at the decorator, at the position
// its frontend recorded.
func TestCompileReportsDecoratorErrorsAtTheDecorator(t *testing.T) {
	length := onktest.Dec("len", onktest.N("1"), onktest.N("5"))
	length.Line, length.Col = 3, 5
	bogus := onktest.Dec("bogus")
	bogus.Line, bogus.Col = 4, 5
	name := onktest.Field("name", "string", length, bogus)
	name.Line, name.Col = 2, 3
	_, err := Compile([]Source{{Path: "api.tsp", AST: onktest.File("", onktest.Message("M", name))}})
	var compileErr *Error
	if !errors.As(err, &compileErr) || compileErr.Line != 4 || compileErr.Column != 5 {
		t.Fatalf("want 4:5, got %v", err)
	}
}
