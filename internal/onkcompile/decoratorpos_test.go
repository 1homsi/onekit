package onkcompile

import (
	"errors"
	"testing"
)

func TestCompileReportsDecoratorErrorsAtTheDecorator(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, "message M {\n  name: string\n    @len(1, 5)\n    @bogus\n}\n")}})
	var compileErr *Error
	if !errors.As(err, &compileErr) || compileErr.Line != 4 || compileErr.Column != 5 {
		t.Fatalf("want 4:5, got %v", err)
	}
}
