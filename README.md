# onekit

onekit is a from-scratch schema language and toolchain for building HTTP APIs — no protobuf, no buf, no protoc.

Define your API once in `.onk` files, and generate the boring pieces around it: Go HTTP servers and clients, TypeScript clients and server routes, Python clients, Rust clients and Axum servers, and OpenAPI 3.1 documents. Every generator is built from scratch against a native intermediate representation (`internal/onkir`) — there is no `google.golang.org/protobuf` dependency anywhere in this repository.

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
- **Doc comments** (`///`) that flow straight into generated Go doc comments, TS/Python docstrings, and OpenAPI descriptions.

## What it generates

| Package | Purpose |
| --- | --- |
| `internal/gengo` | Go structs, validation, HTTP server (`net/http` `ServeMux`), and HTTP client |
| `internal/gents` | TypeScript types, a `fetch`-based client, and framework-agnostic server routes (Web Fetch API); opt-in zod schemas, TanStack Query/SSE hooks, and MSW handlers |
| `internal/genpy` | Python `@dataclass` models, `IntEnum` enums, and a stdlib (`urllib`) client |
| `internal/genrust` | Rust Serde models and validation, a `reqwest` client, and an Axum server/router |
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

[generate.rust-client]
out = "./src/generated"

[generate.rust-server]
out = "./src/generated"

[generate.openapi]
out = "./docs"
title = "Your API"
version = "1.0.0"
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
onek init ./my-api
onek watch   # rebuild on schema/config changes until interrupted
onek mock    # dev server serving schema-derived fixtures for every route
```

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
  TypeScript: promise-based `receive()`; Python/Rust: `send`/`receive`)
  instead of a one-shot response.

Peer dependencies per target, only when the schema uses `@ws`: Go needs
`github.com/coder/websocket`, Python needs `websockets>=12`, the Rust client
needs `tokio-tungstenite`; servers reuse their existing framework sockets
(axum / Web-standard `WebSocketPair`), except the TypeScript Node adapter
below, which needs the `ws` package.

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
snapshot tests stable.

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
Successful builds write an ignored `.onekit/manifest.json` containing the
schema fingerprint and expected generated outputs.

Install the CLI:

```bash
go install github.com/1homsi/onekit/cmd/onek@latest
```

## Repository layout

| Path | Contents |
| --- | --- |
| `cmd/onek/` | CLI entrypoint |
| `internal/onklang/` | Lexer, parser, AST for `.onk` |
| `internal/onkcompile/` | Compiles parsed `.onk` files into the IR, resolving cross-file type references |
| `internal/onkir/` | The native intermediate representation every generator consumes |
| `internal/onek/` | `onekit.toml` parsing and the `build`/`check` orchestration |
| `internal/gengo/`, `internal/gents/`, `internal/genpy/`, `internal/genrust/`, `internal/genopenapi/` | Generator backends |
| `examples/onk-simple-api/` | A complete, working example with committed generated output |

## Status

This is a young project that has completed its migration from the earlier protobuf-based design. It supports messages (scalars including arbitrary `json`, repeated, optional, maps, nested types), enums, discriminated oneofs, field validation (`@email`, `@uuid`, `@uri`, `@pattern`, `@len`, `@range`, `@in`, `@required`, item counts), HTTP path/query/body binding, typed headers and error unions, SSE clients in Go, TypeScript, Python, and Rust, and Go/TypeScript/Python/Rust/OpenAPI generators.

JSON mapping is supported through `@flatten`, root-level `@unwrap`, and `@encode(...)` for safe integer, enum, timestamp, and byte representations. Map-value messages must not use `@unwrap`; `onek check` rejects that shape consistently instead of allowing generators to diverge. Generated clients validate requests before sending, generated servers validate decoded requests, and nested validation is emitted consistently across targets. Generated Go servers also provide functional registration options for mux selection, middleware, request IDs, authorization, route metadata, and lifecycle observation.

## AI agents and language servers

`onek mcp` exposes compiler-backed validation, symbols, definitions, references,
and hover information to Codex and Claude Code. `onek lsp` offers the same
navigation plus diagnostics for unsaved editor buffers. Repository-local MCP
configuration and a Claude Code LSP plugin are included; see
[AI tooling setup](docs/AI_TOOLING.md) for installation, project selection, and
supported capabilities.

## License

onekit is released under the [MIT License](LICENSE).
