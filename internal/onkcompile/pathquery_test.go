package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onktest"
)

// TypeSpec's HTTP library refuses a property that is both a path and a query
// parameter; the compiler refuses such a document from any frontend.
func TestCompileRejectsFieldBoundAsPathAndQuery(t *testing.T) {
	q, dec := onktest.Q, onktest.Dec
	file := onktest.File("",
		onktest.Message("R", onktest.Field("id", "string", dec("query"))),
		onktest.Service("API", "", onktest.RPC("get", "R", "R", dec("get", q("/u/{id}")))),
	)
	_, err := Compile([]Source{{Path: "api.tsp", AST: file}})
	if err == nil || !strings.Contains(err.Error(), `request field "id" cannot be both a path and query binding`) {
		t.Fatalf("want path/query conflict, got %v", err)
	}
}
