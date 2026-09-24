package onkcompat

import "testing"

func TestCompareIgnoresDocOnlyHeaderChangesAndNewOptionalHeaders(t *testing.T) {
	before := `package app
message R {}
service API {
  headers: { "X-Key": string @required @auth("api_key") }
  get(R) -> R @get("/r")
}`
	after := `package app
message R {}
service API {
  headers: {
    "X-Key": string @required @auth("api_key") @example("k-123") @auth_scheme_name("Key")
    "X-Trace": string
  }
  get(R) -> R @get("/r")
}`
	if findings := compareSchemas(t, before, after); len(findings) != 0 {
		t.Fatalf("non-breaking header changes reported: %+v", findings)
	}
	required := `package app
message R {}
service API {
  headers: {
    "X-Key": string @required @auth("api_key")
    "X-Tenant": string @required
  }
  get(R) -> R @get("/r")
}`
	if findings := compareSchemas(t, before, required); len(findings) == 0 {
		t.Fatal("new required header not reported")
	}
}
