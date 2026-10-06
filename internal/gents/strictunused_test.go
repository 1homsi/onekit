package gents

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
)

const strictUnusedSchema = `package app

message Empty {}
message PingRequest {}
message EnvironmentRef { id: int64 @encode("number") }
message Environment { id: int64 @encode("number")  name: string }
message CreateEnvironment { name: string }
message Pong { ok: bool }

service Environments {
  base_path: "/v1"
  ping(PingRequest) -> Pong @get("/ping")
  list(Empty) -> Environment @get("/environments")
  get(EnvironmentRef) -> Environment @get("/environments/{id}")
  remove(EnvironmentRef) -> Empty @delete("/environments/{id}")
  create(CreateEnvironment) -> Environment @post("/environments")
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
	"stream only": `package app

message Watch { id: string }
message Tick { n: int32 }

service Feed {
  events(Watch) -> Tick @get("/events") @stream
}
`,
	"post stream with body": `package app

message Turn { prompt: string }
message Event { text: string }

service Agent {
  turn(Turn) -> Event @post("/turn") @stream
}
`,
	"typed errors headers and query": `package app

message Req { id: string  limit: int32 @query("limit")  tags: string[] @query("tag") }
message Res { id: string }
message NotFound @status(404) { message: string }
message Empty {}

service Things {
  base_path: "/v1"
  headers: { "X-Request": string @required @format("uuid") }
  get(Req) -> Res | NotFound @get("/things/{id}")
  search(Req) -> Res @get("/things")
  purge(Empty) -> Empty @delete("/things")
}
`,
	"websocket": `package app

message Frame { id: string @ws_id  text: string }

service Chat {
  base_path: "/v1"
  talk(Frame) -> Frame @ws("/talk")
}
`,
	"no parameters at all": `package app

message Empty {}
message Pong { ok: bool }

service Health {
  ping(Empty) -> Pong @get("/ping")
}
`,
	"body only": `package app

message Make { name: string }
message Made { id: string }

service Makers {
  make(Make) -> Made @post("/make")
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
