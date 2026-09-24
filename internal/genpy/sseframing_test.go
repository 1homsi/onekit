package genpy

import "testing"

func TestPythonSSEClientFollowsEventStreamFraming(t *testing.T) {
	runPythonSchema(t, `
package app
message Req { id: string }
message Tick { n: int32  text: string }
service Feed { watch(Req) -> Tick @get("/feed/{id}") @stream }
`, `
import http.server, threading
from client import FeedClient, StreamError
from models import Req

BODY = b': keepalive\n\nevent: tick\n\ndata: {"n":1,\ndata: "text":"a"}\r\n\r\nid: 7\ndata: {"n":2}\n\nevent: error\ndata: {"message":"gone"}\n\n'

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        self.wfile.write(BODY)
    def log_message(self, *args):
        pass

server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
events = []
try:
    for tick in FeedClient("http://127.0.0.1:%d" % server.server_port).watch(Req(id="1")):
        events.append(tick)
    raise SystemExit("error event was not raised")
except StreamError as error:
    assert error.payload == {"message": "gone"}, error.payload
assert [(e.n, e.text) for e in events] == [(1, "a"), (2, "")], events
server.shutdown()
print("OK")
`)
}
