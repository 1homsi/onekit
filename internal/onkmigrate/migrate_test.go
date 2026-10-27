package onkmigrate

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
)

func TestQuoteEscapesTypeSpecStrings(t *testing.T) {
	cases := map[string]string{
		`plain`:        `"plain"`,
		`say "hi"`:     `"say \"hi\""`,
		`a\b`:          `"a\\b"`,
		"two\nlines":   `"two\nlines"`,
		"cost ${x} $y": `"cost \${x} $y"`,
	}
	for in, want := range cases {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestNumberHasNoExponent(t *testing.T) {
	for in, want := range map[string]string{"+5": "5", "-1.5e2": "-150", "2.5E+3": "2500", "0.25": "0.25"} {
		if got := number(in); got != want {
			t.Errorf("number(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestIdentEscapesKeywordsAndOddNames(t *testing.T) {
	for in, want := range map[string]string{"user": "user", "model": "`model`", "op": "`op`", "x-y": "`x-y`"} {
		if got := ident(in); got != want {
			t.Errorf("ident(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestRouteSpellsWildcardsAsReservedExpansion(t *testing.T) {
	if got := route("/files/{path...}"); got != "/files/{+path}" {
		t.Fatalf("route = %s", got)
	}
}

func TestConvertQualifiesOnlyAmbiguousNames(t *testing.T) {
	models := &onkschema.File{Package: "app.models", Messages: []*onkschema.MessageDecl{{Name: "User"}, {Name: "Response"}}}
	api := &onkschema.File{Package: "app.api", Services: []*onkschema.ServiceDecl{{
		Name: "Users",
		RPCs: []*onkschema.RPCDecl{{
			Name: "get", RequestType: "User", ResponseType: "Response",
			Decorators: []onkschema.Decorator{{Name: "get", Args: []onkschema.Arg{{Value: "/u"}}}},
		}},
	}}}
	out, err := Convert([]Source{{Path: "/p/models.onk", AST: models}, {Path: "/p/api.onk", AST: api}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var service string
	for _, o := range out {
		if strings.HasSuffix(o.Path, "api.tsp") {
			service = o.Content
		}
	}
	if !strings.Contains(service, "using app.models;") {
		t.Errorf("missing using:\n%s", service)
	}
	// Response is also TypeSpec.Http.Response, so it stays qualified.
	if !strings.Contains(service, "get(...User): app.models.Response;") {
		t.Errorf("unexpected operation:\n%s", service)
	}
}
