package gents

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onktest"
)

const fixtureSrc = `namespace app;

model Address {
  street: string;
  city: string;
}

model User {
  id: string;
  @minLength(2)
  @maxLength(100)
  name: string;
  @format("email")
  email: string;
  bio?: string;
  tags: string[];
  labels: Record<string>;
  home_address: Address;
}

enum Status {
  UNSPECIFIED,
  ACTIVE: "active",
}

@discriminated(#{ discriminatorPropertyName: "type", envelope: "none" })
union EventPayload {
  @Onkt.tag("text") text: TextPayload,
  @Onkt.tag("image") image: ImagePayload,
}

model Event {
  id: string;
  payload: EventPayload;
}

model TextPayload {
  body: string;
}

model ImagePayload {
  url: string;
}

model CreateUserRequest {
  @minLength(2)
  @maxLength(100)
  name: string;
  @format("email")
  email: string;
}

model GetUserRequest {
  id: string;
}

@error
model NotFoundError {
  @statusCode _: 404;
  resource_type: string;
  resource_id: string;
}

@route("/api/v1")
interface UserService {
  @post
  @route("/users")
  createUser(...CreateUserRequest): User;

  @get
  @route("/users/{id}")
  getUser(...GetUserRequest): User | NotFoundError;
}
`

func compileFixture(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onktest.Parse(fixtureSrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	return pkg.Files[0]
}

func TestGenerateTypesProducesOutput(t *testing.T) {
	file := compileFixture(t)
	out := GenerateTypes(file)
	if len(out) == 0 {
		t.Fatalf("expected non-empty generated types")
	}
}

func TestGenerateClientProducesOutput(t *testing.T) {
	file := compileFixture(t)
	out := GenerateClient(file)
	if len(out) == 0 {
		t.Fatalf("expected non-empty generated client")
	}
}

func TestGenerateServerProducesOutput(t *testing.T) {
	file := compileFixture(t)
	out := GenerateServer(file)
	if len(out) == 0 {
		t.Fatalf("expected non-empty generated server")
	}
}

func TestGeneratedTypeScriptTypeChecks(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}

	file := compileFixture(t)
	typesSrc := GenerateTypes(file)
	clientSrc := GenerateClient(file)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "client.ts"), string(clientSrc))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "lib": ["ES2022", "DOM"]
  }
}
`)

	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc type check failed: %v\n%s", err, out)
	}
}

func TestGeneratedTypeScriptNumericValidationTypeChecks(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}

	ast, err := onktest.Parse(`namespace app;

model Request {
  @Onkt.required
  @minValueExclusive(0)
  @encode(string)
  id: int64;
  @minValue(0)
  count?: int32;
  @Onkt.required
  label: string;
}
`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.CompileWithOptions(
		[]onkcompile.Source{{Path: "app.tsp", AST: ast}},
		onkcompile.CompileOptions{AllowLegacyContracts: true},
	)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "lib": ["ES2022", "DOM"]
  }
}
`)

	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("numeric generated types failed to type check: %v\n%s", err, out)
	}
}

func TestGeneratedRootUnwrapRPCTypeChecks(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	ast, err := onktest.Parse(`namespace app;

model RootValues {
  @Onkt.unwrap
  values: string[];
}

interface RootService {
  @post
  @route("/values")
  send(...RootValues): RootValues;
}
`)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "root.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "client.ts"), string(GenerateClient(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServer(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "lib": ["ES2022", "DOM"]
  }
}
`)

	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("root unwrap RPC TypeScript failed: %v\n%s", err, out)
	}
}

const serverHarness = `
import { createUserServiceRoutes, HttpError } from "./server.ts";
import type { RouteDescriptor } from "./server.ts";
import type { User, CreateUserRequest, GetUserRequest, NotFoundError } from "./types.ts";
import { encodeNotFoundError, decodeNotFoundError } from "./types.ts";

const users = new Map<string, User>();

const handler = {
  async createUser(req: CreateUserRequest): Promise<User> {
    const id = "user-" + (users.size + 1);
    const u: User = { id, name: req.name, email: req.email };
    users.set(id, u);
    return u;
  },
  async getUser(req: GetUserRequest): Promise<User> {
    const u = users.get(req.id!);
    if (!u) {
      const body: NotFoundError = { resourceType: "user", resourceId: req.id };
      throw new HttpError(404, encodeNotFoundError(body));
    }
    return u;
  },
};

function findRoute(routes: RouteDescriptor[], method: string, path: string): RouteDescriptor {
  const r = routes.find((r) => r.method === method && r.path === path);
  if (!r) throw new Error("route not found: " + method + " " + path);
  return r;
}

async function main() {
  const routes = createUserServiceRoutes(handler);

  const createRoute = findRoute(routes, "POST", "/api/v1/users");
  const createReq = new Request("http://x/api/v1/users", {
    method: "POST",
    body: JSON.stringify({ name: "Ada Lovelace", email: "ada@example.com" }),
  });
  const createRes = await createRoute.handler(createReq);
  if (createRes.status !== 200) throw new Error("expected 200, got " + createRes.status);
  const created = (await createRes.json()) as User;
  if (created.name !== "Ada Lovelace" || !created.id) throw new Error("unexpected created user: " + JSON.stringify(created));

  const getRoute = findRoute(routes, "GET", "/api/v1/users/{id}");
  const getReq = new Request("http://x/api/v1/users/" + created.id);
  const getRes = await getRoute.handler(getReq);
  if (getRes.status !== 200) throw new Error("expected 200, got " + getRes.status);
  const fetched = (await getRes.json()) as User;
  if (fetched.id !== created.id) throw new Error("unexpected fetched user: " + JSON.stringify(fetched));

  const missReq = new Request("http://x/api/v1/users/does-not-exist");
  const missRes = await getRoute.handler(missReq);
  if (missRes.status !== 404) throw new Error("expected 404, got " + missRes.status);
  const notFoundWire = await missRes.json();
  if (notFoundWire.resource_type !== "user" || notFoundWire.resource_id !== "does-not-exist") {
    throw new Error("unexpected not-found wire body: " + JSON.stringify(notFoundWire));
  }
  const notFound: NotFoundError = decodeNotFoundError(notFoundWire);
  if (notFound.resourceType !== "user" || notFound.resourceId !== "does-not-exist") {
    throw new Error("unexpected decoded not-found body: " + JSON.stringify(notFound));
  }

  console.log("OK");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`

func TestGeneratedServerRuntimeBehavior(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	file := compileFixture(t)
	typesSrc := GenerateTypes(file)
	serverSrc := GenerateServer(file)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(typesSrc))
	// See the matching comment in sse_test.go: Node's native TS execution
	// (used only here, not by real bundler-based consumers) needs an
	// explicit extension on this generated file's own "./types" import.
	serverSrcForNode := strings.ReplaceAll(string(serverSrc), `from "./types.js"`, `from "./types.ts"`)
	writeFile(t, filepath.Join(dir, "server.ts"), serverSrcForNode)
	writeFile(t, filepath.Join(dir, "main.ts"), serverHarness)

	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
