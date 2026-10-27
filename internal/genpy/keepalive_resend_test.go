package genpy

import "testing"

func TestPythonKeepAliveNeverResendsAPostTheServerMayHaveHandled(t *testing.T) {
	runPythonSchema(t, `namespace app;

model Req {
  msg: string;
}

model Res {
  msg: string;
}

interface Echo {
  @post
  @route("/echo")
  echo(...Req): Res;

  @get
  @route("/look")
  look(...Req): Res;
}
`, `
import http.client, json, socket, threading, time
from models import Req
from client import EchoClient

handled = []

def read_request(conn):
    data = b""
    while b"\r\n\r\n" not in data:
        chunk = conn.recv(4096)
        if not chunk:
            return None
        data += chunk
    head, _, rest = data.partition(b"\r\n\r\n")
    length = 0
    for line in head.split(b"\r\n")[1:]:
        name, _, value = line.partition(b":")
        if name.lower() == b"content-length":
            length = int(value)
    while len(rest) < length:
        rest += conn.recv(4096)
    return head.split(b"\r\n")[0].decode()

def reply(conn, body):
    data = json.dumps(body).encode()
    conn.sendall(b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " + str(len(data)).encode() + b"\r\n\r\n" + data)

def session(conn):
    served = 0
    while True:
        line = read_request(conn)
        if line is None:
            return
        handled.append(line)
        served += 1
        if served == 1:
            reply(conn, {"msg": "ok"})
        else:
            conn.close()
            return

def serve(listener):
    while True:
        try:
            conn, _ = listener.accept()
        except OSError:
            return
        threading.Thread(target=session, args=(conn,), daemon=True).start()

listener = socket.socket()
listener.bind(("127.0.0.1", 0))
listener.listen(8)
threading.Thread(target=serve, args=(listener,), daemon=True).start()
url = "http://127.0.0.1:%d" % listener.getsockname()[1]

with EchoClient(url, keep_alive=True) as client:
    assert client.echo(Req(msg="a")).msg == "ok"
    try:
        client.echo(Req(msg="b"))
        raise SystemExit("the connection was dropped after the request was handled: expected an error")
    except (http.client.RemoteDisconnected, ConnectionError):
        pass
    assert handled == ["POST /echo HTTP/1.1", "POST /echo HTTP/1.1"], "a POST must not be sent again: %r" % handled
print("OK")
`)
}
