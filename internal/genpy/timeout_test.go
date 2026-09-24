package genpy

import "testing"

func TestPythonClientTimesOutStalledServer(t *testing.T) {
	runPythonSchema(t, `
package app
message R { id: string }
service S { get(R) -> R @get("/r/{id}") }
`, `
import socket, threading
from client import SClient
from models import R

listener = socket.socket()
listener.bind(("127.0.0.1", 0))
listener.listen(1)
held = []
threading.Thread(target=lambda: held.append(listener.accept()), daemon=True).start()
client = SClient("http://127.0.0.1:%d" % listener.getsockname()[1], timeout=0.2)
try:
    client.get(R(id="1"))
    raise SystemExit("expected timeout")
except (TimeoutError, OSError):
    pass
print("OK")
`)
}
