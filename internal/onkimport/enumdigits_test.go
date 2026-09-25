package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportHandlesDigitLeadingAndCollidingEnumValues(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Video","version":"1"},"paths":{"/v":{"get":{"operationId":"getVideo","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"quality":{"type":"string","enum":["4k","1080p","a-b","a_b"]}}}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{`V_4K @json("4k")`, `V_1080P @json("1080p")`, `A_B @json("a-b")`, `A_B_2 @json("a_b")`} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "video.onk", AST: ast}}); err != nil {
		t.Fatalf("import does not compile: %v", err)
	}
}
