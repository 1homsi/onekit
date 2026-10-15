package onkcompile

import (
	"strings"
	"testing"
)

func TestDecoratorArgumentsMustBeTheRightKind(t *testing.T) {
	cases := []struct {
		name, field, want string
	}{
		{"quoted range", `n: int32 @range("1", "5")`, `@range argument "1" must be a number, not a string`},
		{"quoted len", `s: string @len("1", "5")`, "must be a number, not a string"},
		{"quoted gte", `n: int32 @gte("3")`, "must be a number, not a string"},
		{"quoted in on int", `n: int32 @in("1", "2")`, "must be a number, not a string"},
		{"number in on string", `s: string @in(1, true)`, "must be a quoted string"},
		{"number pattern", `s: string @pattern(1)`, "must be a quoted string"},
		{"valid", `s: string @in("a", "b") @pattern("^[a-z]+$") @len(1, 5)`, ""},
		{"valid numbers", `n: int32 @range(1, 5) @in(1, 2)`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package app\nmessage M {\n  " + tc.field + "\n}\nmessage E {}\nservice S { op(M) -> E @post(\"/m\") }\n"
			_, err := CompileWithOptions([]Source{{Path: "a.onk", AST: parseOrFatal(t, src)}}, CompileOptions{})
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
