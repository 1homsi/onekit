package gents

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
)

const strictUnusedSchema = `namespace app;

model Empty {}

model PingRequest {}

model EnvironmentRef {
  id: int64;
}

model Environment {
  id: int64;
  name: string;
}

model CreateEnvironment {
  name: string;
}

model Pong {
  ok: boolean;
}

@route("/v1")
interface Environments {
  @get
  @route("/ping")
  ping(...PingRequest): Pong;

  @get
  @route("/environments")
  list(...Empty): Environment;

  @get
  @route("/environments/{id}")
  get(...EnvironmentRef): Environment;

  @delete
  @route("/environments/{id}")
  remove(...EnvironmentRef): Empty;

  @post
  @route("/environments")
  create(...CreateEnvironment): Environment;
}
`

func tscStrictUnused(t *testing.T, name string, file *onkir.File, opts Options) {
	t.Helper()
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	dir := t.TempDir()
	types := `"types": []`
	if onkir.FileHasWSMethods(file) {
		if _, err := exec.LookPath("npm"); err != nil {
			t.Skip("npm not available")
		}
		writeFile(t, filepath.Join(dir, "package.json"), `{"name": "strict-ws", "private": true}`)
		install := exec.Command("npm", "install", "--no-audit", "--no-fund", "ws", "@types/node", "@types/ws")
		install.Dir = dir
		if out, err := install.CombinedOutput(); err != nil {
			t.Skipf("npm install failed (offline?): %v\n%s", err, out)
		}
		types = `"types": ["node", "ws"]`
	}
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypesWithOptions(file, nil, opts)))
	if client := GenerateClientWithOptions(file, nil, opts); client != nil {
		writeFile(t, filepath.Join(dir, "client.ts"), string(client))
	}
	if server := GenerateServerWithOptions(file, nil, opts); server != nil {
		writeFile(t, filepath.Join(dir, "server.ts"), string(server))
	}
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022", "module": "ES2022", "moduleResolution": "bundler",
    "strict": true, "noUnusedLocals": true, "noUnusedParameters": true, "noEmit": true,
    "lib": ["ES2022", "DOM"], `+types+`
  }
}
`)
	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: generated TypeScript fails strict unused checks: %v\n%s", name, err, out)
	}
}

var strictUnusedVariants = map[string]string{
	"unary and path only": strictUnusedSchema,
	"stream only": `namespace app;

model Watch {
  id: string;
}

model Tick {
  n: int32;
}

interface Feed {
  @get
  @route("/events")
  @Onkt.stream
  events(...Watch): Tick;
}
`,
	"post stream with body": `namespace app;

model Turn {
  prompt: string;
}

model Event {
  text: string;
}

interface Agent {
  @post
  @route("/turn")
  @Onkt.stream
  turn(...Turn): Event;
}
`,
	"typed errors headers and query": `namespace app;

model Req {
  id: string;
  @query("limit")
  limit: int32;
  @query("tag")
  tags: string[];
}

model Res {
  id: string;
}

@error
model NotFound {
  @statusCode _: 404;
  message: string;
}

model Empty {}

/** Headers every Things operation takes. */
alias ThingsHeaders = {
  @header("X-Request") @format("uuid") header_x_request: string;
};

@route("/v1")
interface Things {
  @get
  @route("/things/{id}")
  get(...ThingsHeaders, ...Req): Res | NotFound;

  @get
  @route("/things")
  search(...ThingsHeaders, ...Req): Res;

  @delete
  @route("/things")
  purge(...ThingsHeaders, ...Empty): Empty;
}
`,
	"websocket": `namespace app;

model Frame {
  @Onkt.wsId
  id: string;
  text: string;
}

@route("/v1")
interface Chat {
  @Onkt.ws
  @route("/talk")
  talk(...Frame): Frame;
}
`,
	"no parameters at all": `namespace app;

model Empty {}

model Pong {
  ok: boolean;
}

interface Health {
  @get
  @route("/ping")
  ping(...Empty): Pong;
}
`,
	"body only": `namespace app;

model Make {
  name: string;
}

model Made {
  id: string;
}

interface Makers {
  @post
  @route("/make")
  make(...Make): Made;
}
`,
}

func TestTSOutputPassesNoUnusedLocalsAndParameters(t *testing.T) {
	for name, schema := range strictUnusedVariants {
		file := compileTSSchema(t, schema)
		t.Run(name+"/camel", func(t *testing.T) { tscStrictUnused(t, name, file, Options{}) })
		t.Run(name+"/wire", func(t *testing.T) { tscStrictUnused(t, name, file, Options{WireFieldNames: true}) })
	}
}
