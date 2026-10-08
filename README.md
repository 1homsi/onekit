<p align="center">
  <img src=".github/assets/onekit-mascot.svg" alt="Kit, the onekit mascot: a pixel-art blue robot with curly-brace ears" width="300">
</p>

# onekit

onekit is a schema language and toolchain for building HTTP APIs. You describe an API once in `.onk` files, and `onek` generates the code around it: Go servers and clients, TypeScript clients and server routes, Python clients, Dart/Flutter clients, Swift clients, Rust clients and Axum servers, and OpenAPI 3.1 documents.

One binary does the whole job. The compiler turns your schemas into a single intermediate representation (`internal/onkir`), and every generator reads that same representation, so a rule written once in the schema behaves the same way in every language.

## The `.onk` language

```
package example.users

message User {
  id: string
  name: string
  email: string
}

message CreateUserRequest {
  name: string @len(2, 100)
  email: string @email
}

service UserService {
  base_path: "/v1"
  headers: {
    "X-API-Key": string @required @format("uuid")
  }

  createUser(CreateUserRequest) -> User @post("/users")
}
```

Fields carry no numbers to manage, and attributes are written as `@decorator(args)` on the field or method they apply to. The language is pre-1.0 and evolving; read [`examples/onk-simple-api`](examples/onk-simple-api) for a complete, working example, or `internal/onklang` for the grammar itself.

Two things `.onk` is built around:

- **RPC error unions** — `-> User | NotFoundError | ValidationError` makes a method's possible errors part of the schema, so generated clients offer exhaustive, statically typed error handling.
- **Doc comments** (`///`) that flow straight into generated Go and Dart doc comments, TS/Python docstrings, and OpenAPI descriptions.
  Mark fields and RPCs with `@deprecated` or `@deprecated("reason")` to get `Deprecated:` notes in Go, `@deprecated` in TypeScript, `#[deprecated]` in Rust, `@Deprecated` in Dart, a `DeprecationWarning` from Python client calls, and `deprecated: true` in OpenAPI.

## What it generates

| Package | Purpose |
| --- | --- |
| `internal/gengo` | Go structs, validation, HTTP server (`net/http` `ServeMux`), and HTTP client |
| `internal/gents` | TypeScript types, a `fetch`-based client, and framework-agnostic server routes (Web Fetch API); opt-in MSW handlers |
| `internal/genpy` | Python `@dataclass` models, `IntEnum` enums, and a stdlib (`urllib`) client |
| `internal/genrust` | Rust Serde models and validation, a `reqwest` client, and an Axum server/router |
| `internal/gendart` | Dart models (enhanced enums, sealed-class oneofs) and validation, a `package:http` client with SSE streams, and `web_socket_channel` sockets for Flutter and Dart |
| `internal/genswift` | Swift models (final classes, enums with associated-value oneofs) and validation, and an `async`/`await` `URLSession` client with SSE streams, for iOS and macOS |
| `internal/genopenapi` | OpenAPI 3.1 documents (via `pb33f/libopenapi`) |

All target languages and formats are driven off the same compiled schema (`internal/onkir`), produced by parsing `.onk` (`internal/onklang`) and resolving cross-references (`internal/onkcompile`).

## Quick start

```bash
git clone https://github.com/1homsi/onekit.git
cd onekit
make build          # builds ./bin/onek
```

Try the example:

```bash
cd examples/onk-simple-api
go test ./...        # exercises the already-generated code end to end
../../bin/onek compat ./previous-schema .  # reports breaking contract changes
../../bin/onek build .   # regenerates api/*.gen.go and docs/openapi.{yaml,json} from models.onk + service.onk
```

**On Windows**, the released `onek.exe` binary works standalone with no extra
setup. Building the repo itself (`make build`, `make check-generated`,
`scripts/run_tests.sh`) needs GNU Make and a POSIX-ish shell, neither of
which ship with Windows by default. CI runs this on `windows-latest` via
Chocolatey's `make` package plus the `sh`/`bash`/coreutils that come with Git
for Windows (already on `PATH` if you have Git installed); the same setup
(`choco install make`, and Git for Windows for `git`) works locally. WSL or
a Git Bash terminal is the simplest way to get all of it at once.

## The `onek` CLI

A project is a directory with an `onekit.toml` and one or more `.onk` files:

```toml
module = "github.com/you/yourapp/api"
route_prefix = "/api"

[generate.go-server]
out = "./api"

[generate.go-client]
out = "./api"

[generate.ts-client]
out = "./web/client"

[generate.dart-client]
out = "./mobile/lib/api"

[generate.swift-client]
out = "./ios/Sources/Api"

[generate.rust-client]
out = "./src/generated"

[generate.rust-server]
out = "./src/generated"

[generate.openapi]
out = "./docs"
title = "Your API"
version = "1.0.0"
servers = ["https://api.example.com"]
```

The OpenAPI target writes one YAML/JSON pair per service, mirroring the schema
directory structure: `api/hub/business/v1` produces
`docs/hub/business/v1/openapi.yaml` and `openapi.json`. Each document includes
only that service and its transitive schema dependencies, including shared types.
Directories containing multiple services use `<ServiceName>.openapi.yaml` and
`<ServiceName>.openapi.json`. Model-only directories do not emit documents.

`route_prefix` is optional. It prepends a public HTTP prefix to every generated
server, client, and OpenAPI route without changing generated package or import
paths. For example, schemas under `hub/business/v1` still generate into
`hub/business/v1`, while their routes start with `/api/hub/business/v1`.

`schema_root` is optional and lets the schema tree live in a subdirectory of
the project (the directory containing `onekit.toml`) while generator outputs
stay anchored at the project root:

```toml
# onekit.toml at the repository root; schemas live in api/
module = "github.com/you/yourapp/gen/go"
schema_root = "api"

[generate.go-server]
out = "gen/go"
```

Base-path inference and cross-package import paths mirror directories under
`schema_root`; output containment keeps being validated against the project
directory, and the drift manifest stays at `<project>/.onekit/manifest.json`
with `schema_files` recorded relative to the schema root.

The prefix must be a canonical literal URL path such as `/api` or
`/api/internal`: it must start with `/`, must not end with `/`, and cannot
contain query strings, fragments, percent escapes, or path parameters.

```bash
onek check   # parse + compile every .onk file, no codegen - fast validation
onek build   # parse + compile + generate everything configured in onekit.toml
onek fmt     # canonicalize .onk files (use --check in CI)
onek init ./my-api                          # Go server and client
onek init --template fullstack ./my-app     # Go server + TypeScript, Flutter and Swift clients
onek init --list-templates                  # every starter project
onek watch   # rebuild on schema/config changes until interrupted
onek mock    # dev server serving schema-derived fixtures for every route
onek import api.yaml        # convert an OpenAPI 3.x document into .onk
onek import service.proto   # convert a .proto service into .onk
```

`onek init` writes `onekit.toml` and a working `api.onk` you can `onek build`
straight away. `--template` picks the stack: `go` (the default), `web` (Go
server plus a TypeScript client), `fullstack` (that plus Dart/Flutter and Swift
clients), `mobile` (Flutter and Swift clients for a backend you already run),
`ts` (TypeScript server and client), and `rust` (axum server and client).
Every template except `go` starts from the same small Todo service (list,
create, get with a typed `NotFound`, and an SSE stream). The starters are
deliberately unopinionated: the generated TypeScript depends on nothing but
`fetch`. MSW handlers are an opt-in flag (`msw`) on `[generate.ts-client]` for
projects that want them.

`onek build` is incremental: it keeps a small per-project record of the last
successful build in your user cache directory (outside the repository). When
the schemas, `onekit.toml` and the `onek` binary are all unchanged, and every
generated file is still exactly as it was written, the build is skipped and
reports `up to date`. Editing, deleting, or re-checking-out any generated file
triggers a normal rebuild. `onek build --check` never uses the cache. Set
`ONEK_NO_CACHE=1` to disable it, or `ONEK_CACHE_DIR` to relocate it.

### Importing an existing API

`onek import` turns an OpenAPI 3.x document or a `.proto` file into a `.onk`
schema you can check and build, and prints a warning for everything it cannot
express instead of dropping it silently. It refuses to write a schema that does
not pass `onek check`.

The `.proto` reader is built in and needs no external tooling. It converts
messages (nested ones are flattened to `OuterInner`), enums, `repeated`,
`optional`, `map<,>` fields, `oneof`, `google.protobuf` well-known types
(`Timestamp` becomes `timestamp`, `Struct` and `Value` become `json`, wrappers
become optional scalars, `Empty` becomes an empty message), doc comments, and
`deprecated` options. Services become onekit services: `google.api.http` rules
give each RPC its verb, path and `body`; an RPC without one becomes
`POST /<package>.<Service>/<Method>` with all fields in the body. Server
streaming becomes a GET `@stream` (the request fields become query
parameters). Client and bidirectional streaming have no HTTP mapping and
are skipped with a warning.

Things to know before relying on the result: oneofs use onekit's discriminated
JSON encoding, field names keep their `snake_case` form, and imported `.proto`
files are not followed (their types become `json` with a warning). Regenerate
both the client and the server from the converted schema so the two ends agree
on the wire format.

### Try it in the browser

`make playground` builds a static page into `playground/dist` (`index.html`,
`onek.wasm`, `wasm_exec.js`): the real compiler and generators, compiled to
WebAssembly, so you can edit a schema and read the Go, TypeScript, Python,
Dart, Swift, Rust and OpenAPI output with no install. Serve the folder with any static
file server (for example `python3 -m http.server -d playground/dist`). It
formats schemas, shows diagnostics with line and column, and shares a schema as
a link (the schema travels in the URL fragment and never leaves the browser).

## Streaming over POST with typed events

`@stream` works with any verb. With a body-bearing verb the request travels as a JSON body, so large requests (messages, images) stream a response without query-string limits:

```onk
message TurnEvent {
  payload: oneof(discriminator: "type") {
    text: Text @tag("text")
    done: Done @tag("done")
  }
}

service Agent {
  turn(TurnRequest) -> TurnEvent @post("/turn") @stream
}
```

When the response message holds a `oneof`, every SSE frame is named after the variant it carries (`event: text`, `event: done`), and `data:` is the response message as usual. Clients in every target still decode each frame into the response type, so a `switch` over the oneof is all a consumer needs. A frame named `error` stays reserved for failures after the stream has started.

Servers send `: ping` comment frames every 15 seconds, which also commits the response headers when the first event is slow, so a client or proxy header timeout (the Go client defaults to 30 seconds) cannot cut off a quiet stream. Tune it with `WithSSEHeartbeat(d)` in Go (0 disables) and `sseHeartbeatMs` in the TypeScript `ServerOptions`. The Rust server uses axum's default keep-alive.

## Bidirectional WebSocket streaming

Alongside SSE (`@stream`), a method can be declared as a bidirectional
WebSocket RPC. Request messages flow client-to-server and response messages
server-to-client as JSON frames of the declared types:

```onk
service ChatService {
  base_path: "/v1"

  chat(ChatMessage) -> ChatEvent @ws("/rooms/{room}")
}
```

- Path and query parameters bind from the connect request; header contracts
  are checked before the upgrade.
- Servers validate every inbound frame; protocol violations receive an
  `{"error": ...}` frame followed by close code 1008.
- Generated clients return a duplex handle (Go: `Send`/`Receive`/`Close`;
  TypeScript: promise-based `receive()`; Python/Rust/Dart: `send`/`receive`)
  instead of a one-shot response.

Peer dependencies per target, only when the schema uses `@ws`: Go needs
`github.com/coder/websocket`, Python needs `websockets>=12`, Dart needs
`web_socket_channel`, the Rust client
needs `tokio-tungstenite`; servers reuse their existing framework sockets
(axum / Web-standard `WebSocketPair`), except the TypeScript Node adapter
below, which needs the `ws` package.

### Using the Dart client from Flutter

`dart-client` writes a shared runtime (`onekit.dart`, plus `onekit_ws*.dart`
when the schema uses `@ws`) at the output root and a `models.dart` and
`client.dart` per schema package. Point `out` inside your app's `lib/` and add
the dependencies:

```yaml
dependencies:
  http: ^1.2.0
  web_socket_channel: ^3.0.0   # only when the schema uses @ws
```

```dart
import 'package:mobile/api/user/v1/client.dart';

final client = UserServiceClient('https://api.example.com', headers: {'authorization': 'Bearer $token'});
final user = await client.getUser(GetUserRequest(id: id), timeout: const Duration(seconds: 5));
await for (final event in client.watchUser(GetUserRequest(id: id))) {
  print(event.name);
}
```

Messages are plain classes with `toJson`/`fromJson` and `validate()`, so no
`build_runner` step is needed. Enums are enhanced Dart enums, oneofs are sealed
classes you can `switch` over, timestamps are `DateTime`, bytes are
`Uint8List`, and `uint64` is `BigInt`. Declared error types are thrown as typed
exceptions, other failures as `UnexpectedStatusException`, and requests time
out after 30 seconds unless `timeout` says otherwise. The same code runs on
Flutter web, with two browser limits: browsers cannot send custom headers on
a WebSocket handshake, so `@ws` methods throw `UnsupportedError` there when the
client has headers (pass credentials as `@query` fields instead), and `int`
holds exact integers only up to 2^53.

### Using the Swift client from iOS and macOS

`swift-client` writes a shared runtime (`Onekit.swift`) at the output root and
a `<package>__Models.swift` and `<package>__Client.swift` per schema package
(Swift needs unique file names across a module, so the file name carries the
package path). Point `out` at a SwiftPM target's `Sources/<Target>` directory;
there are no dependencies beyond Foundation and it needs iOS 15 / macOS 12 or
newer.

```swift
let client = UserServiceClient("https://api.example.com", headers: ["authorization": "Bearer \(token)"])
let user = try await client.getUser(GetUserRequest(id: id), timeout: 5)
for try await event in client.watchUser(GetUserRequest(id: id)) {
    print(event.name)
}
```

Messages are `final class`es (so recursive messages work) with
`init(json:)`, `toJSONValue()` and `validate()`. Enums are `String` raw-value
enums, oneofs are enums with associated values you can `switch` over,
timestamps are `Date`, bytes are `Data`, and `int64`/`uint64` are `Int64` and
`UInt64`. Declared error types are thrown as typed errors, validation failures
as `OnekitError.validation`, and other failures as
`OnekitError.unexpectedStatus`. Requests time out after 30 seconds unless
`timeout` says otherwise; responses are read with a size limit.

Packages in subdirectories get a namespace named after the directory, so the
same message name can exist in several packages: `crm/dashboard/v1` becomes
`CrmDashboardV1` and its types are `CrmDashboardV1.GetDashboardRequest`; other
packages refer to it the same way. Types at the schema root stay at module
level, so a single-package project has no namespace at all. The build stops
with a clear message if a root type is named like a namespace, or if two
directories map to the same namespace.

Limit of the Swift target so far: `@ws` methods are not generated (a schema
with only `@ws` methods gets no client file).

### Deploying a generated TypeScript `@ws` server

`ts-server` emits two entry points per service with `@ws` methods, so the
same generated output runs on either family of server runtime:

```ts
// Workers / Deno / Bun - built on the Web-standard WebSocketPair. The
// consumer's own fetch handler matches `path` and calls `handle`.
import { createChatServiceRoutes, createChatServiceSocketRoutes } from "./server.js";

const socketRoutes = createChatServiceSocketRoutes(handlerImpl);
export default {
  fetch(req: Request) {
    const { pathname } = new URL(req.url);
    const route = socketRoutes.find((r) => r.path === pathname);
    if (route) return route.handle(req, {});
    // ...dispatch createChatServiceRoutes() the same way for regular routes
  },
};
```

```ts
// Plain Node - no WebSocketPair there at all, so this is a separate path
// built on the `ws` package instead, attached directly to an http.Server.
import * as http from "node:http";
import { attachChatServiceNodeSocketHandlers } from "./server.js";

const server = http.createServer(/* your regular-route handler */);
attachChatServiceNodeSocketHandlers(server, handlerImpl);
server.listen(8080);
```

Regular (non-`@ws`) routes get the same treatment. `attachChatServiceNodeHandlers(server, handlerImpl)`
serves them on an `http.Server`, and an unmatched path gets a 404 when nothing else
listens. `createChatServiceNodeHandler(handlerImpl)` returns `(req, res) => boolean`
for composing with your own request listener. Both take any object shaped like
`node:http`'s, so the generated server needs neither `@types/node` nor `ws`
unless it has `@ws` methods.

### Multiplexed correlated calls with `@ws_id`

A single `@ws` connection can carry many independent, concurrent exchanges -
for example a server that pushes an arbitrary number of asynchronous
`HostCall` frames while handling one long-running request, each of which the
client must answer with a matching `HostResult` before the server continues,
all interleaved and resolved out of order. Model the frame as a `oneof` and
mark whichever field carries the correlation key with `@ws_id`:

```onk
message HostCall { id: string @ws_id
method: string }
message HostResult { id: string @ws_id
value: string }

message Frame {
  payload: oneof(discriminator: "type") {
    run: RunRequest @tag("run")
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    run_result: RunResult @tag("run_result")
  }
}

service Runtime {
  base_path: "/v1"

  execute(Frame) -> Frame @ws("/execute")
}
```

`@ws_id` is a plain field-level decorator (like `@required`): put it on a
non-repeated `string`/`int32`/`int64`/`uint32`/`uint64` field, either directly
on a `@ws` method's request/response message or inside one of its oneof
variants. Every `@ws_id` field a single method touches (across both
directions) must resolve to the same scalar type - `onek check` rejects a
mix, and rejects more than one `@ws_id` field in the same message or variant.

Once declared, every generated backend gives both the server handler's `out`
and the client's duplex handle a `call(id, value)` (Go/Rust) / `.call(id, value)`
(TypeScript) method: it registers a pending waiter keyed by `id`, sends
`value`, and resolves with whichever frame the other side answers under that
same `id` - regardless of how many other calls are in flight or what order
replies arrive in. A reply must be a *different* oneof variant from the frame
the call sent: an inbound `host_call` that happens to reuse the id of your own
in-flight `host_call` is the peer starting its own call, so it reaches the
handler/`receive()` instead of being taken for the reply. Plain `Send`/`Receive` keep working for frames that were
never routed to a pending call. This also closes a correctness gap that
otherwise applies to `@ws` even without correlation: every send is now
serialized (Go: `sync.Mutex`; TypeScript: single event listener; Rust:
`tokio::sync::Mutex`), so concurrent senders on one connection can't corrupt
the wire.

### Cancellation, timeouts, and typed errors

A call can be abandoned before its reply arrives:

| Target | Bound a call | Error when it gives up |
| --- | --- | --- |
| Go | `ctx` (cancel or deadline) | `errors.Is(err, context.Canceled)` / `context.DeadlineExceeded` |
| TypeScript | `call(id, value, { signal, timeoutMs })` | `WSCancelledError` / `WSTimeoutError` |
| Rust | `call_timeout(id, value, duration)`, or drop the future | `WsCallError::TimedOut` |

A call on a connection that is gone (or goes away while waiting) fails
immediately with `errors.Is(err, net.ErrClosed)` in Go (`errors.As` reaches
the underlying `websocket.CloseError`), `WSClosedError` (with `code`/
`closeReason`) in TypeScript, and `WsCallError::Closed` in Rust.

To tell the peer as well, so it can stop working on an abandoned call, mark
one oneof variant with `@ws_cancel`. Its message must carry the `@ws_id`:

```onk
message Cancel { id: string @ws_id }

message Frame {
  payload: oneof(discriminator: "type") {
    # ...
    cancel: Cancel @tag("cancel") @ws_cancel
  }
}
```

Every backend then sends `{"type": "cancel", "cancel": {"id": ...}}` when a
call is abandoned. A cancel frame is never treated as a reply: it arrives at
the peer's handler (server) or `receive()` (client) like any other frame, and
the peer decides what stopping means.

### Deadline propagation with `@ws_timeout`

Mark an integer field next to a call's `@ws_id` with `@ws_timeout`:

```onk
message HostCall {
  id: string @ws_id
  method: string
  timeout_ms: int64 @ws_timeout
}
```

When a call carries a deadline, the generated code writes the remaining
milliseconds into that field on a copy of the frame (the caller's value is
never modified, and an explicitly set value wins). The deadline comes from Go's
`ctx` deadline, TypeScript's `timeoutMs`, or Rust's `call_timeout`. The
receiving handler reads it and can give up on work the caller will no longer
wait for.

### Errors on the wire

A `@ws` connection never carries off-schema frames. When the server rejects
a frame that doesn't decode or validate, it closes with status 1007. When a
handler returns an error, it closes with 1011. Either way the message is the
close reason, truncated to the protocol's 123 bytes. Go clients see this as a
`websocket.CloseError`, TypeScript clients as `WSClosedError.code`/
`closeReason`.

### Backpressure

Go and Rust sends block until the frame is written. In TypeScript, the
server's `out.send()` and the client's `send()` return a promise that
resolves once the socket's `bufferedAmount` is at or under a high-water mark
(1 MiB by default; `highWaterMarkBytes` on the server factories and client
options), so a producer that awaits its sends is paced by the peer. The
promise never rejects, so un-awaited sends behave as before.

### Knowing when the connection is gone

A server handler can watch its connection: `out.Context()` in Go (its cause
is the close error), `out.signal` in TypeScript (aborted with a
`WSClosedError`), and `out.closed().await` / `out.is_closed()` in Rust. It can also end its own
connection with a code and reason: `out.Close(code, reason)` in Go,
`out.close(code, reason)` in TypeScript, `out.close(code, reason).await` in
Rust. That's useful for shutting down only idle sockets.
Servers ping every 30 seconds and drop a peer that misses a pong, so a hard
network drop ends the connection instead of hanging until a timeout. Set
`WithWSPingInterval(d)` in Go (negative disables), `pingIntervalMs` in the
TypeScript Node adapter (0 disables), or `WsServerOptions::ping_interval` in
Rust (`None` disables). Clients of methods that use `@ws_id` ping too, every
30 seconds by default, and fail in-flight calls with a closed error when a
pong goes missing: `WSPingInterval` in Go, `with_ws_ping_interval` in Rust.
Browsers and Node's built-in `WebSocket` offer no ping API, so TypeScript
clients rely on the server's pings. A client that receives a frame it can't decode fails
the socket (a 1007 `WSClosedError` in TypeScript, the decode error in Go)
rather than skipping it.

### Large payloads with `@raw`

Mark a non-repeated, non-optional `string` or `bytes` field `@raw` to carry it
outside the JSON:

```onk
message RunResult {
  exit_code: int32
  result_json: string @raw
}
```

A frame whose raw fields hold data is sent as one binary WebSocket message:
a 4-byte big-endian header length, the frame's JSON with every raw field
emptied, a 4-byte segment count, one 4-byte length per segment, then the raw
bytes appended untouched. The segments follow a depth-first walk of the schema
in declaration order (direct fields, message fields, repeated messages, and
the set oneof variant), so no paths travel on the wire. The receiver parses
only the small header. Go gets each raw field as a slice of the message
buffer, with no scanning, unescaping or copying. Frames whose raw fields are
all empty stay plain JSON text, and every receiver still accepts JSON text,
so older peers keep working. In TypeScript a raw `bytes` field is a
`Uint8Array`.

A 400 KB `result_json` (Apple M-series, Go 1.26, Node 26):

| | JSON | `@raw` |
| --- | --- | --- |
| Go encode | 2252 µs (v0.16), 635 µs now | 24.6 µs |
| Go decode | 3470 µs (v0.16), 1642 µs now | 0.6 µs |
| Node encode | 665 µs | 52 µs |
| Node decode | 667 µs | 25 µs |
| Go client ↔ Go server round trip | 3347 µs | 160 µs |

### Messages over 16 MiB

A message larger than 16 MiB (JSON text or a `@raw` binary frame) is sent as
8 MiB binary chunks. Each chunk carries a 13-byte header: the marker
`0xFFFFFFFF`, a kind byte (0 text, 1 binary) and the total length as a
big-endian u64. The receiver reassembles them before decoding, so `@raw`
fields remain zero-copy slices of the assembled buffer. The per-message cap
still applies to each chunk, and the assembled total has its own cap, 256 MiB
by default: `WithMaxWSMessageBytes` / `MaxWSMessageBytes` in Go,
`maxMessageBytes` in TypeScript, `max_message_bytes` / `with_max_ws_message_bytes`
in Rust. Over it, the connection closes with 1009. Anything up to 16 MiB goes
out exactly as before.

### Frame size limit

Every target caps one inbound message at 16 MiB by default and closes the
connection when a peer exceeds it (status 1009 in Go and TypeScript):

- Go: `RegisterXServer(mux, impl, WithMaxWSFrameBytes(n))`, and the client
  field `MaxWSFrameBytes`. Negative disables the check.
- TypeScript: `createXSocketRoutes(handler, { maxFrameBytes })`,
  `attachXNodeSocketHandlers(server, handler, { maxFrameBytes })`, and the
  client option `maxFrameBytes`. Negative disables the check.
- Rust: `x_router_with_ws_options(service, WsServerOptions { max_frame_bytes })`
  and `XClient::with_max_ws_frame_bytes(n)`.

## Frontend TypeScript extras

The generated TypeScript depends on nothing but `fetch`. Each message also gets
a `validate<Message>()` function that returns the violations of its validators
and `@rule`s, so forms can check input against the exact API contract without a
schema library. One companion module is opt-in on the `ts-client` target:

```toml
[generate.ts-client]
out = "./web/client"
msw = true         # msw.ts - Mock Service Worker handlers per route
```

- **msw.ts** emits deterministic fixtures derived from validators
  (`@uuid` → real UUID shape, `@in(...)` → first allowed value, encodings
  honored) so component tests intercept fetch with contract-accurate data:
  `worker.use(...userServiceHandlers)`. The `msw` package is a peer dependency
  only when the flag is on.

The generated TypeScript depends on nothing but `fetch`. The client is a plain
class, so it slots into whatever data layer your app already uses: wrap a call
in your own `useQuery`, or validate with your own schema library, in a few lines.

### The mock server

`onek mock` compiles your schema tree and serves every route with
schema-derived JSON - realistic values from validators, correct wire
encodings, declared error statuses, and SSE streams that emit three frames:

```bash
onek mock --dir api --addr :8080 \
  --seed 1 \                # deterministic errors/latency
  --error-rate 0.1 \        # serve declared typed errors ~10% of the time
  --latency 300ms           # inject up to 300ms jitter per request
```

Fixtures are pure functions of the schema: identical schemas produce
byte-identical responses across runs and machines, keeping frontend
snapshot tests stable. Add `--watch` to pick up schema and config edits
without restarting; a schema that fails to compile is reported and the
previous routes keep serving.

Go client and server targets must use the same output directory because they
share one generated types package. Successful builds remove obsolete OneKit-
generated files from configured output roots while preserving handwritten
files. Maps use string keys on the JSON wire, `json` fields preserve arbitrary
JSON values, and optional scalar presence is declared with `?` (for example,
`count: int32?`). Repeated scalar query parameters are emitted as repeated
`name=value` pairs across the supported HTTP clients and OpenAPI document.

Use `@body("field_name")` to bind one request field as the body of a POST, PUT,
PATCH, or QUERY RPC. Header contracts support required values, UUID/email/URI
formats, examples, deprecation, and `api_key`, `bearer`, or `basic` auth. These
contracts feed server checks and OpenAPI security schemes; generated TypeScript
handlers, Go authorization hooks, and Rust request contexts expose the incoming
headers for application-level authentication.

### Nullable fields with `@nullable`

An optional field (`?`) is omitted from the wire when unset. Add `@nullable` and it has three states that survive a round trip: absent, an explicit `null`, and a value. This is what a PATCH body needs to tell "leave it alone" from "clear it".

```onk
message UpdateEnvironment {
  id: int64 @encode("number")
  name: string? @nullable
  folder_id: int64? @nullable @encode("number")
}
```

| Target | Representation |
| --- | --- |
| Go | the pointer field plus `NameNull bool`: nil and `NameNull` is an explicit null, nil and not `NameNull` is absent, a pointer wins |
| TypeScript | `name?: string \| null`: `undefined` is absent, `null` is null |
| Rust | `Option<Option<T>>`: `None` absent, `Some(None)` null |
| Python | the field plus `name_null: bool` |
| Dart | the field plus `nameNull` |
| Swift | the field plus `nameNull` |
| OpenAPI | the schema allows `null` |

In a message that only responses use (some RPC returns it and no RPC accepts it), a `@nullable` field is always on the wire: Go writes `null` for an unset field without needing the `Null` flag, so the TypeScript property is required and typed `T | null` (`folder_id: number | null`, `app: App | null`) and the decoder turns a missing key into `null`. A message that is also a request keeps the three-state form above. A plain `?` field without `@nullable` stays optional, since Go omits it when unset.

`@nullable` needs the `?` marker and a plain wire form: it cannot be combined with `@required`, `@query`, `@flatten`, `@unwrap`, `@empty`, an `@encode` on an enum, message or timestamp, or `bytes`, and a 64-bit integer must be numeric (`int64_encoding = "number"` or `@encode("number")`). Rules and validators treat `null` as absent.

### Wildcard path parameters and empty routes

A path parameter written `{name...}` captures the rest of the path, slashes included. It must be the last segment, bind to a `string` request field, and cannot be used on `@ws` routes.

```onk
service Files {
  base_path: "/files"
  read(FileRef) -> Content @get("/{path...}")
  root(Empty) -> Content @get("")
}
```

`GET /files/dir/sub/b.txt` delivers `dir/sub/b.txt` to `FileRef.path`, percent-decoded. Generated clients escape each segment separately and keep the slashes. OpenAPI documents the route as `/files/{path}` and marks the parameter with `x-onekit-wildcard: true`.

`@get("")` (any verb) is allowed when the service sets `base_path`, and serves exactly the base path. An exact route wins over a wildcard route that could also match. The Go, TypeScript and mock servers match `/files/` with an empty `path`; the Rust server (axum) requires at least one character.

### Route metadata with `@meta`

`@meta(key, value)` attaches a free-form pair to a method and hands it to your own code at runtime, so facts about a route that your middleware needs, such as the permission it checks or the audit event it records, live in the schema next to the route instead of in a second table:

```onk
edit(EditApp) -> App @post("/apps/{slug}/edit")
  @meta("guard", "app/edit/:slug")
  @meta("audit.event", "app.update")
```

It is repeatable, keys are lower-case (`[a-z][a-z0-9_.-]*`) and unique per method, and values are 1 to 200 bytes. It is not supported on `@ws` methods. onekit attaches no meaning to the pairs.

| Target | Where you read it |
| --- | --- |
| Go server | `RequestMetadata.Meta` / `MetaValue(key)`, in an `Authorizer`, in middleware via `RequestMetadataFromContext(ctx)`, or in the handler. Middleware runs outside the authorizer, so it also sees requests the authorizer denies, which is what an audit log wants |
| TypeScript server | `route.meta` in `authorize(req, route)`, and `context.meta` in the handler |
| Rust server | `context.meta_value(key)` and `context.meta` in the handler |
| OpenAPI | `x-onekit-meta` on the operation |
| `onek compat` | adding, removing or changing a pair is reported as a contract change |

A permission guard keyed on a path parameter is then a few lines: read `guard` from the metadata, read the parameter with `r.PathValue("slug")`, and decide.

### Per-method body limits with `@max_body`

Servers cap request bodies at 8 MiB by default (`WithMaxRequestBodyBytes` in Go sets one value for a whole `Register...Server` call). `@max_body` sets a different cap for one method, so a large upload and a small form can live in the same service:

```onk
service Agent {
  turn(TurnRequest) -> TurnEvent @post("/turn") @stream @max_body("64MiB")
  rename(RenameRequest) -> Renamed @post("/rename")
}
```

The size is a byte count with an optional `B`, `KiB`, `MiB` or `GiB` suffix, and it needs a body-bearing verb. Go, TypeScript and Rust enforce it and answer `413` with `request_body_too_large`; it overrides the server-wide limit for that method only.

### One runtime for every Go package

By default each generated Go package carries its own copy of the server core: `ServerError`, `ErrorWriter`, `Authorizer`, `Middleware`, `RequestMetadata` and the options that configure them. With several schema packages that means several distinct `ErrorWriter` types, and one writer, authorizer or middleware has to be adapted for each. Put the core in one package instead:

```toml
[generate.go-server]
out = "./gen"
runtime = "shared"        # "package" is the default
runtime_dir = "onekitrt"  # relative to out; this is the default
```

The core is written once to `gen/onekitrt/runtime.gen.go`, and every package aliases its types and options (`type ServerError = onekitrt.ServerError`, `var WithErrorWriter = onekitrt.WithErrorWriter`), so existing code keeps compiling. One error writer, one authorizer and one option list now serve every module:

```go
opts := []any{
    onekitrt.WithErrorWriter(envelope),
    onekitrt.WithAuthorizer(authorize),
    onekitrt.WithRequestID("X-Request-ID"),
}
orders.RegisterOrdersServer(mux, append([]any{ordersImpl{}}, opts...)...)
users.RegisterUsersServer(mux, append([]any{usersImpl{}}, opts...)...)
```

`WithPrincipal` stays per package because its type is the package's own principal message. The runtime directory must not share a name with a schema directory, and switching `runtime` back removes the shared package on the next build.

### Choosing which services a target generates

Every target generates code for every service by default, which leaves dead code when a client only needs some of them (a runtime protocol with no browser client, an admin API with no mobile client). Each generator target accepts two glob lists over the service name:

```toml
[generate.go-server]
out = "./gen"
include_services = ["Public", "Admin*"]   # only these

[generate.ts-client]
out = "./web/client"
exclude_services = ["Admin*", "Runtime"]  # everything but these

[generate.openapi]
out = "./docs"
include_services = ["Public"]
```

With `include_services`, only matching services are generated; anything matching `exclude_services` is then dropped. Within a package, types are generated in full, so a client and a server built from one schema keep sharing them. If a package has no service left for a target, that target's client or server file is removed. A pattern that matches no service anywhere in the project is an error, since it is almost always a typo. The options work on every target (`go-server`, `go-client`, `ts-client`, `ts-server`, `python-client`, `dart-client`, `swift-client`, `rust-client`, `rust-server`, `openapi`).

To leave whole packages out of a target, use `include_packages` and `exclude_packages`. They are globs over the schema directories, relative to the schema root (`common`, `hub/*`, `jsruntime`; the root itself is `.`):

```toml
[generate.ts-client]
out = "./web/client"
exclude_packages = ["jsruntime"]

[generate.ts-server]
out = "./runtime/api"
include_packages = ["jsruntime"]
```

An excluded package gets no types, client or server file in that target, and files left from an earlier build are removed. If a package that stays uses a type from one that is left out, the build fails and names both packages, so nothing is generated against a missing import. A pattern that matches no package is an error.

### The `httpkit` package

`github.com/1homsi/onekit/httpkit` holds the glue most services write around a generated Go server. It uses only the standard library and never imports generated code, so it works with inline and shared runtimes alike.

```go
mux := http.NewServeMux()
api.RegisterOrdersServer(mux, impl{},
    api.WithMiddleware(httpkit.Middleware(httpkit.Config{TrustedProxies: []string{"10.0.0.0/8"}})),
    api.WithErrorWriter(httpkit.ErrorWriter[*api.ServerError](httpkit.Envelope{})),
    api.WithAuthorizer(authorize),
)
```

- **Error envelope.** `httpkit.Envelope` writes `{"error": {"code", "message", "request_id"}}`, plus `field` and `violations` when there are any. The cause of a 5xx is logged with the request id (`slog`, or `Envelope.Logger`) and never sent. `Envelope.ContentType` and `Envelope.Wrap` change the content type and the body shape.
- **Request state.** `httpkit.Middleware` puts a `*State` in the context: `RequestID` (the incoming header, or a generated id, echoed on the response), `Start`, `ClientIP` and the response `Status()` and `Bytes()`. `X-Forwarded-For` is believed only when the connection comes from a `TrustedProxies` address, and the client address is the closest hop that is not itself a trusted proxy. `httpkit.SetPrincipal(ctx, p)` records the authenticated caller, `httpkit.Principal[T](ctx)` reads it, and `httpkit.MustPrincipal[T](ctx)` panics when it is missing, for routes behind authentication. The principal lives on the shared state, so an `Authorizer` can set it and the handler and observers can read it.
- **Response size and path values.** `RequestResult` now carries `Bytes` and `PathValues` (the route's path parameters), so an audit log can be a plain `RequestObserver`. `httpkit.NewRecorder` wraps a `ResponseWriter` when you need the same numbers outside the generated server.
- **Guards.** `@guard("object/level/:id")` on an RPC attaches permission patterns whose `:name` segments must be path parameters of the route. Guards are data for your authorizer, not a check on their own: a route with `@guard` and no `WithAuthorizer` is not protected. `ResolveGuards` refuses a path value that is empty, `.`, `..` or contains `/`, `\` or a control character (a percent-encoded `%2F` decodes to a slash inside one path segment), so a caller cannot reshape the key you check. The server passes them to your authorizer as `RequestMetadata.Guards`, and `httpkit.ResolveGuards(meta.Guards, r.PathValue)` fills in the request's values:

```go
func authorize(ctx context.Context, meta api.RequestMetadata, r *http.Request) error {
    guards, ok := httpkit.ResolveGuards(meta.Guards, r.PathValue)
    if !ok {
        return errors.New("unresolvable guard")
    }
    user, err := authenticate(r)
    if err != nil {
        return err
    }
    httpkit.SetPrincipal(ctx, user)
    return user.Can(guards...)
}
```

### Routes that are not JSON in, JSON out

Some routes do not fit a message-in, message-out shape: a JavaScript bundle with an `ETag`, an image, an upload that streams to object storage. Declare them in the schema with `@http` and implement them as ordinary `http.Handler` code. They still get the route, the generated bind-and-validate step, `@guard`, `@meta`, `@max_body`, middleware, the authorizer and the request observer.

```onk
message BundleRequest { slug: string  v: int32 @query }
message UploadRequest { slug: string  key: string }
message Nothing {}

service Apps {
  base_path: "/apps"
  bundle(BundleRequest) -> Nothing @get("/{slug}/bundle.js") @http("application/javascript") @guard("apps/read/:slug")
  put_object(UploadRequest) -> Nothing @put("/{slug}/objects/{key...}") @http @max_body("64MiB")
}
```

The generated Go interface takes the response writer, the request and the bound request message, and returns an error:

```go
func (s *service) Bundle(w http.ResponseWriter, r *http.Request, req *api.BundleRequest) error {
    body, err := s.bundles.Load(r.Context(), req.Slug)
    if err != nil {
        return err
    }
    etag := httpkit.ETag(body)
    w.Header().Set("ETag", etag)
    if httpkit.IfNoneMatch(r, etag) {
        w.WriteHeader(http.StatusNotModified)
        return nil
    }
    w.Header().Set("Content-Type", "application/javascript")
    _, err = w.Write(body)
    return err
}
```

An error returned before the handler has written anything goes through the error writer, so it uses the same envelope as every other route; an error returned after the response started is not written on top of it. Only path parameters and `@query` fields may be declared on the request message, because the handler reads the body itself (`r.Body`, already limited by `@max_body` or `WithMaxRequestBodyBytes`).

Clients return the raw response: the Go client returns `*http.Response` (and takes `body io.Reader, contentType string` for PUT, POST and PATCH), and the TypeScript client returns a `Response` (with `{ body, contentType }` in the options for routes with a body), so headers such as `ETag`, `res.blob()` and streaming all work. `@http` is supported by the `go-server`, `go-client`, `ts-client` and `openapi` targets; a project that also generates the other targets is rejected with a message that says so. OpenAPI documents the route with a binary body and the declared content type.

### Cookies, redirects, 204 and 304

A handler adjusts the response through its context:

- `api.ResponseHeader(ctx).Set("ETag", ...)` and `Cache-Control` set headers.
- `api.SetCookie(ctx, &http.Cookie{...})` adds a `Set-Cookie` header (invalid cookies are dropped).
- `api.Redirect(ctx, url, http.StatusFound)` answers with a `Location` header and no body.
- `api.NotModified(ctx)` answers `304` with no body, while the `200` case still uses the generated JSON encoding.
- `api.SetResponseStatus(ctx, status)` chooses any 2xx or 3xx status; `204` and `304` send no body.

`@success(204)` on an RPC makes `204 No Content` the default status of the method (`@success(201)` works too), and OpenAPI documents it as such. `onek compat` reports a change.

### Hand-written routes with the same pipeline

`api.Mount(mux, "/apps/{slug}/proxy/{resource}/{path...}", handler, api.RequestMetadata{Service: "Proxy", Method: "Forward", Route: "/apps/{slug}/proxy/{resource}/{path...}", Meta: map[string]string{"audit.event": "proxy"}, Guards: []string{"apps/use/:slug"}}, opts...)` registers any `http.Handler` with the options the generated routes get: middleware, `WithAuthorizer` and `WithScopes`, the request id and the request observer. Use it for routes the schema cannot describe, such as one that forwards every HTTP method. Pass the same `ServerOption` values you give `Register...Server`.

A `RequestObserver` sees what an audit log needs: `RequestResult` carries the status (500 when the handler panicked, with the panic in `Panic`), `Bytes`, `PathValues` and the `Request`. `RequestStarted` can call `httpkit.EnsureState(ctx, api.HTTPRequestFromContext(ctx))` so the request id, the principal (`httpkit.SetPrincipal`) and per-request flags (`httpkit.SetFlag(ctx, "skip-audit")`) are shared with the handlers and readable in `RequestFinished`.

`httpkit.Health(checks...)`, `httpkit.SPA(fsys, opts)` (client-side routes get `index.html`, a missing file with an extension is a 404, per-request headers through `OnServe`) and `httpkit.ETag` / `httpkit.IfNoneMatch` cover the usual non-schema routes.

### Shaping error responses

By default every error the generated servers produce themselves, a malformed body, a bad path or query parameter, a missing header, a failed validation or `@authorize` rule, or a handler error with no declared body, is `{"message": "..."}` (with `"violations": [...]` when there are several). Errors a method declares with `@status` keep their declared body. To send a different shape, install one error writer per server:

| Target | How |
| --- | --- |
| Go | `WithErrorWriter(func(w, r, e *ServerError))`; wrap the mux in `ErrorHandler(mux, opts...)` to cover the router's own plain-text 404 and 405 too |
| TypeScript | `{ onError: (error, req) => Response }` on `createXFetchHandler`, `createXNodeHandler` and `attachXNodeHandlers` |
| Rust | `with_error_writer(router, Arc::new(\|info, headers\| ...))` |

The writer receives the status, a stable `code`, the default `message`, the `field` at fault (a path parameter, query parameter or header) and the `violations`. The codes are `invalid_request_body`, `request_body_too_large`, `invalid_path_parameter`, `invalid_query_parameter`, `missing_header`, `invalid_header`, `invalid_credentials`, `validation_failed`, `unauthorized`, `forbidden`, `not_found`, `method_not_allowed` and `internal`; a handler error uses the snake_case name of its status, or its own `PublicCode()`. The underlying error is available as `Cause` (Go) and is never sent by the default writer. In Go the request ID is `RequestIDFromContext(r.Context())`.

```go
mux := http.NewServeMux()
api.RegisterThingsServer(mux, impl{}, api.WithRequestID("X-Request-ID"), api.WithErrorWriter(envelope))
http.ListenAndServe(addr, api.ErrorHandler(mux, api.WithRequestID("X-Request-ID"), api.WithErrorWriter(envelope)))
```

Errors that happen after a stream has started cannot change the HTTP status, so they travel as an `event: error` frame. That frame goes through the same writer: the body is whatever your error writer produces for the handler's error, so the code and message a handler exposes (`PublicCode` and `PublicMessage` in Go, `HttpError` in TypeScript) reach the client in your envelope. Errors a method declares with `@status` keep their declared body, and an unexpected error still shows only the generic message. `error` stays a reserved event name, so a oneof variant of your own should use another name.

Parameter errors no longer include the Go parser's text: a non-numeric `{id}` is `invalid path parameter id: must be an integer`.

A message may have a field called `error` (the Go field is `Error_` on an error message, with the same JSON name), so an envelope such as `{"error": {"code", "message", "request_id"}}` can also be declared as a typed error with `@status`.

The generated TypeScript client throws `ApiError` for any undeclared status. Besides `statusCode` and the raw `body` it now has the parsed `json`, `message`, `code` and `requestId`. The default parser reads `{message}`, `{code, message, request_id}` and the nested `{error: {...}}` envelope and falls back to the `X-Request-ID` header; pass `errorParser` in the client options for any other shape.

### The JSON content type

Generated Go servers answer JSON with `Content-Type: application/json`. To match an existing API that sends a charset, set it on the target:

```toml
[generate.go-server]
out = "./api"
json_content_type = "application/json; charset=utf-8"
```

It applies to every JSON response the server writes, errors included, and to the shared runtime when `runtime = "shared"`. The value must be a JSON media type (`application/json` or a `+json` type).

### 64-bit integers as JSON numbers

`int64` and `uint64` cross the wire as JSON strings by default, so JavaScript and other double-based parsers never silently lose precision. When your IDs are numbers everywhere and you accept that trade, set it once for the whole project instead of marking every field with `@encode("number")`:

```toml
module = "example.com/api"
int64_encoding = "number"   # "string" is the default
```

The setting applies to every `int64` and `uint64` field in every target, including repeated ones (which cannot carry `@encode`), optional ones, and the OpenAPI schemas, and a field's own `@encode("number")` stays valid. Map values were always numbers. A 64-bit value beyond 2^53 loses precision in any consumer that parses JSON numbers as doubles, such as a browser, which is why the default is the string form. Changing the setting on an existing API is a wire change, so `onek compat` reports every affected field.

`oneof` variant payloads that are themselves 64-bit integers keep the string form.

### Repeated messages as values in Go

By default a repeated message field is `[]*T` in Go. For large lists that costs an allocation per item and forces conversion helpers, so one setting changes the Go types to `[]T`:

```toml
repeated_style = "values"
```

Validation, JSON, the getters and `@rule` expressions all follow, and the other targets are unchanged. Items are not pointers, so a nil item cannot occur. A message reachable from a `@ws` method keeps `[]*T`, since the WebSocket frame codec works with pointers. The default is `"pointers"`.

### JSON objects with `@object`

A plain `json` field holds any JSON value. Add `@object` when it must be an object, as with a free-form settings blob: TypeScript types it `Record<string, unknown>` and validates it, Go keeps `json.RawMessage` (the bytes pass through untouched, so keys are never reordered, which a `map[string, json]` would not guarantee) and rejects arrays and scalars in `Validate()`, and OpenAPI declares `type: object`. `null` and an absent field stay allowed. The Python, Dart, Swift and Rust generators treat the field as any `json` value.

### Rejecting non-positive ids

Put `@range(1, 9223372036854775807)` on an `int64` id field to reject zero and negative ids in the generated Go, TypeScript and Rust validators (use `4294967295` for a `uint32`). With 64-bit integers carried as strings, that is the one place the bound is spelled, and `onek compat` reports it if it changes.

### Request fields on GET and DELETE

A GET or DELETE has no body, so a request field reaches the server only through the route (`{id}`) or the query string. Scalar fields (string, bool, integer, float) that are not in the route are bound to the query string automatically, as if they carried `@query`. A field that cannot be sent that way (a message, an enum or a repeated field) is a build error that names the field and the RPC, instead of arriving as a zero value. A request message that a body-carrying RPC also uses (a resource message shared by `get` and `update`) is left alone, since its other fields are meant for the body.

### Member names that are keywords

A field called `in`, `from`, `default`, `type` or `class` keeps that exact name on the wire and in Go and TypeScript (`In`, `from`). Names that a target language cannot use are rejected only when that target is configured: with `[generate.python-client]` present, `in` and `from` fail the build, because Python cannot use them as attribute names; a project that generates only Go and TypeScript can use them freely. Rust, Dart and Swift generate escaped names (`r#in`, `in_`). A project that lists no targets keeps every rule.

### Query parameters on POST, PUT and PATCH

`@query` also works on routes with a body. The field is sent in the query string, the rest of the message is the body (or the single `@body` field), and the generated Go and TypeScript servers, clients and OpenAPI follow. The python, dart, swift and rust targets do not support it yet, so a project that generates them is rejected with a message that says so.

### Integer map keys

A map key is a `string`, `int32`, `int64`, `uint32` or `uint64`: `map[int64, string]`. JSON object keys are strings on the wire, so the wire form is the same for every key type. Go generates `map[int64]string` and decoding rejects a key that is not an integer, Rust generates `HashMap<i64, String>`, and TypeScript keeps `Record<string, V>` and fails validation for a key that is not an integer (an unsigned key may not have a sign). Python, Dart and Swift keep string keys.

### Schemas in `.proto` files

Schemas are `.onk` files. If you want protobuf-aware editors and GitHub to highlight them, name them `.proto` and say so in `onekit.toml`:

```toml
schema_extensions = [".onk", ".proto"]
```

A `.proto` file is then read as OneKit when it is OneKit text. A real Protocol Buffers file, one with a `syntax = ...` line or numbered fields such as `string id = 1;`, is skipped, so a directory that also holds gRPC definitions still builds; convert those with `onek import`. `build`, `check`, `fmt`, `watch`, the language server and MCP all follow the setting. Without it only `.onk` files are schemas. Tools such as `protoc` and `buf` cannot parse OneKit syntax, so keep them away from these files.

### Pinning the onek version

Set `version` in `onekit.toml` and every machine builds with that release, with no Go toolchain and nothing to install beyond a first `onek`:

```toml
version = "0.26.0"
module = "example.com/api"
```

When `onek build`, `check`, `generate`, `watch` or `mock` runs with a different version, it downloads the pinned release from GitHub, checks its SHA-256 against the release's `checksums.txt`, keeps it in the cache directory (`ONEK_CACHE_DIR`, default the user cache dir) and hands the command over to it. The download happens once per version. Set `ONEK_NO_PIN=1` to skip the pin, or `ONEK_DOWNLOAD_BASE` to fetch from a mirror. A development build ignores the pin. Pin a release that already knows the `version` key (0.26.0 or newer), since older releases reject unknown keys.

### Writing zero values

By default the Go and TypeScript targets leave a field out of the JSON when it holds its zero value (`""`, `0`, `false`, an empty list), because on the wire an absent field and a zero field read the same. If consumers expect every field to be present, say so once for the project:

```toml
module = "example.com/api"
emit_zero_values = true
```

Every non-optional scalar, enum, `bytes`, repeated and map field is then always written, in every target: `""`, `0`, `false`, the enum's first name (or `0` for a number-encoded enum), `[]` and `{}`. A nil list or map in Go is written as `[]` or `{}`, never `null`. Optional (`?`) fields keep their meaning, absent when unset, and a singular message field is still omitted when it is not set. Timestamps and `json` values are left as they were, because a zero timestamp has no single form that every language agrees on.

Messages that need this (and other wire adjustments such as 64-bit strings) are encoded by a generated `AppendJSON` method, described below. The generated types import `encoding/json/jsontext` for it, which needs Go 1.27 or newer.

This applies to requests as well as responses, since both use the same types, so a message used as a partial update should declare its patchable fields optional (`name: string?`) to say "not provided". `onek compat` reports the change.

### How generated Go messages are encoded

A message with a wire adjustment (64-bit strings, nullable fields, number enums, `@encode` on timestamps or bytes, a oneof, or `emit_zero_values`), and every message that holds one, gets `AppendJSON(b []byte) ([]byte, error)`. It writes the fields straight into one buffer: no reflection, no helper struct, and no second scan of what a nested message wrote. Nested messages, lists and maps are written by the same call, so a response is encoded once however deep it is. A plain message that sits inside such a message gets `AppendJSON` too, so the call never falls back to `encoding/json` in the middle of a list.

`MarshalJSON` and `MarshalJSONTo` call it, so `json.Marshal` and `jsonv2.Marshal` use it with no change to your code. The generated server writes every response that has an `AppendJSON` through it directly, into a pooled buffer, which skips the validation pass `json.Marshal` runs over a `Marshaler`'s output. Call it yourself to encode into a buffer you own: `buf, err := resp.AppendJSON(buf[:0])`.

Ten resources with nested messages, maps, a nullable field and a `json` field, on Go 1.27.1 (`go test ./internal/gengo -run TestAppendEncoderBenchmark -v`):

| | time | allocations |
| --- | --- | --- |
| hand-written struct, `json.Marshal` | 21.3 µs | 61 |
| generated before, `json.Marshal` | 23.8 µs | 103 |
| generated now, `json.Marshal` | 17.1 µs | 11 |
| generated now, `AppendJSON` into a reused buffer | 7.2 µs | 10 |

The output is the same JSON, with two differences. Keys come out in declaration order; before, fields with a wire adjustment came after the others. A `json` field is validated and written as stored, not compacted, and a field holding invalid JSON fails the encode. A message that uses `@flatten` keeps the earlier encoder. Decoding is unchanged.

### Declaring what a method requires

`@requires("users:read", "users:write")` on an RPC declares the scopes (or
roles) a caller must hold; every listed scope is required. Onekit does not
decide where scopes come from, it makes the requirement part of the contract
and hands it to your authentication code in each target:

```onk
get(GetUser) -> User @get("/users/{id}") @requires("users:read")
```

| Target | What you get |
| --- | --- |
| Go server | `RequestMetadata.Scopes` in your `Authorizer`, plus `WithScopes(func(ctx, r) ([]string, error))`, which returns `403` with `missing required scope: ...` (a `*ScopeError`) and leaves authentication failures as `401` |
| TypeScript server | `scopes` on each `RouteDescriptor`, and `{ authorize: requireScopes((req) => grantedScopes) }` on `createXFetchHandler`, `createXNodeHandler` and `attachXNodeHandlers` (or your own `authorize(req, route)`) |
| Rust server | `context.required_scopes` and `context.missing_scopes(&granted)` on the `RequestContext` passed to every handler |
| OpenAPI | the scopes are listed under each auth scheme in the operation's `security` requirement (needs an `@auth` header on the service or method) |
| `onek compat` | adding, removing or changing required scopes is reported as a contract change |
| `onek import` | scopes in an OpenAPI operation's `security` become `@requires(...)` |

`@requires` is not supported on `@ws` methods yet (the compiler says so rather
than generating a server that silently skips the check); authorize the
WebSocket upgrade request in middleware for now. The generated Go, TypeScript
and Rust clients do not check scopes, since a client does not know what its
server will grant.

### Authorizing callers with `@authorize`

`@requires` names scopes and leaves the decision to you. `@authorize(expression, message)` states the decision itself, in the same expression language as `@rule`, and the generated server enforces it:

```onk
message Principal @principal {
  user_id: string
  roles: string[]
  org: string
}

service Docs {
  delete(DeleteDoc) -> Ack @post("/docs/delete")
    @authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")
}
```

`@principal` marks the one message that describes the authenticated caller (a project may have only one). In an `@authorize` expression `auth` is that message and `req` is the decoded request, so a rule can compare who is calling with what they are asking for. Both are type-checked when the schema compiles, with the same line and column diagnostics as `@rule`. The decorator is repeatable, and every rule must hold.

The order on the server is: authenticate (`401` when no caller can be identified), decode and validate the request (`400`), then evaluate the rules (`403` with the first failed message in `message` and all of them in `violations`). A rule that cannot be evaluated counts as failed, so it can never silently allow a call. A method without `@authorize` never asks who the caller is, and a server that has an `@authorize` method but no way to identify callers answers `500` instead of letting the call through.

| Target | How callers are identified |
| --- | --- |
| Go server | `WithPrincipal(func(ctx, r) (*Principal, error))`; an error is a `401` unless it carries `HTTPStatusCode()`, and returning a nil principal with no error is also a `401`, so a rule that never reads `auth` cannot let an unidentified caller through. Handlers read the caller with `PrincipalFromContext(ctx)` |
| TypeScript server | the `principal` option of `createXFetchHandler`, `createXNodeHandler` and `attachXNodeHandlers`; throw an `HttpError` to choose the status. Handlers read `context.principal` |
| Rust server | insert the `Principal` as a request extension from your authentication layer (`request.extensions_mut().insert(principal)`); a request without one is a `401`. Handlers read `context.principal` |

`@authorize` is enforced by Go, TypeScript and Rust servers, and is not supported on `@ws` methods yet (the compiler says so). `onek compat` reports a changed, added or removed authorization rule, and an `@authorize` rule on a method is part of the contract the same way a required scope is.

Rust client and server targets may share the same output directory. Onekit
then writes a complete Rust module tree (`mod.rs`, `types.rs`, `client.rs`,
and `server.rs`) that can be mounted from the containing crate:

```rust
pub mod generated;
```

Generated Rust uses `serde`/`serde_json` for wire types, `reqwest` for the
async client, and `axum` for the server. Depending on the schema features in
use, add these crates to the consuming `Cargo.toml`:

```toml
[dependencies]
async-stream = "0.3" # SSE clients
axum = "0.8"         # rust-server
base64 = "0.22"      # bytes fields
futures-util = "0.3" # SSE
regex = "1"          # @pattern
reqwest = { version = "0.12", default-features = false, features = ["json", "stream", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
serde_with = "3"     # prefixed @flatten fields
url = "2"            # @uri
urlencoding = "2"    # client path parameters
uuid = "1"           # @uuid
validator = "0.20"   # @email
tokio-tungstenite = { version = "0.28", features = ["rustls-tls-webpki-roots"] } # @ws clients
```

`onek check` performs semantic validation as well as parsing: unsupported or
misplaced decorators, invalid validator values, generated-name collisions,
malformed bindings, duplicate routes or headers, invalid error statuses, and
incompatible header/auth contracts are rejected before generation. Add
`--json` for editor/CI diagnostics with path, line, column, code, and message
fields. `onek compat` compares nested types, fields, enums, oneofs, validators,
routes, bindings, headers, streams, and typed errors, including configured
route prefixes; `onek compat --json` emits stable machine-readable findings.
Accept an intentional break with `--allow app.User.email` (repeatable; a
message or service name also covers its fields and routes).
Successful builds write an ignored `.onekit/manifest.json` containing the
schema fingerprint and expected generated outputs.

### Catching breaking schema changes in pull requests

The repository ships a GitHub Action that runs `onek compat` against the pull
request's base branch, comments on the PR with a table of breaking changes (and
which generated targets ship the affected contract), and fails the job. When
the schema is clean it stays silent, and it updates its own comment instead of
adding a new one on every push.

```yaml
on: pull_request
permissions:
  contents: read
  pull-requests: write
jobs:
  schema-compat:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: 1homsi/onekit@v0.21.0
        with:
          allow: |
            app.User.legacy_email
```

Inputs: `directory` (project dir, default `.`), `against` (default: the PR base
branch), `version` (onek release to install, default `latest`; the download is
verified against the published checksums), `allow` (newline-separated paths to
accept), `comment` and `fail-on-breaking` (both default `true`). The action
runs on Linux and macOS runners, and exposes a `breaking` output with the
number of findings. PRs from forks cannot comment with the default token; the
job summary and the failing status still report the result.

Install the CLI:

```bash
go install github.com/1homsi/onekit/cmd/onek@latest
```


### Keeping wire field names in TypeScript

By default generated TypeScript camel-cases field names (`is_default` becomes `isDefault`) and maps them back to the wire name when it encodes and decodes. If your frontend already reads the wire names, keep them in the types instead:

```toml
[generate.ts-client]
out = "./web/client"
field_names = "wire"   # "camel" is the default

[generate.ts-server]
out = "./server/ts"
field_names = "wire"
```

With `"wire"`, `is_default` stays `is_default` in the types, requests, responses, validators and `@rule` checks, and encoding and decoding become the identity for keys. Oneof variant payload keys follow the same rule. Set it on both targets if you generate both, so a client and a server agree on property names.

### Required fields in TypeScript responses

A TypeScript field is optional (`id?: number | undefined`) unless it is `@required`, because a server may omit a field that holds its zero value. With `emit_zero_values = true` every non-optional scalar, enum, repeated and map field is always sent, so the types say so: in a message that some method returns (a response, a stream event or a declared error, directly or nested) and that no method accepts as a request, those fields are required (`id: number`, `is_default: boolean`). Timestamps and singular message fields are required there too: the Go server always writes a timestamp, and writes a message field that the handler left unset as an empty message. A message field whose type can lead back to itself (a tree node's `parent`) is not given this treatment and stays optional, as do `?` fields and `json`. Request messages keep optional fields so callers can send partial objects, and a message used as both a request and a response stays optional. Without `emit_zero_values` nothing changes.

Generated TypeScript compiles under `"strict": true` with `noUnusedLocals` and `noUnusedParameters`: imports, private helpers and parameters that a given schema never uses are not emitted, so the output can sit inside a project that type-checks it.

### One runtime for every TypeScript client

By default each package's `client.ts` declares its own `ApiError`, `TypedApiError`, `RequestValidationError` and `RequestOptions`, plus the response-size, timeout and SSE helpers. With several schema packages, `error instanceof ApiError` only matches errors thrown by clients from the same package, so application code cannot write one `catch` for all of them. Put the runtime in one module instead:

```toml
[generate.ts-client]
out = "./web/api"
runtime = "shared"        # "package" is the default
runtime_dir = "onekitrt"  # relative to out; this is the default
```

The runtime is written once to `web/api/onekitrt/runtime.ts`, and every `client.ts` imports it and re-exports `ApiError`, `TypedApiError`, `RequestValidationError` and `RequestOptions`, so existing imports from a client module keep working and all resolve to the same classes:

```ts
import { ApiError } from "./api/onekitrt/runtime";

try {
  await ordersClient.get({ id });
  await invoicesClient.get({ id });
} catch (error) {
  if (error instanceof ApiError) show(error.code, error.message);   // errors from either client
}
```

The runtime directory must not share a name with a schema directory, and switching `runtime` back removes the shared module on the next build. WebSocket client support is still emitted per package, and `ts-server` keeps its own runtime.

### TypeScript client call shape

Each client method takes one request object holding every field, path parameters included, plus an optional `RequestOptions` (`signal`, `headers`, `timeoutMs`): `client.update({ id, name })`, not `update(id, input)`. A failed call throws `ApiError` carrying `statusCode`, the raw `body`, and, when the response follows the error envelope, `code`, `message` and `requestId`; set `errorParser` in the client options to read a custom envelope. Calls time out after 30 seconds and read at most 8 MiB; both are settable per client (`timeoutMs`, `maxResponseBodyBytes`) and `timeoutMs` per call.

`defaultHeaders` is a record or a function (sync or async) that runs before every request, so a token that changes is read fresh; headers passed per call win over it. `onResponse(res, { method, url })` sees every response before the body is read, and `onUnauthorized(res, { method, url })` runs on a 401 before the `ApiError` is thrown, which is where an app redirects to its sign-in page. A custom `fetch` is only needed for transport changes now.

The client builds every URL as `baseUrl` plus the route path, and the route path already includes the service's `base_path`. So `baseUrl` is the origin the server answers on (`https://api.example.com`, or an empty string for same-origin calls), never the origin plus a prefix. If a service has `base_path: "/api/environments"` and the app is configured with `VITE_API_BASE_URL=/api`, pass `""`, or move the prefix into the schema's `base_path`; passing `/api` would call `/api/api/environments`.

## Validation rules

`@rule(expression, message)` attaches a rule written in a small expression language to a message or a field. The compiler type-checks every rule against the schema, so a typo or a type mismatch is a compile error with the line of the decorator and the column inside the expression. Rules are repeatable.

```onk
message Booking
  @rule("self.nights * self.rooms <= 60", "at most 60 room-nights per booking")
  @rule("size(self.guests) <= self.rooms * 4", "at most four guests per room")
{
  checkin: string @rule("value.matches('[0-9]{4}-[0-9]{2}-[0-9]{2}')", "dates look like 2026-01-31")
  nights: int32 @rule("value >= 1", "book at least one night")
  rooms: int32 @rule("value >= 1", "book at least one room")
  guests: string[]
  coupon: string? @rule("!has(self.coupon) || size(value) == 8", "coupons have eight characters")
}
```

A message rule sees `self`. A field rule sees `self` and `value`, the field itself. A rule must evaluate to `bool`. It is violated when it is false and also when it cannot be evaluated, for example on a division by zero, so a rule can never silently pass.

The language is deliberately small and total: no side effects, no loops other than `all` and `exists`, and identical results in every target.

| | |
| --- | --- |
| Types | `bool`, `int` (64-bit, from `int32`, `uint32` and `int64` fields), `double`, `string`, `bytes`, lists, `map<string, T>`, messages, enums. `uint64`, `timestamp`, `json` and `oneof` fields cannot be used in rules yet. |
| Operators | `\|\|` `&&` `!`, `==` `!=` `<` `<=` `>` `>=`, `in`, `+` `-` `*` `/` `%`, `a ? b : c`, `.field`, `[index]` |
| Functions | `size(x)`, `has(self.field)`, `int(d)`, `double(i)`, `s.startsWith(p)`, `s.endsWith(p)`, `s.contains(p)`, `s.matches('regex')`, `list.all(x, p)`, `list.exists(x, p)` |

Semantics that differ from what you might assume:

- Integer arithmetic is checked. Overflow is an error, `/` truncates toward zero, `%` takes the sign of the dividend, and dividing by zero is an error. Doubles must stay finite.
- Comparisons never mix types: `1 == 1.0` is a compile error, write `double(1) == 1.0`. Strings compare only with `==` and `!=`, and by Unicode code point: `size('é')` is 1 whether the text is composed or not, but the composed and decomposed forms are not equal.
- Unset fields read as their zero value. `has(self.f)` tests presence: set for optional fields and messages, non-zero for other scalars, non-empty for lists, maps and bytes.
- Enum fields compare to string literals naming a value: `self.status == 'ACTIVE'`, checked against the enum when the schema compiles.
- A missing map key or an out-of-range index is an error. Test first with `'k' in self.labels` or `size(self.items) > 0`.
- `matches` must match the whole string and its pattern must be a string literal in a portable regular-expression subset: literals, bracket classes, `|`, `(...)`, `(?:...)`, `* + ?` and `{n,m}`. Anchors, `.`, shorthand classes such as `\d`, lazy quantifiers, lookaround, repetition of a group that contains `|` or another repetition, `\-` outside a bracket class, and a doubled `&&`, `||` or `~~` inside one are rejected, because the supported targets disagree about them. To write a backslash in a pattern inside a `.onk` string, double it twice: `'[0-9]+(\\\\.[0-9]+)?'`.
- An expression is at most 1024 bytes and 48 levels deep; the message is at most 200 characters.

`a ? b : c` produces a `bool`, `int`, `double`, `string` or enum, never a list, map or message.

Changing a rule counts as a breaking change in `onek compat`.

### Where rules are enforced

| Target | Status |
| --- | --- |
| Go (`go-server`, `go-client`) | Enforced by the generated `Validate()`. Servers answer `400` with the failed rule messages in `violations`, and clients refuse to send an invalid request. |
| TypeScript (`ts-client`, `ts-server`) | Enforced by the generated `validate<Message>()`, which returns the failed rule messages. Clients throw `RequestValidationError` and servers answer `400`. Integers are evaluated as `bigint`, so the target must be ES2020 or newer, as the generated server already requires. |
| Python (`python-client`) | Enforced by the generated `validate()`, which raises `ValueError` with the failed rule messages joined by `"; "`; the generated client validates requests before sending. |
| Rust (`rust-client`, `rust-server`) | Enforced by the generated `validate()`, which returns the first failed rule as a `ValidationError` (the field's name for a field rule, empty for a message rule). `rule_violations()` returns every failed rule. A rule that uses `matches()` needs the `regex` crate, like `@pattern`. |
| Dart (`dart-client`) | Enforced by the generated `validate()`, which returns the failed rule messages along with its other violations. Integers use Dart's native 64-bit `int`, so rules need the Dart VM or Flutter on a device; Dart compiled to JavaScript has no 64-bit integers. |
| Swift (`swift-client`) | Enforced by the generated `validate()`, which returns the failed rule messages along with its other violations. Strings are compared by Unicode scalar rather than by Swift's canonical equivalence, so `"\u{e9}"` and `"e\u{301}"` differ here exactly as they do everywhere else. A rule that reads a `@deprecated` field of a message declared in a different namespace sees that field as unset, because the field's storage is private to its own file. |

Every target is held to the same conformance suite (`internal/onkexpr/conformance`): about a hundred rules run over the same inputs and must fail exactly the rules the reference evaluator fails. Go, TypeScript and Python compile each rule to native expressions; Rust, Dart and Swift embed a small evaluator that is a port of the reference one.

## Repository layout

| Path | Contents |
| --- | --- |
| `cmd/onek/` | CLI entrypoint |
| `internal/onklang/` | Lexer, parser, AST for `.onk` |
| `internal/onkcompile/` | Compiles parsed `.onk` files into the IR, resolving cross-file type references |
| `internal/onkir/` | The native intermediate representation every generator consumes |
| `internal/onek/` | `onekit.toml` parsing and the `build`/`check` orchestration |
| `internal/gengo/`, `internal/gents/`, `internal/genpy/`, `internal/gendart/`, `internal/genswift/`, `internal/genrust/`, `internal/genopenapi/` | Generator backends |
| `examples/onk-simple-api/` | A complete, working example with committed generated output |

## Status

onekit is a young project and the language is pre-1.0. It supports messages (scalars including arbitrary `json`, repeated, optional, maps, nested types), enums, discriminated oneofs, field validation (`@email`, `@uuid`, `@uri`, `@pattern`, `@len`, `@range`, `@in` on strings and integers, `@required`, item counts), HTTP path/query/body binding, typed headers and error unions, SSE clients in Go, TypeScript, Python, Dart, and Rust, and Go/TypeScript/Python/Dart/Rust/OpenAPI generators.

JSON mapping is supported through `@flatten`, root-level `@unwrap`, and `@encode(...)` for safe integer, enum, timestamp, and byte representations. Map-value messages must not use `@unwrap`; `onek check` rejects that shape consistently instead of allowing generators to diverge. Generated clients validate requests before sending, generated servers validate decoded requests, and nested validation is emitted consistently across targets. Generated Go servers also provide functional registration options for mux selection, middleware, request IDs, authorization, route metadata, and lifecycle observation.

## AI agents and language servers

`onek mcp` exposes compiler-backed validation, symbols, definitions, references,
and hover to any MCP client. `onek lsp` is a stdio language server with the same
navigation plus diagnostics for unsaved buffers. Both are read-only, honor
`schema_root` in `onekit.toml`, and need no API key.

Point your agent at `onek mcp --dir .` from the project root. For example,
`.mcp.json` (Claude Code, Cursor and most MCP clients):

```json
{
  "mcpServers": {
    "onekit": { "type": "stdio", "command": "onek", "args": ["mcp", "--dir", "."] }
  }
}
```

or `.codex/config.toml` (Codex):

```toml
[mcp_servers.onekit]
command = "onek"
args = ["mcp", "--dir", "."]
```

Editors attach `onek lsp` (optionally `--dir` to pick one schema project in a
monorepo) to `.onk` files. Tool positions are zero-based lines and UTF-16
columns. Type-reference bindings are withheld until the schema compiles.

## License

onekit is released under the [MIT License](LICENSE).
