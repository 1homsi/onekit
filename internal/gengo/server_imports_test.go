package gengo

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

// The server emitted a `net/url` import gated on "has a @stream method with a
// path parameter", but no code in server.gen.go ever references `url.`. Every
// url.PathEscape use is in the CLIENT emitters (client.go, sse.go, ws.go),
// which write client.gen.go. The server routes use r.PathValue instead.
// So the moment the gate fires, the generated package fails to compile.
const streamWithPathParam = `namespace p;

model TickReq {
  id: string;
}

model Tick {
  @encode(string)
  value: int64;
}

@error
model Boom {
  @statusCode _: 500;
  message: string;
}

@route("/api")
interface Svc {
  @get
  @route("/watch/{id}")
  @Onkt.stream
  watch(...TickReq): Tick | Boom;
}
`

func generateServer(t *testing.T, src string) string {
	t.Helper()
	ast, err := onktest.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "schema.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	data, err := GenerateServer(pkg.Files[0])
	if err != nil {
		t.Fatalf("generate server: %v", err)
	}
	return string(data)
}

// A `@stream` method with at least one path parameter must not emit an import
// the generated file never uses.
func TestGeneratedServerHasNoUnusedNetURLImport(t *testing.T) {
	src := generateServer(t, streamWithPathParam)

	if !strings.Contains(src, `"net/url"`) && !strings.Contains(src, "url.PathEscape") {
		// Neither present: nothing to assert.
		return
	}
	if strings.Contains(src, `"net/url"`) && !strings.Contains(src, "url.") {
		t.Fatalf("generated server imports net/url but never uses url.:\n%s", src)
	}
}

// The client, which does use url.PathEscape, must still import net/url.
func TestGeneratedClientStillImportsNetURLForPathParams(t *testing.T) {
	ast, err := onktest.Parse(streamWithPathParam)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "schema.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	data, err := GenerateClient(pkg.Files[0])
	if err != nil {
		t.Fatalf("generate client: %v", err)
	}
	src := string(data)
	if strings.Contains(src, "url.") && !strings.Contains(src, `"net/url"`) {
		t.Fatalf("client uses url. but does not import net/url:\n%s", src)
	}
}
