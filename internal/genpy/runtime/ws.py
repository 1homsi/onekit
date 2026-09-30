import base64 as _ws_base64
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


class WsClosedError(_builtins.ConnectionError):
    def __init__(self, code=None, reason=""):
        detail = (
            ""
            if code is None
            else " (" + str(code) + (": " + reason if reason else "") + ")"
        )
        super().__init__("websocket closed" + detail)
        self.code = code
        self.reason = reason


class WsTimeoutError(_builtins.TimeoutError):
    def __init__(self):
        super().__init__("websocket call timed out")


class WsCancelledError(Exception):
    def __init__(self):
        super().__init__("websocket call cancelled")


class _WsChunkError(Exception):
    def __init__(self, code):
        super().__init__(
            "malformed chunked message"
            if code == 1007
            else "chunked message exceeds the size limit"
        )
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
            parts = [
                _ws_struct.pack(">I", len(head)),
                head,
                _ws_struct.pack(">I", len(raw)),
            ]
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
    header = json.loads(data[offset : offset + head_len].decode("utf-8"))
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
        raw.append(bytes(view[offset : offset + n]))
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
    return [
        header + payload[i : i + _WS_CHUNK_BYTES]
        for i in range(0, len(payload), _WS_CHUNK_BYTES)
    ]


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
        url = "wss://" + url[len("https://") :]
    elif url.startswith("http://"):
        url = "ws://" + url[len("http://") :]
    connect, _ = _ws_import()
    max_size = (
        None if max_frame_bytes < 0 else (max_frame_bytes or DEFAULT_MAX_WS_FRAME_BYTES)
    )
    ping = ping_interval if ping_interval and ping_interval > 0 else None
    return connect(
        url,
        additional_headers=dict(headers),
        max_size=max_size,
        ping_interval=ping,
        ping_timeout=ping,
    )


class WsFrameSocket:
    """Bidirectional WebSocket handle."""

    def __init__(
        self,
        connection,
        response_type,
        send_codec=_WsCodec,
        recv_codec=_WsCodec,
        max_message_bytes=DEFAULT_MAX_WS_MESSAGE_BYTES,
    ):
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
            except _builtins.TimeoutError as exc:
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

    def __init__(
        self,
        connection,
        response_type,
        send_codec=_WsCodec,
        recv_codec=_WsCodec,
        max_message_bytes=DEFAULT_MAX_WS_MESSAGE_BYTES,
    ):
        super().__init__(
            connection, response_type, send_codec, recv_codec, max_message_bytes
        )
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
                        if candidate is not None and not (
                            candidate[0] and candidate[0] == variant
                        ):
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
            wait = (
                None if deadline is None else max(0.0, deadline - _ws_time.monotonic())
            )
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
