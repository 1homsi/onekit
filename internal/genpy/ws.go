package genpy

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

// writePyWSRuntime emits the shared bidirectional frame-socket handle. The
// websockets dependency is imported lazily so schemas without @ws RPCs keep
// their stdlib-only story.
func writePyWSRuntime(p *Printer) {
	p.P("class WsFrameSocket:")
	p.Indent()
	p.P(`"""Bidirectional JSON-frame WebSocket handle."""`)
	p.P("")
	p.P("def __init__(self, connection, response_type):")
	p.Indent()
	p.P("self._connection = connection")
	p.P("self._response_type = response_type")
	p.Dedent()
	p.P("")
	p.P("def send(self, frame):")
	p.Indent()
	p.P("if hasattr(frame, \"validate\"): frame.validate()")
	p.P("self._connection.send(json.dumps(frame.to_dict()))")
	p.Dedent()
	p.P("")
	p.P("def receive(self):")
	p.Indent()
	p.P("payload = json.loads(self._connection.recv(timeout=30))")
	p.P("return self._response_type.from_dict(payload)")
	p.Dedent()
	p.P("")
	p.P("def close(self):")
	p.Indent()
	p.P("self._connection.close()")
	p.Dedent()
	p.Dedent()
	p.Blank()
}

// writePyWSClientMethod emits one @ws client method returning a WsFrameSocket
// bound to the pair's response codec.
func writePyWSClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	wsPath, _ := m.WebSocketPath()
	fullPath := s.BasePath + wsPath
	p.P("def ", SnakeCase(m.Name), "(self, req: ", p.MessageTypeName(m.Request), ") -> WsFrameSocket:")
	p.Indent()
	p.P(`if hasattr(req, "validate"): req.validate()`)
	p.P(fmt.Sprintf("path = %q", fullPath))
	for _, paramName := range onkir.PathParamNames(wsPath) {
		field := onkir.FindField(m.Request, paramName)
		if field == nil {
			continue
		}
		p.P(fmt.Sprintf(
			"path = path.replace(%q, urllib.parse.quote(str(req.%s), safe=\"\"))",
			"{"+paramName+"}", field.Name,
		))
	}
	writeClientQueryParams(p, m.Request)
	p.P("socket_url = self.base_url + path")
	p.P("try:")
	p.Indent()
	p.P("from websockets.sync.client import connect as _ws_connect")
	p.Dedent()
	p.P("except ImportError as _exc:")
	p.Indent()
	p.P(`raise ImportError("@ws clients need the 'websockets>=12' package") from _exc`)
	p.Dedent()
	p.P("return WsFrameSocket(_ws_connect(socket_url, additional_headers=dict(self.headers)), ", p.MessageTypeName(m.Response), ")")
	p.Dedent()
	p.Blank()
}
