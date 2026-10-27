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

func TestConvertKeepsTheTypeOfANonStringIn(t *testing.T) {
	file := &onkschema.File{Package: "app", Messages: []*onkschema.MessageDecl{{Name: "M", Fields: []*onkschema.FieldDecl{
		{Name: "ratio", Type: &onkschema.TypeRef{Name: "float64"}, Decorators: []onkschema.Decorator{{Name: "in", Args: []onkschema.Arg{{Value: "1"}}}}},
		{Name: "tags", Repeated: true, Type: &onkschema.TypeRef{Name: "string"}, Decorators: []onkschema.Decorator{{Name: "in", Args: []onkschema.Arg{{Value: "a", Quoted: true}}}}},
	}}}}
	out, err := Convert([]Source{{Path: "/p/a.onk", AST: file}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@Onkt.values(1)\n  ratio: float64;", `@Onkt.values("a")` + "\n  tags: string[];"} {
		if !strings.Contains(out[0].Content, want) {
			t.Errorf("missing %q in:\n%s", want, out[0].Content)
		}
	}
}

func TestConvertSpellsEdgeCasesTheEmitterReadsBack(t *testing.T) {
	q := func(v string) onkschema.Arg { return onkschema.Arg{Value: v, Quoted: true} }
	file := &onkschema.File{Package: "app",
		Messages: []*onkschema.MessageDecl{
			{Name: "B", Fields: []*onkschema.FieldDecl{{Name: "v", Type: &onkschema.TypeRef{Name: "string"}}}},
			{Name: "Req", Fields: []*onkschema.FieldDecl{
				{Name: "b", Type: &onkschema.TypeRef{Name: "B"}},
				{Name: "note", Type: &onkschema.TypeRef{Name: "string"}},
				{Name: "ids", Repeated: true, Type: &onkschema.TypeRef{Name: "int64"}, Decorators: []onkschema.Decorator{{Name: "encode", Args: []onkschema.Arg{q("number")}}}},
				{Name: "keys", Repeated: true, Type: &onkschema.TypeRef{Name: "uint64"}},
			}},
		},
		Services: []*onkschema.ServiceDecl{{Name: "S", BasePath: "/", RPCs: []*onkschema.RPCDecl{{
			Name: "a", RequestType: "Req", ResponseType: "B",
			Decorators: []onkschema.Decorator{{Name: "post", Args: []onkschema.Arg{q("/a")}}, {Name: "body", Args: []onkschema.Arg{q("b")}}, {Name: "max_body", Args: []onkschema.Arg{q("1KiB")}}},
		}}}},
	}
	out, err := Convert([]Source{{Path: "/p/sub/a.onk", AST: file}}, Options{BasePath: func(string) string { return "/sub" }})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// An explicit root base path stays the root, not the directory.
		"@route(\"/\")\ninterface S {",
		// Beside the @body field, a plain field does not cross the wire.
		"  @body\n  b: B;",
		"  @bodyIgnore\n  note: string;",
		// Repeated 64-bit integers: numbers stay plain, the default is spelled out.
		"  ids: int64[];",
		"  @Onkt.encode(\"string\")\n  keys: uint64[];",
	} {
		if !strings.Contains(out[0].Content, want) {
			t.Errorf("missing %q in:\n%s", want, out[0].Content)
		}
	}
}
