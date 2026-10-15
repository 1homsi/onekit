package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func runPythonSchema(t *testing.T, schema, script string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	ast, err := onklang.Parse(schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "models.py"), string(GenerateTypes(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "client.py"), string(GenerateClient(pkg.Files[0], "models")))
	writeFile(t, filepath.Join(dir, "main.py"), script)
	cmd := exec.Command("python3", "main.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("python run failed: %v\n%s", err, out)
	}
}

func TestPythonRequiredRejectsEmptyCollections(t *testing.T) {
	runPythonSchema(t, `
package app
message M {
  ids: string[] @required
  labels: map[string, string] @required
}
`, `
from models import M

try:
    M().validate()
    raise SystemExit("expected validation error")
except ValueError as error:
    assert "ids is required" in str(error), error
    assert "labels is required" in str(error), error
M(ids=["a"], labels={"k": "v"}).validate()
print("OK")
`)
}

func TestPythonKeepAliveReusesOneConnection(t *testing.T) {
	runPythonSchema(t, `
package app
message Req { msg: string }
message Res { msg: string }
message NotFound @status(404) { reason: string }

service Echo {
  echo(Req) -> Res | NotFound @post("/echo")
}
`, `
import json, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from models import Req, Res, NotFound
from client import EchoClient, UnexpectedStatusError

connections = []

class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def setup(self):
        connections.append(self.client_address)
        super().setup()

    def log_message(self, *args):
        pass

    def reply(self, status, body, extra=None):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        for k, v in (extra or {}).items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        msg = payload.get("msg", "")
        if msg == "missing":
            return self.reply(404, {"reason": "nope"})
        if msg == "redirect":
            return self.reply(302, {}, {"Location": "/elsewhere"})
        if msg == "teapot":
            return self.reply(418, {"x": 1})
        if msg == "big":
            return self.reply(200, {"msg": "x" * 5000})
        if msg == "drop":
            self.close_connection = True
        self.reply(200, {"msg": msg})

server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
url = "http://127.0.0.1:%d" % server.server_address[1]

plain = EchoClient(url)
for i in range(3):
    assert plain.echo(Req(msg="a")).msg == "a"
assert len(connections) == 3, connections

del connections[:]
with EchoClient(url, keep_alive=True) as client:
    for i in range(5):
        assert client.echo(Req(msg="b%d" % i)).msg == "b%d" % i
    assert len(connections) == 1, "keep_alive must reuse one connection: %r" % connections

    try:
        client.echo(Req(msg="missing"))
        raise SystemExit("expected NotFound")
    except NotFound as error:
        assert error.reason == "nope", error
    try:
        client.echo(Req(msg="teapot"))
        raise SystemExit("expected UnexpectedStatusError")
    except UnexpectedStatusError as error:
        assert error.status == 418 and b'"x"' in error.body, error
    try:
        client.echo(Req(msg="redirect"))
        raise SystemExit("a redirect is not followed with keep_alive")
    except UnexpectedStatusError as error:
        assert error.status == 302 and error.headers.get("Location") == "/elsewhere", error
    assert len(connections) == 1, "errors must not cost the connection: %r" % connections

    assert client.echo(Req(msg="drop")).msg == "drop"
    assert client.echo(Req(msg="after")).msg == "after"
    assert len(connections) == 2, "a closed connection is replaced: %r" % connections

    results = []
    def worker(tag):
        for i in range(3):
            results.append(client.echo(Req(msg=tag)).msg)
    threads = [threading.Thread(target=worker, args=("t%d" % n,)) for n in range(3)]
    for th in threads: th.start()
    for th in threads: th.join()
    assert sorted(results) == sorted(["t0"] * 3 + ["t1"] * 3 + ["t2"] * 3), results

small = EchoClient(url, keep_alive=True, max_response_body_bytes=100)
try:
    small.echo(Req(msg="big"))
    raise SystemExit("expected the response limit to apply")
except ValueError as error:
    assert "exceeds" in str(error), error
assert small.echo(Req(msg="ok")).msg == "ok"
small.close()
server.shutdown()
print("OK")
`)
}
