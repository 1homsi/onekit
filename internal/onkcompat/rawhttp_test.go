package onkcompat

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
)

func rawFindings(t *testing.T, oldRoute, newRoute string) string {
	t.Helper()
	build := func(route string) *onkir.Package {
		pkg, err := onkcompile.Compile([]onkcompile.Source{*compile(t, "package app\nmessage Req { slug: string }\nmessage Out { ok: bool }\nmessage Empty {}\nservice API {\n"+route+"\n}")})
		if err != nil {
			t.Fatal(err)
		}
		return pkg
	}
	var lines []string
	for _, f := range Compare(build(oldRoute), build(newRoute)) {
		lines = append(lines, f.Path+": "+f.Message)
	}
	return strings.Join(lines, "\n")
}

func TestCompareTracksRawRoutes(t *testing.T) {
	base := `  bundle(Req) -> Empty @get("/apps/{slug}/bundle.js") @http("application/javascript") @guard("app/use/:slug")`
	cases := []struct {
		name, changed, want string
	}{
		{"unchanged", base, ""},
		{"path", strings.Replace(base, "bundle.js", "bundle.mjs", 1), "route"},
		{"method", strings.Replace(base, "@get(", "@post(", 1), "route"},
		{"guard", strings.Replace(base, "app/use/:slug", "app/edit/:slug", 1), "guard"},
		{"content type", strings.Replace(base, "application/javascript", "text/plain", 1), "raw HTTP handler"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rawFindings(t, base, tc.changed)
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
		})
	}
	t.Run("request and response kind", func(t *testing.T) {
		old := `  up(Req) -> Empty @post("/apps/{slug}/up") @http`
		got := rawFindings(t, old, `  up(Req) -> Empty @post("/apps/{slug}/up") @http(request: "multipart")`)
		if !strings.Contains(got, "raw HTTP request kind") {
			t.Fatalf("a changed request kind must be reported:\n%s", got)
		}
		got = rawFindings(t, old, `  up(Req) -> Out @post("/apps/{slug}/up") @http(response: "json")`)
		if !strings.Contains(got, "raw HTTP response kind") {
			t.Fatalf("a changed response kind must be reported:\n%s", got)
		}
	})
	t.Run("any", func(t *testing.T) {
		got := rawFindings(t, `  p(Req) -> Empty @any("/apps/{slug}/p") @http`, `  p(Req) -> Empty @post("/apps/{slug}/p") @http`)
		if !strings.Contains(got, "route") {
			t.Fatalf("narrowing @any to one method must be reported:\n%s", got)
		}
	})
}
