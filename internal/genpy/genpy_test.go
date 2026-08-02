package genpy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
  bio: string?
  tags: string[]
  labels: map[string, string]
  home_address: Address
}

enum Status {
  UNSPECIFIED
  ACTIVE @json("active")
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
	out := GenerateClient(file, "models")
	if len(out) == 0 {
		t.Fatalf("expected non-empty generated client")
	}
}

func TestGenerateStreamingClientProducesIterator(t *testing.T) {
	ast, err := onklang.Parse(`
package app
message StreamRequest { channel: string @query }
message Event { value: string }
service Events { streamEvents(StreamRequest) -> Event @get("/events") @stream }
`)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "stream.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	out := string(GenerateClient(pkg.Files[0], "models"))
	for _, want := range []string{"from typing import Iterator", "-> Iterator[Event]:", "text/event-stream", "yield Event.from_dict(payload)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated streaming client missing %q:\n%s", want, out)
		}
	}
}

const serverHarness = `
import http.server
import json
import threading

import models


class Handler(http.server.BaseHTTPRequestHandler):
    users = {}

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        data = json.loads(self.rfile.read(length))
        user_id = "user-%d" % (len(Handler.users) + 1)
        user = models.User(id=user_id, name=data["name"], email=data["email"])
        Handler.users[user_id] = user
        self._write(200, user.to_dict())

    def do_GET(self):
        prefix = "/api/v1/users/"
        if self.path.startswith(prefix):
            user_id = self.path[len(prefix):]
            user = Handler.users.get(user_id)
            if user is None:
                err = models.NotFoundError(resource_type="user", resource_id=user_id)
                self._write(404, err.to_dict())
                return
            self._write(200, user.to_dict())
            return
        self._write(404, {"message": "not found"})

    def _write(self, status, body):
        payload = json.dumps(body).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, fmt, *args):
        pass


def main():
    import client

    server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    base_url = "http://127.0.0.1:%d" % server.server_port
    c = client.UserServiceClient(base_url)

    created = c.create_user(models.CreateUserRequest(name="Ada Lovelace", email="ada@example.com"))
    assert created.name == "Ada Lovelace", created
    assert created.email == "ada@example.com", created
    assert created.id, created

    fetched = c.get_user(models.GetUserRequest(id=created.id))
    assert fetched.id == created.id, fetched
    assert fetched.name == created.name, fetched

    try:
        c.get_user(models.GetUserRequest(id="does-not-exist"))
        raise AssertionError("expected NotFoundError")
    except models.NotFoundError as e:
        assert e.resource_type == "user", e
        assert e.resource_id == "does-not-exist", e

    server.shutdown()
    print("OK")


if __name__ == "__main__":
    main()
`

func TestGeneratedPythonRuntimeBehavior(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	file := compileFixture(t)
	typesSrc := GenerateTypes(file)
	clientSrc := GenerateClient(file, "models")

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "models.py"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "client.py"), string(clientSrc))
	writeFile(t, filepath.Join(dir, "main.py"), serverHarness)

	cmd := exec.Command("python3", "main.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python run failed: %v\n%s", err, out)
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
