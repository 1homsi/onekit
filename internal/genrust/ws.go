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
	p.P("axum::Extension(ws_options): axum::Extension<WsServerOptions>,")
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
	p.P("let ws = ws.max_message_size(ws_options.max_frame_bytes).max_frame_size(ws_options.max_frame_bytes);")
	p.P("ws.on_upgrade(move |socket: axum::extract::ws::WebSocket| async move {")
	p.Indent()
	p.P("use futures_util::StreamExt;")
	p.P("let (sink, mut stream) = socket.split();")
	p.P("let sink = std::sync::Arc::new(tokio::sync::Mutex::new(sink));")
	p.P("let (closed_tx, closed_rx) = tokio::sync::watch::channel(false);")
	if correlated {
		kType := RustScalarType(idField.Type.Scalar)
		p.P("let out = WsCallSink::<", kType, ", ", responseRef, ", ", requestRef, "> { inner: WsSink { sink: sink.clone(), closed: closed_rx, _marker: std::marker::PhantomData }, pending: WsPending::new() };")
	} else {
		p.P("let out = WsSink::<", responseRef, "> { sink: sink.clone(), closed: closed_rx, _marker: std::marker::PhantomData };")
	}
	writeWSServerReadLoop(p, method, requestRef, correlated)
	p.P("let _ = closed_tx.send(true);")
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
	p.P("closed: tokio::sync::watch::Receiver<bool>,")
	p.P("_marker: std::marker::PhantomData<E>,")
	p.P("}")
	p.P()
	p.P("impl<E> Clone for WsSink<E> {")
	p.P("fn clone(&self) -> Self { Self { sink: self.sink.clone(), closed: self.closed.clone(), _marker: std::marker::PhantomData } }")
	p.P("}")
	p.P()
	writeWSSinkLifecycle(p)
	if hasWSCorrelation {
		writeWSCallRuntime(p)
	}
	writeWSFrameLimitConst(p)
	writeWSServerHelpers(p)
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
	p.P("impl<K: std::hash::Hash + Eq + Clone + Send + Sync + 'static, E: serde::Serialize + WsCorrelated<K> + Send + Sync + 'static, R: Clone + Send + 'static> WsCallSink<K, E, R> {")
	p.P("pub async fn send(&self, value: E) -> Result<(), String> {")
	p.P("self.inner.send(value).await")
	p.P("}")
	p.P()
	p.P("pub fn is_closed(&self) -> bool { self.inner.is_closed() }")
	p.P()
	p.P("pub async fn closed(&self) { self.inner.closed().await }")
	p.P()
	p.P("// call sends value, then awaits a request-direction frame carrying the")
	p.P("// matching @ws_id (resolved by the read loop) or the connection closing.")
	p.P("// Dropping the future first releases the waiter and sends the peer the")
	p.P("// schema's @ws_cancel frame, if it declares one.")
	p.P("pub async fn call(&self, id: K, value: E) -> Result<R, WsCallError> {")
	p.P("let reply = self.start(id.clone(), value).await?;")
	p.P("let mut guard = self.guard(id);")
	p.P("let result = reply.await;")
	p.P("guard.on_drop = None;")
	p.P("result.map_err(|_| WsCallError::Closed)")
	p.P("}")
	p.P()
	p.P("// call_timeout is call bounded by timeout: past it, the peer is sent the")
	p.P("// @ws_cancel frame (before this returns, so it precedes anything the")
	p.P("// caller sends next) and TimedOut returned.")
	p.P("pub async fn call_timeout(&self, id: K, value: E, timeout: std::time::Duration) -> Result<R, WsCallError> {")
	p.P("let reply = self.start(id.clone(), value).await?;")
	p.P("let mut guard = self.guard(id.clone());")
	p.P("let result = tokio::time::timeout(timeout, reply).await;")
	p.P("guard.on_drop = None;")
	p.P("match result {")
	p.P("Ok(result) => result.map_err(|_| WsCallError::Closed),")
	p.P("Err(_) => { self.abandon(id).await; Err(WsCallError::TimedOut) }")
	p.P("}")
	p.P("}")
	p.P()
	p.P("async fn start(&self, id: K, value: E) -> Result<tokio::sync::oneshot::Receiver<R>, WsCallError> {")
	p.P("let Some(reply) = self.pending.register(id.clone(), value.ws_variant()).await else { return Err(WsCallError::Closed); };")
	p.P("if let Err(error) = self.send(value).await { self.pending.cancel(&id).await; return Err(WsCallError::Send(error)); }")
	p.P("Ok(reply)")
	p.P("}")
	p.P()
	p.P("async fn abandon(&self, id: K) {")
	p.P("self.pending.cancel(&id).await;")
	p.P("if let Some(frame) = E::ws_cancel(id) { let _ = self.send(frame).await; }")
	p.P("}")
	p.P()
	p.P("// guard does abandon's work if the call future is dropped mid-wait; it")
	p.P("// can't await in Drop, so it spawns.")
	p.P("fn guard(&self, id: K) -> WsCallGuard {")
	p.P("let (pending, sink) = (self.pending.clone(), self.inner.clone());")
	p.P("WsCallGuard { on_drop: Some(Box::new(move || {")
	p.P("let Ok(runtime) = tokio::runtime::Handle::try_current() else { return; };")
	p.P("runtime.spawn(async move {")
	p.P("pending.cancel(&id).await;")
	p.P("if let Some(frame) = E::ws_cancel(id) { let _ = sink.send(frame).await; }")
	p.P("});")
	p.P("})) }")
	p.P("}")
	p.P("}")
	p.Blank()
}

// writeWSCallRuntime emits WsCallError and the drop guard behind call():
// shared by both runtimes, each of which lives in its own module.
func writeWSCallRuntime(p *Printer) {
	p.P("// WsCallError is why a correlated call() did not produce a reply.")
	p.P("#[derive(Debug, Clone, PartialEq, Eq)]")
	p.P("pub enum WsCallError {")
	p.P("// The connection closed (or was already closed) before the reply.")
	p.P("Closed,")
	p.P("// call_timeout's deadline passed first.")
	p.P("TimedOut,")
	p.P("// Sending the frame failed.")
	p.P("Send(String),")
	p.P("}")
	p.P()
	p.P("impl std::fmt::Display for WsCallError {")
	p.P("fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {")
	p.P("match self {")
	p.P(`WsCallError::Closed => write!(f, "websocket closed"),`)
	p.P(`WsCallError::TimedOut => write!(f, "websocket call timed out"),`)
	p.P(`WsCallError::Send(error) => write!(f, "websocket send failed: {error}"),`)
	p.P("}")
	p.P("}")
	p.P("}")
	p.P()
	p.P("impl std::error::Error for WsCallError {}")
	p.P()
	p.P("// WsCallGuard runs its hook if a call() future is dropped before the reply")
	p.P("// arrives - by call_timeout, tokio::select!, or the caller giving up - so the")
	p.P("// waiter is released and the peer is sent the @ws_cancel frame.")
	p.P("struct WsCallGuard {")
	p.P("on_drop: Option<Box<dyn FnOnce() + Send>>,")
	p.P("}")
	p.P()
	p.P("impl Drop for WsCallGuard {")
	p.P("fn drop(&mut self) { if let Some(hook) = self.on_drop.take() { hook(); } }")
	p.P("}")
	p.Blank()
}

func writeWSFrameLimitConst(p *Printer) {
	p.P("// Inbound message cap applied unless configured otherwise; the same default")
	p.P("// every onekit target uses.")
	p.P("pub const DEFAULT_MAX_WS_FRAME_BYTES: usize = 16 << 20;")
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
	p.P("// None once close_all has run: closed is sticky, so a call() made after the")
	p.P("// connection is gone fails immediately instead of awaiting forever.")
	p.P("// Each waiter keeps the oneof variant tag its call sent (\"\" when not a")
	p.P("// oneof); see resolve.")
	p.P("waiters: std::sync::Arc<tokio::sync::Mutex<Option<std::collections::HashMap<K, (tokio::sync::oneshot::Sender<T>, &'static str)>>>>,")
	p.P("}")
	p.P()
	p.P("impl<K, T> Clone for WsPending<K, T> {")
	p.P("fn clone(&self) -> Self { Self { waiters: self.waiters.clone() } }")
	p.P("}")
	p.P()
	p.P("impl<K: std::hash::Hash + Eq, T> WsPending<K, T> {")
	p.P("pub fn new() -> Self {")
	p.P("Self { waiters: std::sync::Arc::new(tokio::sync::Mutex::new(Some(std::collections::HashMap::new()))) }")
	p.P("}")
	p.P()
	p.P("pub async fn register(&self, id: K, sent: &'static str) -> Option<tokio::sync::oneshot::Receiver<T>> {")
	p.P("let mut guard = self.waiters.lock().await;")
	p.P("let waiters = guard.as_mut()?;")
	p.P("let (tx, rx) = tokio::sync::oneshot::channel();")
	p.P("waiters.insert(id, (tx, sent));")
	p.P("Some(rx)")
	p.P("}")
	p.P()
	p.P("// resolve hands value back when no call is waiting on id - or when value")
	p.P("// is the same oneof variant that call sent, which is the peer starting its")
	p.P("// own call under a colliding id rather than a reply - so the caller can")
	p.P("// still dispatch it as an ordinary inbound frame.")
	p.P("pub async fn resolve(&self, id: &K, variant: &str, value: T) -> Option<T> {")
	p.P("let mut guard = self.waiters.lock().await;")
	p.P("let Some(waiters) = guard.as_mut() else { return Some(value); };")
	p.P("match waiters.get(id) {")
	p.P("Some((_, sent)) if sent.is_empty() || *sent != variant => {}")
	p.P("_ => return Some(value),")
	p.P("}")
	p.P("if let Some((sender, _)) = waiters.remove(id) { let _ = sender.send(value); }")
	p.P("None")
	p.P("}")
	p.P()
	p.P("pub async fn cancel(&self, id: &K) {")
	p.P("if let Some(waiters) = self.waiters.lock().await.as_mut() { waiters.remove(id); }")
	p.P("}")
	p.P()
	p.P("pub async fn close_all(&self) {")
	p.P("self.waiters.lock().await.take();")
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
	if hasWSCorrelation {
		writeWSCallRuntime(p)
	}
	writeWSFrameLimitConst(p)
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
	p.P("use tokio_tungstenite::tungstenite::Message;")
	p.P("let mut stream = self.stream.lock().await;")
	p.P("loop {")
	p.P("return match stream.next().await? {")
	p.P("Ok(Message::Text(text)) => Some(serde_json::from_str::<Out>(&text).map_err(|error| error.to_string())),")
	p.P("Ok(Message::Binary(bytes)) => Some(serde_json::from_slice::<Out>(&bytes).map_err(|error| error.to_string())),")
	p.P("Ok(Message::Close(_)) => None,")
	p.P("Ok(_) => continue,")
	p.P("Err(error) => Some(Err(error.to_string())),")
	p.P("};")
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
	writeWSCallSocketType(p)
}

// writeWSCallSocketType emits WsCallSocket, split out of WriteWSClientRuntime
// to keep both functions under the linter's statement-count limit.
func writeWSCallSocketType(p *Printer) {
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
	p.P("impl<K: std::hash::Hash + Eq + Clone + Send + Sync + 'static, In: serde::Serialize + WsCorrelated<K> + Send + Sync + 'static, Out: serde::de::DeserializeOwned + WsCorrelated<K> + Send + Sync + 'static> WsCallSocket<K, In, Out> {")
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
	p.P("let frame = match frame.ws_id() {")
	p.Indent()
	p.P("Some(id) => { let variant = frame.ws_variant(); match reader_pending.resolve(&id, variant, frame).await { Some(frame) => frame, None => continue } }")
	p.P("None => frame,")
	p.Dedent()
	p.P("};")
	p.P("let _ = tx.send(frame);")
	p.Dedent()
	p.P("}")
	p.P("reader_pending.close_all().await;")
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
	writeWSCallSocketCallMethods(p)
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
	p.P("let config = tokio_tungstenite::tungstenite::protocol::WebSocketConfig::default().max_message_size(Some(self.max_ws_frame_bytes)).max_frame_size(Some(self.max_ws_frame_bytes));")
	p.P("let (stream, _) = tokio_tungstenite::connect_async_with_config(url, Some(config), false).await.map_err(", errorName, "::WsTransport)?;")
	p.P("Ok(", socketConstructor, "::new(stream))")
	p.Dedent()
	p.P("}")
	p.Blank()
}

// writeWSCallSocketCallMethods emits WsCallSocket's call/call_timeout and
// their helpers, split out of writeWSCallSocketType for the linter's
// statement-count limit.
func writeWSCallSocketCallMethods(p *Printer) {
	p.P("// call sends value, then awaits a response-direction frame carrying the")
	p.P("// matching @ws_id. Safe alongside receive(): the background reader task")
	p.P("// routes correlated replies here and everything else to it. Dropping the")
	p.P("// future first releases the waiter and sends the peer the schema's")
	p.P("// @ws_cancel frame, if it declares one.")
	p.P("pub async fn call(&self, id: K, value: &In) -> Result<Out, WsCallError> {")
	p.P("let reply = self.start(id.clone(), value).await?;")
	p.P("let mut guard = self.guard(id);")
	p.P("let result = reply.await;")
	p.P("guard.on_drop = None;")
	p.P("result.map_err(|_| WsCallError::Closed)")
	p.P("}")
	p.P()
	p.P("// call_timeout is call bounded by timeout: past it, the peer is sent the")
	p.P("// @ws_cancel frame (before this returns, so it precedes anything the")
	p.P("// caller sends next) and TimedOut returned.")
	p.P("pub async fn call_timeout(&self, id: K, value: &In, timeout: std::time::Duration) -> Result<Out, WsCallError> {")
	p.P("let reply = self.start(id.clone(), value).await?;")
	p.P("let mut guard = self.guard(id.clone());")
	p.P("let result = tokio::time::timeout(timeout, reply).await;")
	p.P("guard.on_drop = None;")
	p.P("match result {")
	p.P("Ok(result) => result.map_err(|_| WsCallError::Closed),")
	p.P("Err(_) => { self.abandon(id).await; Err(WsCallError::TimedOut) }")
	p.P("}")
	p.P("}")
	p.P()
	p.P("async fn start(&self, id: K, value: &In) -> Result<tokio::sync::oneshot::Receiver<Out>, WsCallError> {")
	p.P("let Some(reply) = self.pending.register(id.clone(), value.ws_variant()).await else { return Err(WsCallError::Closed); };")
	p.P("if let Err(error) = self.send(value).await { self.pending.cancel(&id).await; return Err(WsCallError::Send(error)); }")
	p.P("Ok(reply)")
	p.P("}")
	p.P()
	p.P("async fn abandon(&self, id: K) {")
	p.P("self.pending.cancel(&id).await;")
	p.P("if let Some(frame) = In::ws_cancel(id) { let _ = self.send(&frame).await; }")
	p.P("}")
	p.P()
	p.P("// guard does abandon's work if the call future is dropped mid-wait; it")
	p.P("// can't await in Drop, so it spawns.")
	p.P("fn guard(&self, id: K) -> WsCallGuard {")
	p.P("let (pending, socket) = (self.pending.clone(), self.socket.clone());")
	p.P("WsCallGuard { on_drop: Some(Box::new(move || {")
	p.P("let Ok(runtime) = tokio::runtime::Handle::try_current() else { return; };")
	p.P("runtime.spawn(async move {")
	p.P("pending.cancel(&id).await;")
	p.P("if let Some(frame) = In::ws_cancel(id) { let _ = socket.send(&frame).await; }")
	p.P("});")
	p.P("})) }")
	p.P("}")
}

// writeWSServerHelpers emits WsServerOptions and ws_close, split out of
// WriteWSServerRuntime for the linter's statement-count limit.
func writeWSServerHelpers(p *Printer) {
	p.P("// WsServerOptions configures a router's @ws routes; see")
	p.P("// <service>_router_with_ws_options.")
	p.P("#[derive(Debug, Clone, Copy)]")
	p.P("pub struct WsServerOptions {")
	p.P("// Cap on one inbound message; a larger one ends the connection.")
	p.P("pub max_frame_bytes: usize,")
	p.P("pub ping_interval: Option<std::time::Duration>,")
	p.P("}")
	p.P()
	p.P("impl Default for WsServerOptions {")
	p.P("fn default() -> Self { Self { max_frame_bytes: DEFAULT_MAX_WS_FRAME_BYTES, ping_interval: Some(std::time::Duration::from_secs(30)) } }")
	p.P("}")
	p.P()
	p.P("// ws_close ends the connection with code and message as the reason, cut")
	p.P("// to the close frame's 123-byte limit on a char boundary.")
	p.P("async fn ws_close(sink: &std::sync::Arc<tokio::sync::Mutex<futures_util::stream::SplitSink<axum::extract::ws::WebSocket, axum::extract::ws::Message>>>, code: u16, mut reason: String) {")
	p.P("if reason.len() > 123 {")
	p.P("let mut cut = 123;")
	p.P("while !reason.is_char_boundary(cut) { cut -= 1; }")
	p.P("reason.truncate(cut);")
	p.P("}")
	p.P("use futures_util::SinkExt;")
	p.P("let _ = sink.lock().await.send(axum::extract::ws::Message::Close(Some(axum::extract::ws::CloseFrame { code, reason: reason.into() }))).await;")
	p.P("}")
	p.P()
}

func writeWSSinkLifecycle(p *Printer) {
	p.P("impl<E> WsSink<E> {")
	p.P("pub fn is_closed(&self) -> bool { *self.closed.borrow() }")
	p.P()
	p.P("pub async fn closed(&self) {")
	p.P("let mut closed = self.closed.clone();")
	p.P("let _ = closed.wait_for(|closed| *closed).await;")
	p.P("}")
	p.P("}")
	p.P()
	p.P("async fn ws_tick(ping: &mut Option<tokio::time::Interval>) {")
	p.P("match ping {")
	p.P("Some(interval) => { interval.tick().await; }")
	p.P("None => std::future::pending::<()>().await,")
	p.P("}")
	p.P("}")
	p.P()
}

func writeWSServerReadLoop(p *Printer, method *onkir.Method, requestRef string, correlated bool) {
	p.P("let mut ping = ws_options.ping_interval.map(|every| tokio::time::interval_at(tokio::time::Instant::now() + every, every));")
	p.P("let mut awaiting_pong = false;")
	p.P("loop {")
	p.Indent()
	p.P("let next = tokio::select! {")
	p.P("message = stream.next() => Some(message),")
	p.P("_ = ws_tick(&mut ping) => None,")
	p.P("};")
	p.P("let Some(message) = next else {")
	p.P("if awaiting_pong { break; }")
	p.P("awaiting_pong = true;")
	p.P("use futures_util::SinkExt;")
	p.P("let _ = sink.lock().await.send(axum::extract::ws::Message::Ping(Default::default())).await;")
	p.P("continue;")
	p.P("};")
	p.P("let Some(Ok(message)) = message else { break; };")
	p.P("awaiting_pong = false;")
	p.P("let decoded = match message {")
	p.P("axum::extract::ws::Message::Text(text) => serde_json::from_str::<", requestRef, ">(&text),")
	p.P("axum::extract::ws::Message::Binary(bytes) => serde_json::from_slice::<", requestRef, ">(&bytes),")
	p.P("axum::extract::ws::Message::Close(_) => break,")
	p.P("_ => continue,")
	p.P("};")
	p.P("let frame = match decoded {")
	p.Indent()
	p.P("Ok(frame) => frame,")
	p.P(`Err(_) => { ws_close(&sink, 1007, "invalid JSON frame".to_string()).await; break; }`)
	p.Dedent()
	p.P("};")
	p.P("if let Err(error) = frame.validate() { ws_close(&sink, 1007, error.to_string()).await; break; }")
	if correlated {
		p.P("let frame = match frame.ws_id() {")
		p.Indent()
		p.P("Some(id) => { let variant = frame.ws_variant(); match out.pending.resolve(&id, variant, frame).await { Some(frame) => frame, None => continue } }")
		p.P("None => frame,")
		p.Dedent()
		p.P("};")
	}
	p.P("if let Err(error) = service.", RustIdent(method.Name), "(context.clone(), frame, out.clone()).await {")
	p.Indent()
	p.P("ws_close(&sink, 1011, error.to_string()).await;")
	p.P("break;")
	p.Dedent()
	p.P("}")
	p.Dedent()
	p.P("}")
}
