package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRequiresAuthorizationHeaderForHTTPAuth(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model R {}

/** Headers every API operation takes. */
alias APIHeaders = {
  @header("X-Token") @Onkt.auth("bearer") header_x_token: string;
};

interface API {
  @get
  @route("/x")
  a(...APIHeaders, ...R): R;
}
`)}})
	if err == nil || !strings.Contains(err.Error(), "must be declared on the Authorization header") {
		t.Fatalf("want Authorization header error, got %v", err)
	}
	if _, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model R {}

/** Headers every API operation takes. */
alias APIHeaders = {
  @header("authorization") @Onkt.auth("basic") header_authorization: string;
  @header("X-Key") @Onkt.auth("api_key") header_x_key: string;
};

interface API {
  @get
  @route("/x")
  a(...APIHeaders, ...R): R;
}
`)}}); err != nil {
		t.Fatalf("valid auth headers rejected: %v", err)
	}
}
