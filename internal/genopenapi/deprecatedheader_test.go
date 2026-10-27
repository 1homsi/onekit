package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIMarksBareDeprecatedHeaders(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model R {}

/** Headers every S operation takes. */
alias SHeaders = {
  #deprecated ""
  @header("X-Old") header_x_old?: string;
  #deprecated "use X-New"
  @header("X-Older") header_x_older?: string;
};

interface S {
  @get
  @route("/r")
  get(...SHeaders, ...R): R;
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{"name: X-Old in: header required: false deprecated: true", "name: X-Older in: header required: false deprecated: true"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
