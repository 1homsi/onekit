# onekit

onekit is a from-scratch schema language and toolchain for building HTTP APIs — no protobuf, no buf, no protoc.

Define your API once in `.onk` files, and generate the boring pieces around it: Go HTTP servers and clients, TypeScript clients and server routes, Python clients, Dart/Flutter clients, Swift clients, Rust clients and Axum servers, and OpenAPI 3.1 documents. Every generator is built from scratch against a native intermediate representation (`internal/onkir`) — there is no `google.golang.org/protobuf` dependency anywhere in this repository.

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

No explicit field numbers, no wire-format baggage, no separate options-extension mechanism — attributes are just `@decorator(args)` on the field or method they apply to. The language is pre-1.0 and evolving; read [`examples/onk-simple-api`](examples/onk-simple-api) for a complete, working example, or `internal/onklang` for the grammar itself.

Two things `.onk` does that protobuf couldn't:

- **RPC error unions** — `-> User | NotFoundError | ValidationError` makes a method's possible errors part of the schema, so generated clients can produce exhaustive, statically-typed error handling instead of "parse the body as any `*Error`."
- **Doc comments** (`///`) that flow straight into generated Go and Dart doc comments, TS/Python docstrings, and OpenAPI descriptions.
  Mark fields and RPCs with `@deprecated` or `@deprecated("reason")` to get `Deprecated:` notes in Go, `@deprecated` in TypeScript, `#[deprecated]` in Rust, `@Deprecated` in Dart, a `DeprecationWarning` from Python client calls, and `deprecated: true` in OpenAPI.

## What it generates

| Package | Purpose |
| --- | --- |
| `internal/gengo` | Go structs, validation, HTTP server (`net/http` `ServeMux`), and HTTP client |
| `internal/gents` | TypeScript types, a `fetch`-based client, and framework-agnostic server routes (Web Fetch API); opt-in zod schemas, TanStack Query/SSE hooks, and MSW handlers |
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
onek import service.proto   # convert a Protocol Buffers service into .onk
```

`onek init` writes `onekit.toml` and a working `api.onk` you can `onek build`
straight away. `--template` picks the stack: `go` (the default), `web` (Go
server plus a TypeScript client), `fullstack` (that plus Dart/Flutter and Swift
clients), `mobile` (Flutter and Swift clients for a backend you already run),
`ts` (TypeScript server and client), and `rust` (axum server and client).
Every template except `go` starts from the same small Todo service (list,
create, get with a typed `NotFound`, and an SSE stream). The starters are
deliberately unopinionated: the generated TypeScript depends on nothing but
`fetch`. Zod schemas, TanStack Query hooks and MSW handlers are opt-in flags
(`zod`, `react_query`, `msw`) on `[generate.ts-client]` for projects that want
them.

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

For Protocol Buffers it has no dependency on protobuf tooling. It converts
messages (nested ones are flattened to `OuterInner`), enums, `repeated`,
`optional`, `map<,>` fields, `oneof`, `google.protobuf` well-known types
(`Timestamp` becomes `timestamp`, `Struct` and `Value` become `json`, wrappers
become optional scalars, `Empty` becomes an empty message), doc comments, and
`deprecated` options. Services become onekit services: `google.api.http` rules
give each RPC its verb, path and `body`; an RPC without one becomes
`POST /<package>.<Service>/<Method>` with all fields in the body. Server
streaming becomes `@stream` (which needs a GET, so the request fields become
query parameters). Client and bidirectional streaming have no HTTP mapping and
are skipped with a warning.

Things to know before relying on the result: onekit's oneofs use a
discriminated encoding that differs from protobuf JSON, field names keep their
`snake_case` form where protobuf JSON defaults to `lowerCamelCase`, and
imported `.proto` files are not followed (their types become `json` with a
warning). Regenerate both ends from the converted schema rather than mixing
generated code with an existing gRPC-JSON gateway.

### Try it in the browser

`make playground` builds a static page into `playground/dist` (`index.html`,
`onek.wasm`, `wasm_exec.js`): the real compiler and generators, compiled to
WebAssembly, so you can edit a schema and read the Go, TypeScript, Python,
Dart, Swift, Rust and OpenAPI output with no install. Serve the folder with any static
file server (for example `python3 -m http.server -d playground/dist`). It
formats schemas, shows diagnostics with line and column, and shares a schema as
a link (the schema travels in the URL fragment and never leaves the browser).

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

The `ts-client` target accepts opt-in flags that emit companion modules next
to `types.ts` and `client.ts`:

```toml
[generate.ts-client]
out = "./web/client"
zod = true         # schemas.ts  - zod mirrors of every message and validator
react_query = true # query.ts    - TanStack Query hooks + resilient SSE hook
msw = true         # msw.ts      - Mock Service Worker handlers per route
```

- **schemas.ts** maps each field to the zod constraint its server enforces
  (`@email` → `.email()`, `@len(2,8)` → `.min(2).max(8)`, `?` → `.optional()`,
  int64/timestamp/bytes wire encodings, oneof discriminated unions), so forms
  validate against the exact API contract.
- **query.ts** exposes `createUserServiceHooks(client)` factories: GET routes
  become `useQuery` hooks keyed by service/method/request, body-bearing
  routes become `useMutation` hooks that invalidate their service scope, and
  SSE routes become a reconnecting `useXEvents(req)` hook with exponential
  backoff and abort-safe teardown. Helpers `isApiError` and `errorMessage`
  round out typed error handling for RPC error unions.

  ```ts
  const hooks = createUserServiceHooks(new UserServiceClient("/v1"));
  const user = hooks.useGetUser({ id });            // useQuery
  const create = hooks.useCreateUser();             // useMutation
  const ticks = hooks.useWatchTicks(req);           // SSE: events/latest/error/connected
  ```

- **msw.ts** emits deterministic fixtures derived from validators
  (`@uuid` → real UUID shape, `@in(...)` → first allowed value, encodings
  honored) so component tests intercept fetch with contract-accurate data:
  `worker.use(...userServiceHandlers)`.

Peer dependencies are only required for enabled flags: `zod`,
`@tanstack/react-query` (+ `react`), and `msw`.

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
| Go server | `WithPrincipal(func(ctx, r) (*Principal, error))`; an error is a `401` unless it carries `HTTPStatusCode()`. Handlers read the caller with `PrincipalFromContext(ctx)` |
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

This is a young project that has completed its migration from the earlier protobuf-based design. It supports messages (scalars including arbitrary `json`, repeated, optional, maps, nested types), enums, discriminated oneofs, field validation (`@email`, `@uuid`, `@uri`, `@pattern`, `@len`, `@range`, `@in` on strings and integers, `@required`, item counts), HTTP path/query/body binding, typed headers and error unions, SSE clients in Go, TypeScript, Python, Dart, and Rust, and Go/TypeScript/Python/Dart/Rust/OpenAPI generators.

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
