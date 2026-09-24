package genpy

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

func writePyWSRuntime(p *Printer) {
	p.P(pyWSRuntimeSource)
}

func (p *Printer) isExternal(m *onkir.Message) bool {
	if p.resolver == nil {
		return false
	}
	_, ok := p.resolver.ResolveMessage(m)
	return ok
}

func fileMessagesDeep(file *onkir.File) []*onkir.Message {
	var out []*onkir.Message
	var walk func(ms []*onkir.Message)
	walk = func(ms []*onkir.Message) {
		for _, m := range ms {
			out = append(out, m)
			walk(m.Nested)
		}
	}
	walk(file.Messages)
	return out
}

func wsFrameMessages(p *Printer, file *onkir.File) []*onkir.Message {
	seen := map[*onkir.Message]bool{}
	var out []*onkir.Message
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if !m.IsWebSocket() {
				continue
			}
			for _, frame := range []*onkir.Message{m.Request, m.Response} {
				if frame != nil && !seen[frame] && !p.isExternal(frame) {
					seen[frame] = true
					out = append(out, frame)
				}
			}
		}
	}
	return out
}

func pyWireInt(f *onkir.Field, expr string) string {
	if f.Type != nil && f.Type.Kind == onkir.KindScalar && (f.Type.Scalar == onkir.ScalarInt64 || f.Type.Scalar == onkir.ScalarUint64) {
		if v, _ := fieldEncodeValue(f); v != "number" {
			return "str(" + expr + ")"
		}
	}
	return expr
}

func pyIDValue(f *onkir.Field, expr string) string {
	if f.Type != nil && f.Type.Kind == onkir.KindScalar && f.Type.Scalar != onkir.ScalarString {
		return "int(" + expr + ")"
	}
	return expr
}

func pyVariantTarget(f *onkir.Field, v *onkir.OneofVariant) string {
	if f.Oneof.Flatten() {
		return "o"
	}
	return fmt.Sprintf("o.get(%q)", v.Name)
}

func pyOneofMatch(f *onkir.Field, v *onkir.OneofVariant) string {
	return fmt.Sprintf("isinstance(o, dict) and o.get(%q) == %q", oneofDiscriminator(f), v.Tag())
}

func oneofDiscriminator(f *onkir.Field) string {
	if disc, ok := f.Oneof.Discriminator(); ok && disc != "" {
		return disc
	}
	return "type"
}

func writePyWSCodecs(p *Printer, file *onkir.File) {
	for _, m := range fileMessagesDeep(file) {
		if onkir.MessageHasRaw(m, p.isExternal) {
			writePyRawFuncs(p, m)
		}
	}
	for _, m := range wsFrameMessages(p, file) {
		writePyFrameFuncs(p, m)
		writePyCodecClass(p, m)
	}
}

func writePyRawFuncs(p *Printer, m *onkir.Message) {
	steps := onkir.RawSteps(m, p.isExternal)
	p.P("def _ws_split_raw_", m.Name, "(d, raw):")
	p.Indent()
	p.P("d = dict(d)")
	for _, step := range steps {
		key := fmt.Sprintf("%q", step.Field.Name)
		switch {
		case step.Variant != nil:
			p.P("o = d.get(", key, ")")
			if step.Field.Oneof.Flatten() {
				p.P("if ", pyOneofMatch(step.Field, step.Variant), ":")
				p.Indent()
				p.P("d[", key, "] = _ws_split_raw_", step.Child.Name, "(o, raw)")
				p.Dedent()
				continue
			}
			vKey := fmt.Sprintf("%q", step.Variant.Name)
			p.P("if ", pyOneofMatch(step.Field, step.Variant), " and o.get(", vKey, ") is not None:")
			p.Indent()
			p.P("o = dict(o)")
			p.P("o[", vKey, "] = _ws_split_raw_", step.Child.Name, "(o[", vKey, "], raw)")
			p.P("d[", key, "] = o")
			p.Dedent()
		case step.Child != nil && step.Field.Repeated:
			p.P("if d.get(", key, ") is not None:")
			p.Indent()
			p.P("d[", key, "] = [_ws_split_raw_", step.Child.Name, "(item, raw) if item is not None else None for item in d[", key, "]]")
			p.Dedent()
		case step.Child != nil:
			p.P("if d.get(", key, ") is not None:")
			p.Indent()
			p.P("d[", key, "] = _ws_split_raw_", step.Child.Name, "(d[", key, "], raw)")
			p.Dedent()
		case step.Field.Type.Scalar == onkir.ScalarBytes:
			p.P("v = d.pop(", key, ", None)")
			p.P(`raw.append(_ws_base64.b64decode(v) if v else b"")`)
		default:
			p.P("raw.append((d.get(", key, `) or "").encode("utf-8"))`)
			p.P("d[", key, `] = ""`)
		}
	}
	p.P("return d")
	p.Dedent()
	p.Blank()
	p.P("def _ws_join_raw_", m.Name, "(d, it):")
	p.Indent()
	for _, step := range steps {
		key := fmt.Sprintf("%q", step.Field.Name)
		switch {
		case step.Variant != nil:
			p.P("o = d.get(", key, ")")
			target := pyVariantTarget(step.Field, step.Variant)
			p.P("if ", pyOneofMatch(step.Field, step.Variant), " and ", target, " is not None and not _ws_join_raw_", step.Child.Name, "(", target, ", it):")
			p.Indent()
			p.P("return False")
			p.Dedent()
		case step.Child != nil && step.Field.Repeated:
			p.P("for item in d.get(", key, ") or []:")
			p.Indent()
			p.P("if item is not None and not _ws_join_raw_", step.Child.Name, "(item, it):")
			p.Indent()
			p.P("return False")
			p.Dedent()
			p.Dedent()
		case step.Child != nil:
			p.P("if d.get(", key, ") is not None and not _ws_join_raw_", step.Child.Name, "(d[", key, "], it):")
			p.Indent()
			p.P("return False")
			p.Dedent()
		default:
			p.P("segment = next(it, None)")
			p.P("if segment is None:")
			p.Indent()
			p.P("return False")
			p.Dedent()
			if step.Field.Type.Scalar == onkir.ScalarBytes {
				p.P("d[", key, `] = _ws_base64.b64encode(segment).decode("ascii")`)
			} else {
				p.P("d[", key, `] = segment.decode("utf-8")`)
			}
		}
	}
	p.P("return True")
	p.Dedent()
	p.Blank()
}

func writePyFrameFuncs(p *Printer, m *onkir.Message) {
	idField, correlated := wsIDOf(m)
	if correlated {
		writePyReply(p, m)
		writePyVariant(p, m)
	}
	if _, _, cancelID, ok := onkir.WSCancelVariant(m); ok {
		f, v, _, _ := onkir.WSCancelVariant(m)
		p.P("def _ws_cancel_", m.Name, "(call_id):")
		p.Indent()
		value := pyWireInt(cancelID, "call_id")
		disc := oneofDiscriminator(f)
		if f.Oneof.Flatten() {
			p.P(fmt.Sprintf("return {%q: {%q: %q, %q: %s}}", f.Name, disc, v.Tag(), cancelID.Name, value))
		} else {
			p.P(fmt.Sprintf("return {%q: {%q: %q, %q: {%q: %s}}}", f.Name, disc, v.Tag(), v.Name, cancelID.Name, value))
		}
		p.Dedent()
		p.Blank()
	}
	if onkir.MessageHasWSTimeout(m) {
		writePyWithTimeout(p, m)
	}
	_ = idField
}

func wsIDOf(m *onkir.Message) (*onkir.Field, bool) {
	return onkir.WSIDField(m)
}

func writePyReply(p *Printer, m *onkir.Message) {
	p.P("def _ws_reply_", m.Name, "(d):")
	p.Indent()
	if f := onkir.FindWSIDDirect(m); f != nil {
		p.P("if ", fmt.Sprintf("%q", f.Name), " in d:")
		p.Indent()
		p.P("return (", pyIDValue(f, fmt.Sprintf("d[%q]", f.Name)), `, "")`)
		p.Dedent()
	}
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		p.P("o = d.get(", fmt.Sprintf("%q", f.Name), ")")
		for _, v := range f.Oneof.Variants {
			if v.IsWSCancel() || v.Type == nil || v.Type.Kind != onkir.KindMessage {
				continue
			}
			vf := onkir.FindWSIDDirect(v.Type.Message)
			if vf == nil {
				continue
			}
			p.P("if ", pyOneofMatch(f, v), ":")
			p.Indent()
			p.P("v = ", pyVariantTarget(f, v))
			p.P("if isinstance(v, dict) and ", fmt.Sprintf("%q", vf.Name), " in v:")
			p.Indent()
			p.P("return (", pyIDValue(vf, fmt.Sprintf("v[%q]", vf.Name)), ", ", fmt.Sprintf("%q", v.Tag()), ")")
			p.Dedent()
			p.Dedent()
		}
	}
	p.P("return None")
	p.Dedent()
	p.Blank()
}

func writePyVariant(p *Printer, m *onkir.Message) {
	p.P("def _ws_variant_", m.Name, "(d):")
	p.Indent()
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		p.P("o = d.get(", fmt.Sprintf("%q", f.Name), ")")
		for _, v := range f.Oneof.Variants {
			if v.Type == nil || v.Type.Kind != onkir.KindMessage || onkir.FindWSIDDirect(v.Type.Message) == nil {
				continue
			}
			p.P("if ", pyOneofMatch(f, v), ":")
			p.Indent()
			p.P("return ", fmt.Sprintf("%q", v.Tag()))
			p.Dedent()
		}
	}
	p.P(`return ""`)
	p.Dedent()
	p.Blank()
}

func writePyWithTimeout(p *Printer, m *onkir.Message) {
	unset := "in (None, 0, \"0\", \"\")"
	p.P("def _ws_with_timeout_", m.Name, "(d, ms):")
	p.Indent()
	p.P("d = dict(d)")
	if f := onkir.WSTimeoutField(m); f != nil {
		p.P("if d.get(", fmt.Sprintf("%q", f.Name), ") ", unset, ":")
		p.Indent()
		p.P("d[", fmt.Sprintf("%q", f.Name), "] = ", pyWireInt(f, "ms"))
		p.Dedent()
	}
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, v := range f.Oneof.Variants {
			if v.Type == nil || v.Type.Kind != onkir.KindMessage {
				continue
			}
			tf := onkir.WSTimeoutField(v.Type.Message)
			if tf == nil {
				continue
			}
			p.P("o = d.get(", fmt.Sprintf("%q", f.Name), ")")
			if f.Oneof.Flatten() {
				p.P("if ", pyOneofMatch(f, v), " and o.get(", fmt.Sprintf("%q", tf.Name), ") ", unset, ":")
				p.Indent()
				p.P("o = dict(o)")
				p.P("o[", fmt.Sprintf("%q", tf.Name), "] = ", pyWireInt(tf, "ms"))
				p.P("d[", fmt.Sprintf("%q", f.Name), "] = o")
				p.Dedent()
				continue
			}
			vKey := fmt.Sprintf("%q", v.Name)
			p.P("if ", pyOneofMatch(f, v), " and isinstance(o.get(", vKey, "), dict) and o[", vKey, "].get(", fmt.Sprintf("%q", tf.Name), ") ", unset, ":")
			p.Indent()
			p.P("o = dict(o)")
			p.P("inner = dict(o[", vKey, "])")
			p.P("inner[", fmt.Sprintf("%q", tf.Name), "] = ", pyWireInt(tf, "ms"))
			p.P("o[", vKey, "] = inner")
			p.P("d[", fmt.Sprintf("%q", f.Name), "] = o")
			p.Dedent()
		}
	}
	p.P("return d")
	p.Dedent()
	p.Blank()
}

func writePyCodecClass(p *Printer, m *onkir.Message) {
	p.P("class _", m.Name, "WsCodec(_WsCodec):")
	p.Indent()
	wrote := false
	if onkir.MessageHasRaw(m, p.isExternal) {
		p.P("split = staticmethod(_ws_split_raw_", m.Name, ")")
		p.P("join = staticmethod(_ws_join_raw_", m.Name, ")")
		wrote = true
	}
	if onkir.MessageHasWSTimeout(m) {
		p.P("with_timeout = staticmethod(_ws_with_timeout_", m.Name, ")")
		wrote = true
	}
	if _, _, _, ok := onkir.WSCancelVariant(m); ok {
		p.P("cancel = staticmethod(_ws_cancel_", m.Name, ")")
		wrote = true
	}
	if _, correlated := wsIDOf(m); correlated {
		p.P("reply = staticmethod(_ws_reply_", m.Name, ")")
		p.P("variant = staticmethod(_ws_variant_", m.Name, ")")
		wrote = true
	}
	if !wrote {
		p.P("pass")
	}
	p.Dedent()
	p.Blank()
}

func pyCodecName(p *Printer, m *onkir.Message) string {
	if p.isExternal(m) {
		return "_WsCodec"
	}
	return "_" + m.Name + "WsCodec"
}

func writePyWSClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	wsPath, _ := m.WebSocketPath()
	fullPath := s.BasePath + wsPath
	_, correlated := m.WSIDField()
	socketType := "WsFrameSocket"
	if correlated {
		socketType = "WsCallSocket"
	}
	p.P("def ", SnakeCase(m.Name), "(self, req: ", p.MessageTypeName(m.Request), ") -> ", socketType, ":")
	p.Indent()
	p.P(`if hasattr(req, "validate"): req.validate()`)
	p.P(fmt.Sprintf("path = %q", fullPath))
	writePyPathParams(p, wsPath, m.Request)
	writeClientQueryParams(p, m.Request)
	p.P("connection = _ws_connect(self.base_url + path, self.headers, self.max_ws_frame_bytes, self.ws_ping_interval)")
	p.P("return ", socketType, "(connection, ", p.MessageTypeName(m.Response), ", ", pyCodecName(p, m.Request), ", ", pyCodecName(p, m.Response), ", self.max_ws_message_bytes)")
	p.Dedent()
	p.Blank()
}

const pyWSRuntimeSource = `import base64 as _ws_base64
import math as _ws_math
import queue as _ws_queue
import struct as _ws_struct
import threading as _ws_threading
import time as _ws_time
from concurrent.futures import Future as _WsFuture
from concurrent.futures import TimeoutError as _WsFutureTimeout

DEFAULT_MAX_WS_FRAME_BYTES = 16 << 20
DEFAULT_MAX_WS_MESSAGE_BYTES = 256 << 20
_WS_CHUNK_BYTES = 8 << 20
_WS_CHUNK_THRESHOLD = 16 << 20
_WS_CLOSED = object()


class WsClosedError(ConnectionError):
    def __init__(self, code=None, reason=""):
        detail = "" if code is None else " (" + str(code) + (": " + reason if reason else "") + ")"
        super().__init__("websocket closed" + detail)
        self.code = code
        self.reason = reason


class WsTimeoutError(TimeoutError):
    def __init__(self):
        super().__init__("websocket call timed out")


class WsCancelledError(Exception):
    def __init__(self):
        super().__init__("websocket call cancelled")


class _WsChunkError(Exception):
    def __init__(self, code):
        super().__init__("malformed chunked message" if code == 1007 else "chunked message exceeds the size limit")
        self.code = code


class _WsCodec:
    split = None
    join = None
    with_timeout = None
    cancel = None

    @staticmethod
    def reply(d):
        return None

    @staticmethod
    def variant(d):
        return ""


def _ws_json(d):
    return json.dumps(d, separators=(",", ":"), ensure_ascii=False)


def _ws_encode(d, split):
    if split is not None:
        raw = []
        header = split(d, raw)
        if any(raw):
            head = _ws_json(header).encode("utf-8")
            parts = [_ws_struct.pack(">I", len(head)), head, _ws_struct.pack(">I", len(raw))]
            parts.extend(_ws_struct.pack(">I", len(segment)) for segment in raw)
            parts.extend(raw)
            return b"".join(parts)
    return _ws_json(d)


def _ws_decode(data, join):
    if isinstance(data, str):
        return json.loads(data)
    if join is None:
        return json.loads(data.decode("utf-8"))
    malformed = ValueError("malformed binary frame")
    if len(data) < 4:
        raise malformed
    (head_len,) = _ws_struct.unpack_from(">I", data, 0)
    offset = 4
    if offset + head_len + 4 > len(data):
        raise malformed
    header = json.loads(data[offset:offset + head_len].decode("utf-8"))
    offset += head_len
    (count,) = _ws_struct.unpack_from(">I", data, offset)
    offset += 4
    if offset + 4 * count > len(data):
        raise malformed
    lengths = _ws_struct.unpack_from(">" + str(count) + "I", data, offset)
    offset += 4 * count
    view = memoryview(data)
    raw = []
    for n in lengths:
        if offset + n > len(data):
            raise malformed
        raw.append(bytes(view[offset:offset + n]))
        offset += n
    if offset != len(data):
        raise malformed
    it = iter(raw)
    if not join(header, it) or next(it, None) is not None:
        raise malformed
    return header


def _ws_chunks(data):
    if isinstance(data, str):
        if len(data) <= _WS_CHUNK_THRESHOLD // 4:
            return None
        payload = data.encode("utf-8")
        kind = 0
    else:
        payload = data
        kind = 1
    if len(payload) <= _WS_CHUNK_THRESHOLD:
        return None
    header = _ws_struct.pack(">IBQ", 0xFFFFFFFF, kind, len(payload))
    return [header + payload[i:i + _WS_CHUNK_BYTES] for i in range(0, len(payload), _WS_CHUNK_BYTES)]


class _WsAssembler:
    def __init__(self, limit):
        self._limit = limit
        self._buf = None
        self._total = 0
        self._kind = 0

    def feed(self, data):
        if isinstance(data, str) or len(data) < 13 or data[:4] != b"\xff\xff\xff\xff":
            if self._buf is not None:
                raise _WsChunkError(1007)
            return data
        _, kind, total = _ws_struct.unpack_from(">IBQ", data, 0)
        if kind > 1:
            raise _WsChunkError(1007)
        if self._buf is None:
            if self._limit >= 0 and total > self._limit:
                raise _WsChunkError(1009)
            self._buf = bytearray()
            self._total = total
            self._kind = kind
        elif total != self._total or kind != self._kind:
            raise _WsChunkError(1007)
        self._buf += data[13:]
        if len(self._buf) > self._total:
            raise _WsChunkError(1007)
        if len(self._buf) < self._total:
            return None
        out = bytes(self._buf)
        self._buf = None
        return out if self._kind == 1 else out.decode("utf-8")


def _ws_closed(exc):
    rcvd = getattr(exc, "rcvd", None)
    if rcvd is not None:
        return WsClosedError(rcvd.code, rcvd.reason)
    return WsClosedError()


def _ws_import():
    try:
        from websockets.exceptions import ConnectionClosed
        from websockets.sync.client import connect
    except ImportError as exc:
        raise ImportError("@ws clients need the 'websockets>=12' package") from exc
    return connect, ConnectionClosed


def _ws_connect(url, headers, max_frame_bytes, ping_interval):
    if url.startswith("https://"):
        url = "wss://" + url[len("https://"):]
    elif url.startswith("http://"):
        url = "ws://" + url[len("http://"):]
    connect, _ = _ws_import()
    max_size = None if max_frame_bytes < 0 else (max_frame_bytes or DEFAULT_MAX_WS_FRAME_BYTES)
    ping = ping_interval if ping_interval and ping_interval > 0 else None
    return connect(url, additional_headers=dict(headers), max_size=max_size, ping_interval=ping, ping_timeout=ping)


class WsFrameSocket:
    """Bidirectional WebSocket handle."""

    def __init__(self, connection, response_type, send_codec=_WsCodec, recv_codec=_WsCodec, max_message_bytes=DEFAULT_MAX_WS_MESSAGE_BYTES):
        self._connection = connection
        self._response_type = response_type
        self._send_codec = send_codec
        self._recv_codec = recv_codec
        self._assembler = _WsAssembler(max_message_bytes)
        self._send_lock = _ws_threading.Lock()
        self._closed_error = _ws_import()[1]

    def _send_dict(self, d):
        data = _ws_encode(d, self._send_codec.split)
        chunks = _ws_chunks(data)
        with self._send_lock:
            try:
                if chunks is None:
                    self._connection.send(data)
                else:
                    for chunk in chunks:
                        self._connection.send(chunk)
            except self._closed_error as exc:
                raise _ws_closed(exc) from exc

    def send(self, frame):
        if hasattr(frame, "validate"):
            frame.validate()
        self._send_dict(frame.to_dict())

    def _next_dict(self, timeout):
        while True:
            try:
                data = self._connection.recv(timeout=timeout)
            except TimeoutError as exc:
                raise WsTimeoutError() from exc
            except self._closed_error as exc:
                raise _ws_closed(exc) from exc
            try:
                payload = self._assembler.feed(data)
            except _WsChunkError as exc:
                self._connection.close(exc.code, str(exc))
                raise WsClosedError(exc.code, str(exc)) from exc
            if payload is None:
                continue
            try:
                return _ws_decode(payload, self._recv_codec.join)
            except (ValueError, KeyError, TypeError) as exc:
                self._connection.close(1007, "invalid frame")
                raise WsClosedError(1007, "invalid frame") from exc

    def receive(self, timeout=None):
        return self._response_type.from_dict(self._next_dict(timeout))

    def close(self, code=1000, reason=""):
        self._connection.close(code, reason)

    def __enter__(self):
        return self

    def __exit__(self, *exc_info):
        self.close()

    def __iter__(self):
        while True:
            try:
                yield self.receive()
            except WsClosedError as exc:
                if exc.code in (None, 1000, 1001):
                    return
                raise


class WsCallSocket(WsFrameSocket):
    """WebSocket handle with correlated call() alongside receive()."""

    def __init__(self, connection, response_type, send_codec=_WsCodec, recv_codec=_WsCodec, max_message_bytes=DEFAULT_MAX_WS_MESSAGE_BYTES):
        super().__init__(connection, response_type, send_codec, recv_codec, max_message_bytes)
        self._lock = _ws_threading.Lock()
        self._pending = {}
        self._inbox = _ws_queue.Queue()
        self._error = None
        self._reader = _ws_threading.Thread(target=self._read_loop, daemon=True)
        self._reader.start()

    def _read_loop(self):
        error = WsClosedError()
        try:
            while True:
                d = self._next_dict(None)
                frame = self._response_type.from_dict(d)
                reply = self._recv_codec.reply(d)
                entry = None
                if reply is not None:
                    reply_id, variant = reply
                    with self._lock:
                        candidate = self._pending.get(reply_id)
                        if candidate is not None and not (candidate[0] and candidate[0] == variant):
                            entry = self._pending.pop(reply_id)
                if entry is not None:
                    entry[1].set_result(frame)
                    continue
                self._inbox.put(frame)
        except WsClosedError as exc:
            error = exc
        except Exception as exc:
            error = WsClosedError(1007, str(exc))
        with self._lock:
            self._error = error
            pending, self._pending = self._pending, {}
        for _, future in pending.values():
            future.set_exception(error)
        self._inbox.put(_WS_CLOSED)

    def receive(self, timeout=None):
        try:
            item = self._inbox.get(timeout=timeout)
        except _ws_queue.Empty as exc:
            raise WsTimeoutError() from exc
        if item is _WS_CLOSED:
            self._inbox.put(_WS_CLOSED)
            raise self._error
        return item

    def call(self, call_id, value, timeout=None, cancel=None):
        if hasattr(value, "validate"):
            value.validate()
        d = value.to_dict()
        if timeout is not None and self._send_codec.with_timeout is not None:
            d = self._send_codec.with_timeout(d, max(1, _ws_math.ceil(timeout * 1000)))
        future = _WsFuture()
        with self._lock:
            if self._error is not None:
                raise self._error
            self._pending[call_id] = (self._send_codec.variant(d), future)
        try:
            self._send_dict(d)
        except BaseException:
            with self._lock:
                self._pending.pop(call_id, None)
            raise
        deadline = None if timeout is None else _ws_time.monotonic() + timeout
        while True:
            wait = None if deadline is None else max(0.0, deadline - _ws_time.monotonic())
            if cancel is not None:
                wait = 0.05 if wait is None else min(wait, 0.05)
            try:
                return future.result(timeout=wait)
            except _WsFutureTimeout:
                if cancel is not None and cancel.is_set():
                    self._abandon(call_id)
                    raise WsCancelledError() from None
                if deadline is not None and _ws_time.monotonic() >= deadline:
                    self._abandon(call_id)
                    raise WsTimeoutError() from None

    def _abandon(self, call_id):
        with self._lock:
            self._pending.pop(call_id, None)
        if self._send_codec.cancel is None:
            return
        try:
            self._send_dict(self._send_codec.cancel(call_id))
        except Exception:
            pass
`
