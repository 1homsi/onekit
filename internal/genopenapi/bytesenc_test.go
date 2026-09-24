package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIBytesEncodingsUseStandardNames(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message Blob {
  a: bytes @encode(hex)
  b: bytes @encode(base64_raw)
  c: bytes @encode(base64url_raw)
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		"a: type: string pattern: ^[0-9a-fA-F]*$ contentEncoding: base16 x-onekit-encoding: hex",
		"b: type: string pattern: ^[A-Za-z0-9+/]*$ contentEncoding: base64 x-onekit-encoding: base64_raw",
		"c: type: string pattern: ^[A-Za-z0-9_-]*$ contentEncoding: base64url x-onekit-encoding: base64url_raw",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "contentEncoding: base64_raw") {
		t.Fatalf("non-standard contentEncoding emitted:\n%s", doc)
	}
}
