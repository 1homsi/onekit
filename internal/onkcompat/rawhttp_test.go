package onkcompat

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
)

// rawFindings compares two versions of one operation, each given as its
// TypeSpec declaration inside interface API.
func rawFindings(t *testing.T, oldOperation, newOperation string) string {
	t.Helper()
	build := func(operation string) *onkir.Package {
		pkg, err := onkcompile.Compile([]onkcompile.Source{*compile(t, "namespace app;\nmodel Req { slug: string; }\nmodel Out { ok: boolean; }\nmodel Empty {}\ninterface API {\n"+operation+"\n}")})
		if err != nil {
			t.Fatal(err)
		}
		return pkg
	}
	var lines []string
	for _, f := range Compare(build(oldOperation), build(newOperation)) {
		lines = append(lines, f.Path+": "+f.Message)
	}
	return strings.Join(lines, "\n")
}

func TestCompareTracksRawRoutes(t *testing.T) {
	base := `  @get @route("/apps/{slug}/bundle.js") @Onkt.http(#{ contentType: "application/javascript" }) @Onkt.guard("app/use/:slug") bundle(...Req): Empty;`
	cases := []struct {
		name, changed, want string
	}{
		{"unchanged", base, ""},
		{"path", strings.Replace(base, "bundle.js", "bundle.mjs", 1), "route"},
		{"method", strings.Replace(base, "@get ", "@post ", 1), "route"},
		{"guard", strings.Replace(base, "app/use/:slug", "app/edit/:slug", 1), "guard"},
		{"content type", strings.Replace(base, "application/javascript", "text/plain", 1), "raw HTTP handler"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name != "unchanged" && tc.changed == base {
				t.Fatal("the edit did not apply")
			}
			got := rawFindings(t, base, tc.changed)
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
		})
	}
	t.Run("request and response kind", func(t *testing.T) {
		old := `  @post @route("/apps/{slug}/up") @Onkt.http up(...Req): Empty;`
		got := rawFindings(t, old, `  @post @route("/apps/{slug}/up") @Onkt.http(#{ request: "multipart" }) up(...Req): Empty;`)
		if !strings.Contains(got, "raw HTTP request kind") {
			t.Fatalf("a changed request kind must be reported:\n%s", got)
		}
		got = rawFindings(t, old, `  @post @route("/apps/{slug}/up") @Onkt.http(#{ response: "json" }) up(...Req): Out;`)
		if !strings.Contains(got, "raw HTTP response kind") {
			t.Fatalf("a changed response kind must be reported:\n%s", got)
		}
	})
	t.Run("any", func(t *testing.T) {
		got := rawFindings(t, `  @Onkt.method("ANY") @route("/apps/{slug}/p") @Onkt.http p(...Req): Empty;`, `  @post @route("/apps/{slug}/p") @Onkt.http p(...Req): Empty;`)
		if !strings.Contains(got, "route") {
			t.Fatalf("narrowing @any to one method must be reported:\n%s", got)
		}
	})
}
