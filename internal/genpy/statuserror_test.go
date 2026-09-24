package genpy

import "testing"

func TestPythonClientRaisesUnexpectedStatusError(t *testing.T) {
	runPythonSchema(t, `
package app
message R { id: string }
service S { get(R) -> R @get("/r/{id}") }
`, `
import http.server, threading
from client import SClient, UnexpectedStatusError
from models import R

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(503)
        self.send_header("Retry-After", "3")
        self.end_headers()
        self.wfile.write(b"busy")
    def log_message(self, *args):
        pass

server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
try:
    SClient("http://127.0.0.1:%d" % server.server_port).get(R(id="1"))
    raise SystemExit("expected an error")
except UnexpectedStatusError as error:
    assert error.status == 503 and error.body == b"busy" and error.headers.get("Retry-After") == "3", vars(error)
server.shutdown()
print("OK")
`)
}
