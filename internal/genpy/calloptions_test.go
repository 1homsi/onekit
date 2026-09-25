package genpy

import "testing"

func TestPythonClientPerCallHeadersAndTimeout(t *testing.T) {
	runPythonSchema(t, `
package app
message Note { id: string }
service Notes { get(Note) -> Note @get("/notes/{id}") }
`, `
import json, threading, http.server
from client import NotesClient
from models import Note

seen = []
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        seen.append((self.headers.get("X-Trace"), self.headers.get("X-Base")))
        body = json.dumps({"id": "n1"}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *args): pass

server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
client = NotesClient("http://127.0.0.1:%d" % server.server_port, headers={"X-Base": "b", "X-Trace": "default"})
assert client.get(Note(id="n1"), headers={"X-Trace": "t-1"}, timeout=5).id == "n1"
assert client.get(Note(id="n1")).id == "n1"
assert seen == [("t-1", "b"), ("default", "b")], seen
server.shutdown()
print("OK")
`)
}
