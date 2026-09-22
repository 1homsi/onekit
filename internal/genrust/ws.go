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

// wsOutType returns the type the trait method's `out` parameter takes for a
// @ws method: WsSink<Response> normally, or WsCallSink<K, Response, Request>
// when the method uses @ws_id - the extra Call(id, value) -> Request that
// registers a pending reply, sends, and awaits it.
func wsOutType(p *Printer, m *onkir.Method) string {
	responseType := p.MessageTypeName(m.Response)
	idField, correlated := m.WSIDField()
	if !correlated {
		return "WsSink<" + responseType + ">"
	}
	kType := RustScalarType(idField.Type.Scalar)
	return "WsCallSink<" + kType + ", " + responseType + ", " + p.MessageTypeName(m.Request) + ">"
}

// writeWSUpgradeHandler emits the axum upgrade handler. Unlike a single
// one-shot call to the trait method, the handler is invoked once per
// decoded inbound frame for the connection's whole lifetime, matching the
// Go/TS backends: without this a Rust @ws handler could never see a second
// inbound frame at all (the read loop used to just drain and discard).
func writeWSUpgradeHandler(p *Printer, service *onkir.Service, method *onkir.Method) {
	wsPath, _ := method.WebSocketPath()
	pathFields := pathFieldNames(wsPath)
	errorName := serverErrorName(service, method)
	requestRef := p.MessageTypeName(method.Request)
	responseRef := p.MessageTypeName(method.Response)
	idField, correlated := method.WSIDField()

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
			fmt.Sprintf("%q", "missing path field "+name), ".into()).into_response();",
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
				".into()).into_response();",
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
	p.P("use futures_util::StreamExt;")
	p.P("let (sink, mut stream) = socket.split();")
	p.P("let sink = std::sync::Arc::new(tokio::sync::Mutex::new(sink));")
	if correlated {
		kType := RustScalarType(idField.Type.Scalar)
		p.P("let out = WsCallSink::<", kType, ", ", responseRef, ", ", requestRef, "> { inner: WsSink { sink: sink.clone(), _marker: std::marker::PhantomData }, pending: WsPending::new() };")
	} else {
		p.P("let out = WsSink::<", responseRef, "> { sink: sink.clone(), _marker: std::marker::PhantomData };")
	}
	p.P("while let Some(message) = stream.next().await {")
	p.Indent()
	p.P("let Ok(message) = message else { break; };")
	p.P("let Ok(text) = message.into_text() else { continue; };")
	p.P("let frame: ", requestRef, " = match serde_json::from_str(&text) {")
	p.Indent()
	p.P("Ok(frame) => frame,")
	p.P("Err(_) => continue,")
	p.Dedent()
	p.P("};")
	p.P("if let Err(error) = frame.validate() {")
	p.Indent()
	p.P("let _ = out.send(", responseRef, "::default()).await;")
	p.P("let mut guard = sink.lock().await;")
	p.P("use futures_util::SinkExt;")
	p.P("let _ = guard.send(axum::extract::ws::Message::Close(Some(axum::extract::ws::CloseFrame { code: 1008, reason: error.to_string().into() }))).await;")
	p.P("break;")
	p.Dedent()
	p.P("}")
	if correlated {
		p.P("if let Some(id) = frame.ws_id() {")
		p.Indent()
		p.P("if out.pending.resolve(&id, frame.clone()).await { continue; }")
		p.Dedent()
		p.P("}")
	}
	p.P("if let Err(error) = service.", RustIdent(method.Name), "(context.clone(), frame, out.clone()).await {")
	p.Indent()
	p.P(`let _ = axum::Json(serde_json::json!({ "error": error.to_string() })).into_response();`)
	p.Dedent()
	p.P("}")
	p.Dedent()
	p.P("}")
	if correlated {
		p.P("out.pending.close_all().await;")
	}
	p.Dedent()
	p.P("})")
	p.Dedent()
	p.P("}")
	p.Blank()
}

// WriteWSServerRuntime emits the server-side runtime shared by every @ws
// method in the file: WsSink (mutex-guarded, Clone - so a handler can retain
// it and send from a spawned task, and so the read loop above can hand a
// fresh clone to every frame's handler call without moving it away), and,
// when any method uses @ws_id, WsPending (the correlation map) and
// WsCallSink (WsSink plus the Call helper).
func WriteWSServerRuntime(p *Printer, hasWSCorrelation bool) {
	p.P("pub struct WsSink<E> {")
	p.P("sink: std::sync::Arc<tokio::sync::Mutex<futures_util::stream::SplitSink<axum::extract::ws::WebSocket, axum::extract::ws::Message>>>,")
	p.P("_marker: std::marker::PhantomData<E>,")
	p.P("}")
	p.P()
	p.P("impl<E> Clone for WsSink<E> {")
	p.P("fn clone(&self) -> Self { Self { sink: self.sink.clone(), _marker: std::marker::PhantomData } }")
	p.P("}")
	p.P()
	p.P("impl<E: serde::Serialize> WsSink<E> {")
	p.P("pub async fn send(&self, value: E) -> Result<(), String> {")
	p.P("let data = serde_json::to_string(&value).map_err(|error| error.to_string())?;")
	p.P("use futures_util::SinkExt;")
	p.P("self.sink.lock().await.send(axum::extract::ws::Message::text(data)).await.map_err(|error| error.to_string())")
	p.P("}")
	p.P("}")
	p.Blank()
	if !hasWSCorrelation {
		return
	}
	writeWSPendingType(p)
	p.P("pub struct WsCallSink<K, E, R> {")
	p.P("inner: WsSink<E>,")
	p.P("pending: WsPending<K, R>,")
	p.P("}")
	p.P()
	p.P("impl<K, E, R> Clone for WsCallSink<K, E, R> {")
	p.P("fn clone(&self) -> Self { Self { inner: self.inner.clone(), pending: self.pending.clone() } }")
	p.P("}")
	p.P()
	p.P("impl<K: std::hash::Hash + Eq + Clone + Send + Sync + 'static, E: serde::Serialize, R: Clone + Send + 'static> WsCallSink<K, E, R> {")
	p.P("pub async fn send(&self, value: E) -> Result<(), String> {")
	p.P("self.inner.send(value).await")
	p.P("}")
	p.P()
	p.P("// call sends value, then awaits a request-direction frame carrying the")
	p.P("// matching @ws_id (resolved by the read loop) or the connection closing.")
	p.P("pub async fn call(&self, id: K, value: E) -> Result<R, String> {")
	p.P("let reply = self.pending.register(id.clone()).await;")
	p.P("self.send(value).await?;")
	p.P(`reply.await.map_err(|_| "websocket closed while awaiting reply".to_string())`)
	p.P("}")
	p.P("}")
	p.Blank()
}

// writeWSPendingType emits the correlation-map runtime shared by every
// @ws_id-using type in the file: register(id) hands back a oneshot receiver
// that resolve(id, value) fulfills exactly once, so a concurrent call can
// await a specific reply among many interleaved frames. Emitted once by
// whichever of WriteWSServerRuntime/WriteWSClientRuntime runs for a given
// file - never both, since (unlike Go) client.rs and server.rs share no
// coherence-sensitive scope here, but a file only ever generates one side.
func writeWSPendingType(p *Printer) {
	p.P("// WsPending tracks in-flight correlated WebSocket calls, keyed by an")
	p.P("// application-supplied @ws_id value, so multiple calls can be")
	p.P("// outstanding at once on a single connection and resolved out of order.")
	p.P("pub struct WsPending<K, T> {")
	p.P("waiters: std::sync::Arc<tokio::sync::Mutex<std::collections::HashMap<K, tokio::sync::oneshot::Sender<T>>>>,")
	p.P("}")
	p.P()
	p.P("impl<K, T> Clone for WsPending<K, T> {")
	p.P("fn clone(&self) -> Self { Self { waiters: self.waiters.clone() } }")
	p.P("}")
	p.P()
	p.P("impl<K: std::hash::Hash + Eq, T> WsPending<K, T> {")
	p.P("pub fn new() -> Self {")
	p.P("Self { waiters: std::sync::Arc::new(tokio::sync::Mutex::new(std::collections::HashMap::new())) }")
	p.P("}")
	p.P()
	p.P("pub async fn register(&self, id: K) -> tokio::sync::oneshot::Receiver<T> {")
	p.P("let (tx, rx) = tokio::sync::oneshot::channel();")
	p.P("self.waiters.lock().await.insert(id, tx);")
	p.P("rx")
	p.P("}")
	p.P()
	p.P("pub async fn resolve(&self, id: &K, value: T) -> bool {")
	p.P("let sender = self.waiters.lock().await.remove(id);")
	p.P("match sender {")
	p.P("Some(sender) => sender.send(value).is_ok(),")
	p.P("None => false,")
	p.P("}")
	p.P("}")
	p.P()
	p.P("pub async fn cancel(&self, id: &K) {")
	p.P("self.waiters.lock().await.remove(id);")
	p.P("}")
	p.P()
	p.P("pub async fn close_all(&self) {")
	p.P("self.waiters.lock().await.clear();")
	p.P("}")
	p.P("}")
	p.P()
	p.P("impl<K: std::hash::Hash + Eq, T> Default for WsPending<K, T> {")
	p.P("fn default() -> Self { Self::new() }")
	p.P("}")
	p.Blank()
}

// --- client ---------------------------------------------------------------

// WriteWSClientRuntime emits the client-side runtime shared by every @ws
// method in the file: WsFrameSocket (typed send/receive/close over a mutex-
// guarded split sink/stream - fixes the pre-existing lack of write
// serialization, and now actually encodes/decodes instead of passing raw
// strings), and, when any method uses @ws_id, WsCallSocket (adds Call and a
// background reader task routing correlated replies away from receive()).
func WriteWSClientRuntime(p *Printer, hasWSCorrelation bool) {
	p.P("type WsStream = tokio_tungstenite::WebSocketStream<tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>>;")
	p.Blank()
	p.P("pub struct WsFrameSocket<In, Out> {")
	p.P("sink: std::sync::Arc<tokio::sync::Mutex<futures_util::stream::SplitSink<WsStream, tokio_tungstenite::tungstenite::Message>>>,")
	p.P("stream: std::sync::Arc<tokio::sync::Mutex<futures_util::stream::SplitStream<WsStream>>>,")
	p.P("_marker: std::marker::PhantomData<(In, Out)>,")
	p.P("}")
	p.P()
	p.P("impl<In, Out> Clone for WsFrameSocket<In, Out> {")
	p.P("fn clone(&self) -> Self { Self { sink: self.sink.clone(), stream: self.stream.clone(), _marker: std::marker::PhantomData } }")
	p.P("}")
	p.P()
	p.P("impl<In: serde::Serialize, Out: serde::de::DeserializeOwned> WsFrameSocket<In, Out> {")
	p.P("pub fn new(stream: WsStream) -> Self {")
	p.P("use futures_util::StreamExt;")
	p.P("let (sink, stream) = stream.split();")
	p.P("Self { sink: std::sync::Arc::new(tokio::sync::Mutex::new(sink)), stream: std::sync::Arc::new(tokio::sync::Mutex::new(stream)), _marker: std::marker::PhantomData }")
	p.P("}")
	p.P()
	p.P("pub async fn send(&self, value: &In) -> Result<(), String> {")
	p.P("let data = serde_json::to_string(value).map_err(|error| error.to_string())?;")
	p.P("use futures_util::SinkExt;")
	p.P("self.sink.lock().await.send(tokio_tungstenite::tungstenite::Message::text(data)).await.map_err(|error| error.to_string())")
	p.P("}")
	p.P()
	p.P("pub async fn receive(&self) -> Option<Result<Out, String>> {")
	p.P("use futures_util::StreamExt;")
	p.P("match self.stream.lock().await.next().await {")
	p.P("Some(Ok(message)) => Some(serde_json::from_str::<Out>(&message.into_text().unwrap_or_default()).map_err(|error| error.to_string())),")
	p.P("Some(Err(error)) => Some(Err(error.to_string())),")
	p.P("None => None,")
	p.P("}")
	p.P("}")
	p.P()
	p.P("pub async fn close(&self) {")
	p.P("use futures_util::SinkExt;")
	p.P("let _ = self.sink.lock().await.close().await;")
	p.P("}")
	p.P("}")
	p.Blank()
	if !hasWSCorrelation {
		return
	}
	writeWSPendingType(p)
	p.P("pub struct WsCallSocket<K, In, Out> {")
	p.P("socket: WsFrameSocket<In, Out>,")
	p.P("pending: WsPending<K, Out>,")
	p.P("inbox: std::sync::Arc<tokio::sync::Mutex<tokio::sync::mpsc::UnboundedReceiver<Out>>>,")
	p.P("}")
	p.P()
	p.P("impl<K, In, Out> Clone for WsCallSocket<K, In, Out> {")
	p.P("fn clone(&self) -> Self { Self { socket: self.socket.clone(), pending: self.pending.clone(), inbox: self.inbox.clone() } }")
	p.P("}")
	p.P()
	p.P("impl<K: std::hash::Hash + Eq + Clone + Send + Sync + 'static, In: serde::Serialize + Send + Sync + 'static, Out: serde::de::DeserializeOwned + WsCorrelated<K> + Send + Sync + 'static> WsCallSocket<K, In, Out> {")
	p.P("pub fn new(stream: WsStream) -> Self {")
	p.P("let socket = WsFrameSocket::<In, Out>::new(stream);")
	p.P("let pending = WsPending::new();")
	p.P("let (tx, rx) = tokio::sync::mpsc::unbounded_channel();")
	p.P("let reader_socket = socket.clone();")
	p.P("let reader_pending = pending.clone();")
	p.P("tokio::spawn(async move {")
	p.Indent()
	p.P("while let Some(result) = reader_socket.receive().await {")
	p.Indent()
	p.P("let Ok(frame) = result else { break; };")
	p.P("if let Some(id) = frame.ws_id() {")
	p.Indent()
	p.P("if reader_pending.resolve(&id, frame).await { continue; }")
	p.Dedent()
	p.P("continue;")
	p.Dedent()
	p.P("}")
	p.P("let _ = tx.send(frame);")
	p.Dedent()
	p.P("}")
	p.Dedent()
	p.P("});")
	p.P("Self { socket, pending, inbox: std::sync::Arc::new(tokio::sync::Mutex::new(rx)) }")
	p.P("}")
	p.P()
	p.P("pub async fn send(&self, value: &In) -> Result<(), String> {")
	p.P("self.socket.send(value).await")
	p.P("}")
	p.P()
	p.P("// receive() returns frames that were NOT claimed by a pending call().")
	p.P("pub async fn receive(&self) -> Option<Out> {")
	p.P("self.inbox.lock().await.recv().await")
	p.P("}")
	p.P()
	p.P("// call sends value, then awaits a response-direction frame carrying the")
	p.P("// matching @ws_id. Safe alongside receive(): the background reader task")
	p.P("// routes correlated replies here and everything else to it.")
	p.P("pub async fn call(&self, id: K, value: &In) -> Result<Out, String> {")
	p.P("let reply = self.pending.register(id).await;")
	p.P("self.send(value).await?;")
	p.P(`reply.await.map_err(|_| "websocket closed while awaiting reply".to_string())`)
	p.P("}")
	p.P()
	p.P("pub async fn close(&self) { self.socket.close().await; }")
	p.P("}")
	p.Blank()
}

func writeRustWSClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	wsPath, _ := m.WebSocketPath()
	fullPath := s.BasePath + wsPath
	methodName := RustIdent(m.Name)
	requestType := p.MessageTypeName(m.Request)
	responseType := p.MessageTypeName(m.Response)
	errorName := clientErrorName(s, m)
	idField, correlated := m.WSIDField()

	socketName, socketArgs := "WsFrameSocket", requestType+", "+responseType
	if correlated {
		kType := RustScalarType(idField.Type.Scalar)
		socketName, socketArgs = "WsCallSocket", kType+", "+requestType+", "+responseType
	}
	socketType := socketName + "<" + socketArgs + ">"
	// Expression position needs turbofish (`Type::<Args>::method`) - plain
	// `Type<Args>::method` only parses in type position.
	socketConstructor := socketName + "::<" + socketArgs + ">"

	p.P("pub async fn ", methodName, "(&self, req: &", requestType, ") -> Result<", socketType, ", ", errorName, "> {")
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
	p.P("let (stream, _) = tokio_tungstenite::connect_async(url).await.map_err(", errorName, "::WsTransport)?;")
	p.P("Ok(", socketConstructor, "::new(stream))")
	p.Dedent()
	p.P("}")
	p.Blank()
}
