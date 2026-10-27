package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func runPythonSchema(t *testing.T, schema, script string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	ast, err := onktest.Parse(schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
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
	runPythonSchema(t, `namespace app;

model M {
  @Onkt.required
  ids: string[];
  @Onkt.required
  labels: Record<string>;
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
	runPythonSchema(t, `namespace app;

model Req {
  msg: string;
}

model Res {
  msg: string;
}

@error
model NotFound {
  @statusCode _: 404;
  reason: string;
}

interface Echo {
  @post
  @route("/echo")
  echo(...Req): Res | NotFound;
}
`, `
import json, socket, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from models import Req, Res, NotFound
from client import EchoClient, UnexpectedStatusError

connections = []
dropped = threading.Event()

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
            self.reply(200, {"msg": msg})
            self.connection.shutdown(socket.SHUT_WR)
            self.close_connection = True
            return dropped.set()
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
    assert dropped.wait(10), "the server must drop the connection"
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

func TestPythonClientAcceptsAnEmptySuccessBody(t *testing.T) {
	runPythonSchema(t, `namespace app;

model Req {
  id: string;
}

model Res {
  id: string;
}

interface Items {
  @post
  @route("/remove")
  remove(...Req): Res;
}
`, `
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from models import Req, Res
from client import ItemsClient

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        self.rfile.read(int(self.headers["Content-Length"]))
        self.send_response(204)
        self.end_headers()

server = HTTPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
client = ItemsClient("http://127.0.0.1:%d" % server.server_address[1])
result = client.remove(Req(id="x"))
assert isinstance(result, Res) and result.id == "", result
server.shutdown()
print("OK")
`)
}

func TestPythonClientRejectsDotSegmentPathParameters(t *testing.T) {
	runPythonSchema(t, `namespace app;

model Item {
  id: string;
}

model Count {
  n: int32;
}

interface Items {
  @get
  @route("/items/{id}")
  get(...Item): Item;

  @get
  @route("/watch/{id}")
  @Onkt.stream
  watch(...Item): Item;

  @get
  @route("/total/{n}")
  total(...Count): Item;
}
`, `
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from models import Item
from client import ItemsClient

paths = []

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        paths.append(self.path)
        self.send_response(200)
        self.send_header("Content-Length", "2")
        self.end_headers()
        self.wfile.write(b"{}")

server = HTTPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
client = ItemsClient("http://127.0.0.1:%d" % server.server_address[1])
for bad in (".", ".."):
    try:
        client.get(Item(id=bad))
        raise SystemExit("get(%r) must be rejected" % bad)
    except ValueError as error:
        assert "dot segments" in str(error), error
    try:
        list(client.watch(Item(id=bad)))
        raise SystemExit("watch(%r) must be rejected" % bad)
    except ValueError as error:
        assert "dot segments" in str(error), error
assert paths == [], paths
for good in ("a", "a.b", "...", "a/b", ".a"):
    client.get(Item(id=good))
assert paths == ["/items/a", "/items/a.b", "/items/...", "/items/a%2Fb", "/items/.a"], paths
server.shutdown()
print("OK")
`)
}
