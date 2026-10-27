package onkcompat

import "testing"

func TestCompareIgnoresDocOnlyHeaderChangesAndNewOptionalHeaders(t *testing.T) {
	before := `namespace app;

model R {}

/** Headers every API operation takes. */
alias APIHeaders = {
  @header("X-Key") @Onkt.auth("api_key") header_x_key: string;
};

interface API {
  @get
  @route("/r")
  get(...APIHeaders, ...R): R;
}
`
	after := `namespace app;

model R {}

/** Headers every API operation takes. */
alias APIHeaders = {
  @header("X-Key") @Onkt.example("k-123") @Onkt.auth("api_key", "Key") header_x_key: string;
  @header("X-Trace") header_x_trace?: string;
};

interface API {
  @get
  @route("/r")
  get(...APIHeaders, ...R): R;
}
`
	if findings := compareSchemas(t, before, after); len(findings) != 0 {
		t.Fatalf("non-breaking header changes reported: %+v", findings)
	}
	required := `namespace app;

model R {}

/** Headers every API operation takes. */
alias APIHeaders = {
  @header("X-Key") @Onkt.auth("api_key") header_x_key: string;
  @header("X-Tenant") header_x_tenant: string;
};

interface API {
  @get
  @route("/r")
  get(...APIHeaders, ...R): R;
}
`
	if findings := compareSchemas(t, before, required); len(findings) == 0 {
		t.Fatal("new required header not reported")
	}
}
