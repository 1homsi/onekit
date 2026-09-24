package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIIncludesServiceAndEnumDocs(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
/// Order lifecycle.
enum Status {
  /// Waiting for payment.
  PENDING
  PAID
}
message Order { status: Status }
/// Manages orders.
service Orders { get(Order) -> Order @get("/orders") }
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		"tags: - name: Orders description: Manages orders.",
		"description: |- Order lifecycle. - `PENDING`: Waiting for payment.",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
