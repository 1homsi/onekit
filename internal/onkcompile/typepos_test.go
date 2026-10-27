package onkcompile

import (
	"errors"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

// An unresolved type is reported at the type reference's span. TypeSpec
// resolves every reference itself, so these come from documents.
func TestCompileReportsUnresolvedTypesAtTheType(t *testing.T) {
	q, dec := onktest.Q, onktest.Dec
	span := func(line, col int) onkschema.Span { return onkschema.Span{Line: line, Col: col} }
	rpc := func(request, response string, errorTypes ...string) *onkschema.RPCDecl {
		r := onktest.RPC("a", request, response, dec("get", q("/a")))
		r.ErrorTypes = errorTypes
		r.Line, r.Col = 3, 3
		return r
	}
	requestMissing := rpc("Missing", "R")
	requestMissing.RequestSpan = span(3, 5)
	responseMissing := rpc("R", "Nope")
	responseMissing.ResponseSpan = span(3, 11)
	errorMissing := rpc("R", "R", "Gone")
	errorMissing.ErrorSpans = []onkschema.Span{span(3, 15)}
	mapValue := onktest.Field("values", "map<string, Missing>")
	mapValue.Type.MapVal.Span = span(2, 23)
	field := onktest.Field("value", "Missing")
	field.Type.Span = span(2, 10)

	tests := []struct {
		file      *onkschema.File
		line, col int
	}{
		{onktest.File("", onktest.Message("R"), onktest.Service("S", "", requestMissing)), 3, 5},
		{onktest.File("", onktest.Message("R"), onktest.Service("S", "", responseMissing)), 3, 11},
		{onktest.File("", onktest.Message("R"), onktest.Service("S", "", errorMissing)), 3, 15},
		{onktest.File("", onktest.Message("M", mapValue)), 2, 23},
		{onktest.File("", onktest.Message("M", field)), 2, 10},
	}
	for i, tt := range tests {
		_, err := Compile([]Source{{Path: "api.tsp", AST: tt.file}})
		var compileErr *Error
		if !errors.As(err, &compileErr) || compileErr.Line != tt.line || compileErr.Column != tt.col {
			t.Fatalf("case %d: want %d:%d, got %v", i, tt.line, tt.col, err)
		}
	}
}
