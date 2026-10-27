package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

// TypeSpec's @discriminated union with envelope "none" already refuses a
// scalar variant; the compiler refuses the same document from any frontend.
func TestFlattenedOneofRequiresMessageVariants(t *testing.T) {
	file := onktest.File("app",
		onktest.Message("Email", onktest.Field("address", "string")),
		onktest.Message("Contact", onktest.Oneof("via", []onkschema.Arg{onktest.Named("flatten", onktest.N("true"))},
			onktest.Variant("email", "Email"),
			onktest.Variant("phone", "string"),
		)),
	)
	_, err := Compile([]Source{{Path: "a.tsp", AST: file}})
	if err == nil || !strings.Contains(err.Error(), `oneof variant "phone" must be a message to use flatten: true`) {
		t.Fatalf("err = %v", err)
	}
}
