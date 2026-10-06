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
		{"optional string accepted", `note: string? @nullable`, ""},
		{"optional message accepted", `item: Item? @nullable`, ""},
		{"number-encoded int64 accepted", `n: int64? @nullable @encode("number")`, ""},
		{"non-optional rejected", `note: string @nullable`, "requires the ? marker"},
		{"repeated rejected", `notes: string[] @nullable`, "requires the ? marker"},
		{"bytes rejected", `data: bytes? @nullable`, "bytes"},
		{"string-encoded int64 rejected", `n: int64? @nullable`, "numeric 64-bit encoding"},
		{"required rejected", `note: string? @nullable @required`, "@required"},
		{"query rejected", `note: string? @nullable @query`, "@query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, "package api\nmessage Item { name: string }\nmessage Patch {\n  "+tc.field+"\n}\n")
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
