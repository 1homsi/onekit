package onek

import "testing"

func TestGoTimestampOneofVariantCompiles(t *testing.T) {
	buildGoSchema(t, `
package check

message Mark {
  v: oneof { at: timestamp  label: string }
}
`, "")
}
