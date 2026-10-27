package onkcompile

import "testing"

func TestNumericDecoratorsAcceptALeadingPlusEverywhere(t *testing.T) {
	src := "namespace app;\n\nmodel M {\n  @Onkt.range(1, 5)\n  a: int32;\n  @minValue(1)\n  b: int32;\n  @minLength(1)\n  @maxLength(5)\n  c: string;\n  @Onkt.values(1, 5)\n  d: int32;\n  @maxItems(3)\n  e: string[];\n  f: \"+1\" | \"x\";\n}\n\nmodel E {}\n\ninterface S {\n  @post\n  @route(\"/m\")\n  `op`(...M): E;\n}\n"
	pkg, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: parseOrFatal(t, src)}}, CompileOptions{})
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
