package onkcompile

import (
	"strings"
	"testing"
)

func TestNullableFieldValidation(t *testing.T) {
	cases := []struct {
		name  string
		field string
		want  string
	}{
		{"optional string accepted", `note?: string | null;`, ""},
		{"optional message accepted", `item?: Item | null;`, ""},
		{"number-encoded int64 accepted", `n?: int64 | null;`, ""},
		{"non-optional rejected", `note: string | null;`, "requires the ? marker"},
		{"repeated rejected", `notes: string[] | null;`, "requires the ? marker"},
		{"bytes rejected", `data?: bytes | null;`, "bytes"},
		{"string-encoded int64 rejected", `@Onkt.encode("string") n?: int64 | null;`, "numeric 64-bit encoding"},
		{"required rejected", `@Onkt.required note?: string | null;`, "@required"},
		{"query rejected", `@query note?: string | null;`, "@query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, "namespace api;\nmodel Item { name: string; }\nmodel Patch {\n  "+tc.field+"\n}\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
