package onek

import "testing"

func TestGoTimestampOneofVariantCompiles(t *testing.T) {
	buildGoSchema(t, `namespace check;

union MarkV {
  at: utcDateTime,
  label: string,
}

model Mark {
  v: MarkV;
}
`, "")
}
