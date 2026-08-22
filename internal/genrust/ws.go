package genrust

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

// --- server ---------------------------------------------------------------

// wsHandlerFnName mints the axum upgrade-handler name for a @ws method.
func wsHandlerFnName(service *onkir.Service, method *onkir.Method) string {
	return SnakeCase(service.Name) + "_" + SnakeCase(method.Name) + "_ws_handler"
}

// writeWSRouterEntry routes the upgrade through axum's native WebSocket
// support; GET carries the upgrade handshake.
func writeWSRouterEntry(p *Printer, service *onkir.Service, method *onkir.Method) {
	wsPath, _ := method.WebSocketPath()
	fullPath := service.BasePath + wsPath
	p.P(
		".route(", fmt.Sprintf("%q", fullPath), ", axum::routing::get(",
		wsHandlerFnName(service, method), "::<T>))",
	)
}

func writeWSUpgradeHandler(p *Printer, service *onkir.Service, method *onkir.Method) {
	wsPath, _ := method.WebSocketPath()
	pathFields := pathFieldNames(wsPath)
	errorName := serverErrorName(service, method)
	requestRef := p.MessageTypeName(method.Request)

	p.P("#[allow(clippy::too_many_arguments)]")
	p.P("async fn ", wsHandlerFnName(service, method), "<T: ", PascalCase(service.Name), ">(")
	p.Indent()
	p.P("ws: axum::extract::ws::WebSocketUpgrade,")
	p.P("State(service): State<Arc<T>>,")
	p.P("headers: HeaderMap,")
	if len(pathFields) > 0 {
		p.P("Path(path): Path<std::collections::HashMap<String, String>>,")
	}
	p.Dedent()
	p.P(") -> Response {")
	p.Indent()
	p.P("let mut req = ", requestRef, "::default();")
	for _, name := range pathFields {
		field := onkir.FindField(method.Request, name)
		if field == nil {
			continue
		}
		p.P("let Some(value) = path.get(", fmt.Sprintf("%q", name), ") else {")
		p.Indent()
		p.P(
			"return ", errorName, "::InvalidRequest(",
			fmt.Sprintf("%q", "missing path field "+name), ").into_response();",
		)
		p.Dedent()
		p.P("};")
		p.P("req.", RustIdent(field.Name), " = match parse_path(value) {")
		p.Indent()
		p.P("Ok(value) => value,")
		writeInvalidPathArm(p, errorName, name)
		p.Dedent()
		p.P("};")
	}
	for _, header := range combinedHeaders(service, method) {
		format, hasFormat := header.Format()
		p.P("let header_value = headers.get(", fmt.Sprintf("%q", header.Name), ").and_then(|value| value.to_str().ok());")
		if header.Required() {
			p.P("if header_value.is_none_or(|value| value.is_empty()) {")
			p.Indent()
			p.P(
				"return ", errorName, "::InvalidRequest(",
				fmt.Sprintf("%q", "missing required header "+header.Name),
				").into_response();",
			)
			p.Dedent()
			p.P("}")
		}
		if hasFormat {
			_ = format
		}
	}
	p.P("if let Err(error) = req.validate() { return ", errorName, "::Validation(error).into_response(); }")
	p.P("let context = RequestContext { headers };")
	p.P("ws.on_upgrade(move |socket: axum::extract::ws::WebSocket| async move {")
	p.Indent()
	p.P("let (mut sink, mut stream) = socket.split();")
	p.P("let out = WsSink::<", p.MessageTypeName(method.Response), "> { sink };")
	p.P("if let Err(error) = service.", RustIdent(method.Name), "(context, req, out).await {")
	p.Indent()
	p.P(`let _ = axum::Json(serde_json::json!({ "error": error.to_string() })).into_response();`)
	p.Dedent()
	p.P("}")
	p.P("while let Some(frame) = stream.next().await {")
	p.Indent()
	p.P("let _ = frame;")
	p.Dedent()
	p.P("}")
	p.Dedent()
	p.P("})")
	p.Dedent()
	p.P("}")
	p.Blank()

	// The WsSink helper is emitted once per module by WriteWSServerRuntime.
}

// WriteWSServerRuntime emits the server-side sender wrapping an axum split
// sink so trait methods push typed response frames.
func WriteWSServerRuntime(p *Printer) {
	p.P("pub struct WsSink<E> {")
	p.P("sink: futures_util::stream::SplitSink<axum::extract::ws::WebSocket, axum::extract::ws::Message>,")
	p.P("}")
	p.P()
	p.P("impl<E: serde::Serialize> WsSink<E> {")
	p.P("pub async fn send(&mut self, value: E) -> Result<(), String> {")
	p.P("let data = serde_json::to_string(&value).map_err(|error| error.to_string())?;")
	p.P("use futures_util::SinkExt;")
	p.P("self.sink.send(axum::extract::ws::Message::text(data)).await.map_err(|error| error.to_string())")
	p.P("}")
	p.P("}")
	p.Blank()
}

// --- client ---------------------------------------------------------------

func writeRustWSClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	wsPath, _ := m.WebSocketPath()
	fullPath := s.BasePath + wsPath
	methodName := RustIdent(m.Name)
	requestType := p.MessageTypeName(m.Request)
	responseType := p.MessageTypeName(m.Response)
	socketName := PascalCase(s.Name) + PascalCase(m.Name) + "Socket"
	errorName := clientErrorName(s, m)

	p.P("pub async fn ", methodName, "(&self, req: &", requestType, ") -> Result<", socketName, "<", responseType, ">, ", errorName, "> {")
	p.Indent()
	p.P("req.validate().map_err(", errorName, "::Validation)?;")
	p.P("let mut path = ", fmt.Sprintf("%q", fullPath), ".to_owned();")
	for _, name := range pathFieldNames(wsPath) {
		if field := onkir.FindField(m.Request, name); field != nil {
			access := "req." + RustIdent(field.Name)
			p.P(
				"path = path.replace(", fmt.Sprintf("%q", "{"+name+"}"),
				", &urlencoding::encode(&query_value(&", access, ")));",
			)
		}
	}
	p.P("let mut url = self.base_url.clone();")
	p.P("url.push_str(&path);")
	p.P(`let url = url.replacen("https://", "wss://", 1).replacen("http://", "ws://", 1);`)
	p.P("let (stream, _) = tokio_tungstenite::connect_async(url).await.map_err(", errorName, "::Transport)?;")
	p.P("Ok(", socketName, " { stream })")
	p.Dedent()
	p.P("}")
	p.Blank()
}

func writeRustWSDuplexTypes(p *Printer, service *onkir.Service, methods []*onkir.Method) {
	if len(methods) == 0 {
		return
	}
	_ = service
	p.P("pub struct WsFrameSocket<E> {")
	p.P("stream: tokio_tungstenite::WebSocketStream<tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>>,")
	p.P("_marker: std::marker::PhantomData<E>,")
	p.P("}")
	p.P()
	p.P("impl<E: serde::de::DeserializeOwned + serde::Serialize> WsFrameSocket<E> {")
	p.P("pub async fn send_raw(&mut self, data: String) -> Result<(), tokio_tungstenite::tungstenite::Error> {")
	p.P("use futures_util::SinkExt;")
	p.P("self.stream.send(tokio_tungstenite::tungstenite::Message::text(data)).await")
	p.P("}")
	p.P()
	p.P("pub async fn receive_raw(&mut self) -> Option<Result<String, tokio_tungstenite::tungstenite::Error>> {")
	p.P("use futures_util::StreamExt;")
	p.P("match self.stream.next().await {")
	p.P("Some(Ok(message)) => Some(Ok(message.into_text().unwrap_or_default())),")
	p.P("Some(Err(error)) => Some(Err(error)),")
	p.P("None => None,")
	p.P("}")
	p.P("}")
	p.P()
	p.P("pub async fn close(&mut self) {")
	p.P("use futures_util::SinkExt;")
	p.P("let _ = self.stream.close().await;")
	p.P("}")
	p.P("}")
	p.Blank()
	_ = len(methods)
}
