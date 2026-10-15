package onkcompile

import "testing"

func TestNumericDecoratorsAcceptALeadingPlusEverywhere(t *testing.T) {
	src := `package app
message M {
  a: int32 @range(+1, +5)
  b: int32 @gte(+1)
  c: string @len(+1, +5)
  d: int32 @in(+1, 5)
  e: string[] @max_items(+3)
  f: string @in("+1", "x")
}
message E {}
service S { op(M) -> E @post("/m") }
`
	pkg, err := CompileWithOptions([]Source{{Path: "a.onk", AST: parseOrFatal(t, src)}}, CompileOptions{})
	if err != nil {
		t.Fatalf("a leading plus must be accepted by every numeric decorator: %v", err)
	}
	want := map[string][]string{"a": {"1", "5"}, "b": {"1"}, "c": {"1", "5"}, "d": {"1", "5"}, "e": {"3"}, "f": {"+1", "x"}}
	for _, f := range pkg.Files[0].Messages[0].Fields {
		for _, d := range f.Decorators {
			var got []string
			for _, a := range d.Args {
				got = append(got, a.Value)
			}
			if w, ok := want[f.Name]; ok && len(got) == len(w) {
				for i := range w {
					if got[i] != w[i] {
						t.Errorf("%s @%s args = %v, want %v", f.Name, d.Name, got, w)
						break
					}
				}
			}
		}
	}
}
