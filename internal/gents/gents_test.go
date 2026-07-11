package gents

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const fixtureSrc = `
package app

message Address {
  street: string
  city: string
}

message User {
  id: string
  name: string @len(2, 100)
  email: string @email
  bio: string? @nullable
  tags: string[]
  labels: map[string, string]
  home_address: Address
}

enum Status {
  UNSPECIFIED
  ACTIVE @json("active")
}

message Event {
  id: string
  payload: oneof(discriminator: "type", flatten: true) {
    text: TextPayload @tag("text")
    image: ImagePayload @tag("image")
  }
}

message TextPayload {
  body: string
}

message ImagePayload {
  url: string
}

message CreateUserRequest {
  name: string @len(2, 100)
  email: string @email
}

message GetUserRequest {
  id: string
}

message NotFoundError @status(404) {
  resource_type: string
  resource_id: string
}

service UserService {
  base_path: "/api/v1"

  createUser(CreateUserRequest) -> User @post("/users")

  getUser(GetUserRequest) -> User | NotFoundError @get("/users/{id}")
}
`

func compileFixture(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(fixtureSrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
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

const serverHarness = `
import { createUserServiceRoutes, HttpError } from "./server.ts";
import type { RouteDescriptor } from "./server.ts";
import type { User, CreateUserRequest, GetUserRequest, NotFoundError } from "./types.ts";

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
      const body: NotFoundError = { resource_type: "user", resource_id: req.id };
      throw new HttpError(404, body);
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
  const notFound = (await missRes.json()) as NotFoundError;
  if (notFound.resource_type !== "user" || notFound.resource_id !== "does-not-exist") {
    throw new Error("unexpected not-found body: " + JSON.stringify(notFound));
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
	writeFile(t, filepath.Join(dir, "server.ts"), string(serverSrc))
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
