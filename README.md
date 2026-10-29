<p align="center">
  <img src=".github/assets/onekit-mascot.svg" alt="Kit, the onekit mascot: a pixel-art blue robot with curly-brace ears" width="300">
</p>

# onekit

onekit generates the code around an HTTP API from its [TypeSpec](https://typespec.io) definition: Go servers and clients, TypeScript clients and server routes, Python clients, Dart/Flutter clients, Swift clients, Rust clients and Axum servers, and OpenAPI 3.1 documents.

You describe the API in TypeSpec, the `@onkt/typespec` emitter turns it into onekit's schema, and the `onkt` CLI compiles that into a single intermediate representation (`internal/onkir`) that every generator reads, so a rule written once in the schema behaves the same way in every language.

## Writing an API in TypeSpec

```typespec
import "@typespec/http";
import "@onkt/typespec";

using TypeSpec.Http;

namespace example.users;

model User {
  id: string;
  name: string;
  email: string;
}

model CreateUserRequest {
  @minLength(2) @maxLength(100) name: string;
  @format("email") email: string;
}

@error
model NotFound {
  @statusCode _: 404;
  message: string;
}

alias ApiKey = {
  @header("X-API-Key") @format("uuid") apiKey: string;
};

@route("/v1")
interface UserService {
  @post @route("/users") createUser(...ApiKey, ...CreateUserRequest): User;
}
```

Everything TypeSpec already has a word for is read from TypeSpec itself: `@route` and the verb decorators, `@header`, `@query`, `@body`, `@minLength`/`@maxLength`, `@minValue`/`@maxValue` and their exclusive forms, `@minItems`/`@maxItems`, `@pattern`, `@format("email" | "uuid" | "uri")`, `@encode`, doc comments, `#deprecated`, `T | null`, enums, and `@error` models with a `@statusCode`.

Two things onekit is built around:

- **Error unions**: `createUser(...): User | NotFound | ValidationError` makes a method's possible errors part of the schema, so generated clients offer exhaustive, statically typed error handling.
- **Doc comments** (`/** */` or `@doc`) that flow straight into generated Go and Dart doc comments, TS/Python docstrings, and OpenAPI descriptions. `#deprecated "reason"` on a property or operation gives `Deprecated:` notes in Go, `@deprecated` in TypeScript, `#[deprecated]` in Rust, `@Deprecated` in Dart, a `DeprecationWarning` from Python client calls, and `deprecated: true` in OpenAPI.

### How the schema maps

| TypeSpec | onekit |
| --- | --- |
| `model` | message; spread a model into an operation (`op f(...M)`) and `M` is the request message |
| `interface` | a service; each operation is an RPC. Its `@route` (after its namespaces') is the base path; without one the base path comes from the source directory, `hr/time_entry/v1` serving under `/hr/time-entry/v1`, and `@route("/")` serves it at the root |
| `A \| B` return type with `@error` models | the response and the typed errors |
| `{ @statusCode _: 201; @body body: T }` | a `201` success status |
| `@body` on one request field | that field is the whole body; a sibling that is not a path, `@query` or `@header` parameter needs `@bodyIgnore` and does not cross the wire |
| `@header` on an operation parameter | a header contract; one spread into every operation (`...ApiKey`) belongs to the whole service |
| `@useAuth(BearerAuth)`, `BasicAuth`, `ApiKeyAuth<ApiKeyLocation.header, "X-Key">`, OAuth2 | a required credential header (`Authorization` or the key's header); on the service when every operation shares it, otherwise on each operation, so `@useAuth(NoAuth)` makes one public |
| a named `union` of models | a oneof written `{"type": "<variant>", "<variant>": {...}}`; `@Onkt.discriminator("kind")` renames the key |
| `@discriminated(#{ discriminatorPropertyName: "kind", envelope: "none" })` union | a flattened oneof, the variant's fields next to the discriminator |
| `"a" \| "b"` string property | an allowed-values check |
| `x?: T \| null` | a nullable field: absent, `null` and a value survive a round trip |
| `Record<T>` | a map with string keys (`@Onkt.mapKey("int64")` for integer keys) |
| `int64`, `int64[]` | JSON numbers; `@encode(string)` (`@Onkt.encode("string")` on an array) crosses as strings |
| `utcDateTime`, `plainDate`, `unknown`, `bytes` | timestamp, a date, any JSON value, base64 bytes |
| `@route("/{+path}")` | a wildcard tail parameter |

### onkt decorators

The `Onkt` namespace adds what TypeSpec has no decorator for:

| Decorator | Meaning |
| --- | --- |
| `@Onkt.stream` | the operation answers with server-sent events of its return type |
| `@Onkt.ws` | the operation is a bidirectional WebSocket call |
| `@Onkt.authorize(expr, message)`, `@Onkt.principal` | authorization rules the server enforces, and the model that describes the caller |
| `@Onkt.requires(...scopes)`, `@Onkt.guard(...)`, `@Onkt.meta(key, value)` | scopes, guards and metadata handed to server code with the route |
| `@Onkt.rule(expr, message)` | a validation rule in onekit's expression language |
| `@Onkt.required` | the value must be present and non-empty |
| `@Onkt.http(#{ contentType, request, response })` | a route handled by hand-written code |
| `@Onkt.maxBody("64MiB")`, `@Onkt.method("ANY" \| "QUERY")` | a per-method body limit, and the methods TypeSpec has no verb for |
| `@Onkt.flatten`, `@Onkt.unwrap`, `@Onkt.empty`, `@Onkt.object`, `@Onkt.encode` | JSON mapping |
| `@Onkt.wsId`, `@Onkt.wsTimeout`, `@Onkt.wsCancel`, `@Onkt.raw` | WebSocket correlation, deadlines, cancellation and binary frames |
| `@Onkt.name("Branding")` | the service name an interface generates under, for when a model or enum of its namespace already has that name |
| `@Onkt.tag`, `@Onkt.json`, `@Onkt.values`, `@Onkt.range`, `@Onkt.example`, `@Onkt.auth`, `@Onkt.parent` | the rest of onekit's schema vocabulary |

Each decorator is documented in [`typespec/lib/main.tsp`](typespec/lib/main.tsp), and [`examples/simple-api`](examples/simple-api) is a complete, working project.

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

All targets are driven off the same compiled schema (`internal/onkir`), produced from the emitter's schema document (`internal/onkschema`) by `internal/onkcompile`.

## Quick start

`onkt` compiles TypeSpec with the project's own TypeSpec toolchain, so a project needs Node.js and npm.
The CLI itself is on npm too, so a project pins it in `package.json` beside the
emitter and everyone on the project, CI included, runs the same version:

```bash
npx @onkt/cli init ./my-api # onkt.toml, main.tsp and package.json
cd my-api
npm install                 # @onkt/cli, @onkt/typespec, @typespec/compiler, @typespec/http
npx onkt build              # generates everything configured in onkt.toml
```

The `@onkt/cli` package is a small launcher; its command is `onkt`. The first time it runs, it downloads the
binary for the machine (macOS, Linux and Windows, on x64 and arm64; the Linux
binary is static, so Alpine works) from the GitHub release of its own version,
checks it against the release's `checksums.txt`, and caches it under the user
cache directory (`~/.cache/onekit/releases/<version>` on Linux). It needs no
install script, so `--ignore-scripts` and pnpm are fine. Set
`ONKT_DOWNLOAD_BASE` to a mirror of `https://github.com/1homsi/onekit/releases/download`
or `ONKT_CACHE_DIR` to move the cache. Keep `@onkt/cli` and `@onkt/typespec` on the
same version: they are released together. To add both to an existing project:

```bash
npm install --save-dev @onkt/cli @onkt/typespec @typespec/compiler @typespec/http
```

To work on onekit itself:

```bash
git clone https://github.com/1homsi/onekit.git
cd onekit
make build          # builds ./bin/onkt
make typespec       # installs the npm workspace and builds the @onkt/typespec emitter
cd examples/simple-api
go test ./...        # exercises the already-generated code end to end
../../bin/onkt build # regenerates api/*.gen.go and docs/openapi.{yaml,json} from main.tsp and models.tsp
```

**On Windows**, the released `onkt.exe` binary works standalone (with Node.js
for TypeSpec). Building the repo itself (`make build`, `make check-generated`,
`scripts/run_tests.sh`) needs GNU Make and a POSIX-ish shell, neither of
which ship with Windows by default. CI runs this on `windows-latest` via
Chocolatey's `make` package plus the `sh`/`bash`/coreutils that come with Git
for Windows (already on `PATH` if you have Git installed); the same setup
(`choco install make`, and Git for Windows for `git`) works locally. WSL or
a Git Bash terminal is the simplest way to get all of it at once.

## Moving from `.onk`

onekit used to have its own schema language, `.onk`. `onkt migrate` converts a
project: it writes a `.tsp` file next to every `.onk` file, a `main.tsp` that
imports them, and a `package.json` for the TypeSpec toolchain, renames
`onekit.toml` to `onkt.toml`, moves a pinned `version` to the release doing
the migration, and removes the `.onk` files (`--keep` leaves them). A project
that already has a `package.json` keeps it; `onkt migrate` prints the
`npm install --save-dev` line that adds the TypeSpec toolchain to it. A project that kept `.onk` schemas in `.proto` files with
`schema_extensions` is converted too, and real Protocol Buffers files are left
alone. Then run `npm install` and `onkt build`; the generated code is the same
as before, byte for byte. `onkt compat --against main` still reads a `.onk`
baseline, so the move can be checked against the schema it replaces.

The CLI used to be called `onek`. Releases still carry the binary under that
name too, and `onekit.toml` and the `ONEK_*` environment variables are still
read, so existing scripts keep working while you switch.

## The `onkt` CLI

A project is a directory with an `onkt.toml`, a `main.tsp` and a `package.json`:

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

TypeSpec compiles from `main.tsp` at the schema root, and every other `.tsp`
file is reached through its imports. Without a `main.tsp`, onkt compiles every
`.tsp` file under the schema root together. The directory a declaration's source
file sits in decides its generated package, so a project with several
services keeps each in its own directory.

The OpenAPI target writes one YAML/JSON pair per service, mirroring the schema
directory structure: `api/hub/business/v1` produces
`docs/hub/business/v1/openapi.yaml` and `openapi.json`. Each document includes
only that service and its transitive schema dependencies, including shared types.
Directories containing multiple services use `<ServiceName>.openapi.yaml` and
`<ServiceName>.openapi.json`. Model-only directories do not emit documents.

`route_prefix` is optional. It prepends a public HTTP prefix to every generated
server, client, and OpenAPI route without changing generated package or import
paths.

`schema_root` is optional and lets the schema tree live in a subdirectory of
the project (the directory containing `onkt.toml`) while generator outputs
stay anchored at the project root:

```toml
# onkt.toml at the repository root; schemas live in api/
module = "github.com/you/yourapp/gen/go"
schema_root = "api"

[generate.go-server]
out = "gen/go"
```

Package import paths mirror directories under `schema_root`; output
containment keeps being validated against the project directory, and the
drift manifest stays at `<project>/.onekit/manifest.json` with `schema_files`
recorded relative to the schema root.

The prefix must be a canonical literal URL path such as `/api` or
`/api/internal`: it must start with `/`, must not end with `/`, and cannot
contain query strings, fragments, percent escapes, or path parameters.

```bash
onkt check   # compile the TypeSpec and check it, no codegen - fast validation
onkt build   # compile + generate everything configured in onkt.toml
onkt init ./my-api                          # Go server and client
onkt init --template fullstack ./my-app     # Go server + TypeScript, Flutter and Swift clients
onkt init --list-templates                  # every starter project
onkt watch   # rebuild on schema/config changes until interrupted
onkt mock    # dev server serving schema-derived fixtures for every route
onkt import api.yaml        # convert an OpenAPI 3.x document into TypeSpec
onkt migrate                # convert a .onk project to TypeSpec
```

Format TypeSpec with `npx tsp format "**/*.tsp"`, and use the TypeSpec
extension for VS Code (or any editor with the TypeSpec language server) for
completion, navigation and diagnostics. `onkt check --json` reports TypeSpec
and onekit diagnostics alike with path, line and column.

`onkt init` writes `onkt.toml`, `main.tsp` and `package.json`; after
`npm install` the project builds straight away. `--template` picks the stack:
`go` (the default), `web` (Go server plus a TypeScript client), `fullstack`
(that plus Dart/Flutter and Swift clients), `mobile` (Flutter and Swift
clients for a backend you already run), `ts` (TypeScript server and client),
and `rust` (axum server and client). Every template except `go` starts from
the same small Todo service (list, create, get with a typed `NotFound`, and
an SSE stream). The starters are deliberately unopinionated: the generated
TypeScript depends on nothing but `fetch`. MSW handlers are an opt-in flag
(`msw`) on `[generate.ts-client]` for projects that want them.

`onkt build` is incremental: it keeps a small per-project record of the last
successful build in your user cache directory (outside the repository). When
the schemas, `onkt.toml` and the `onkt` binary are all unchanged, and every
generated file is still exactly as it was written, the build is skipped and
reports `up to date`. Editing, deleting, or re-checking-out any generated file
triggers a normal rebuild. `onkt build --check` never uses the cache. Set
`ONKT_NO_CACHE=1` to disable it, or `ONKT_CACHE_DIR` to relocate it.

When only some schemas changed, the build regenerates only the packages that
depend on them. A package is kept when its own schema files, the schema files
of every package whose types it uses, the list of packages, `onkt.toml` and
the `onkt` binary are all unchanged, and its generated files are still exactly
as the last build wrote them. The Go, TypeScript, Python and OpenAPI outputs
are kept per package; the Dart, Swift and Rust targets, which write
project-wide files, always regenerate.

### Importing an existing API

`onkt import` turns an OpenAPI 3.x document into a TypeSpec schema you can
check and build, and prints a warning for everything it cannot express instead
of dropping it silently. It refuses to write a schema that does not pass
`onkt check`, and writes a `main.tsp` importing it when the output directory
has none. Protocol Buffers are not supported.

## Streaming over POST with typed events

`@Onkt.stream` works with any verb. With a body-bearing verb the request travels as a JSON body, so large requests (messages, images) stream a response without query-string limits:

```typespec
@Onkt.discriminator("type")
union TurnEventPayload {
  @Onkt.tag("text") text: Text,
  @Onkt.tag("done") done: Done,
}

model TurnEvent {
  payload: TurnEventPayload;
}

interface Agent {
  @post @route("/turn") @Onkt.stream turn(...TurnRequest): TurnEvent;
}
```

When the response message holds a `oneof`, every SSE frame is named after the variant it carries (`event: text`, `event: done`), and `data:` is the response message as usual. Clients in every target still decode each frame into the response type, so a `switch` over the oneof is all a consumer needs. A frame named `error` stays reserved for failures after the stream has started.

Servers send `: ping` comment frames every 15 seconds, which also commits the response headers when the first event is slow, so a client or proxy header timeout (the Go client defaults to 30 seconds) cannot cut off a quiet stream. Tune it with `WithSSEHeartbeat(d)` in Go (0 disables) and `sseHeartbeatMs` in the TypeScript `ServerOptions`. The Rust server uses axum's default keep-alive.

## Bidirectional WebSocket streaming

Alongside SSE (`@Onkt.stream`), a method can be declared as a bidirectional
WebSocket RPC. Request messages flow client-to-server and response messages
server-to-client as JSON frames of the declared types:

```typespec
@route("/v1")
interface ChatService {
  @Onkt.ws @route("/rooms/{room}") chat(...ChatMessage): ChatEvent;
}
```

- Path and query parameters bind from the connect request; header contracts
  are checked before the upgrade.
- Servers validate every inbound frame; protocol violations receive an
  `{"error": ...}` frame followed by close code 1008.
- Generated clients return a duplex handle (Go: `Send`/`Receive`/`Close`;
  TypeScript: promise-based `receive()`; Python/Rust/Dart: `send`/`receive`)
  instead of a one-shot response.

Peer dependencies per target, only when the schema uses `@Onkt.ws`: Go needs
`github.com/coder/websocket`, Python needs `websockets>=12`, Dart needs
`web_socket_channel`, the Rust client
needs `tokio-tungstenite`; servers reuse their existing framework sockets
(axum / Web-standard `WebSocketPair`), except the TypeScript Node adapter
below, which needs the `ws` package.

### Using the Dart client from Flutter

`dart-client` writes a shared runtime (`onekit.dart`, plus `onekit_ws*.dart`
when the schema uses `@Onkt.ws`) at the output root and a `models.dart` and
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
a WebSocket handshake, so `@Onkt.ws` methods throw `UnsupportedError` there when the
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

Limit of the Swift target so far: `@Onkt.ws` methods are not generated (a schema
with only `@Onkt.ws` methods gets no client file).

### Deploying a generated TypeScript `@Onkt.ws` server

`ts-server` emits two entry points per service with `@Onkt.ws` methods, so the
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

Regular (non-`@Onkt.ws`) routes get the same treatment. `attachChatServiceNodeHandlers(server, handlerImpl)`
serves them on an `http.Server`, and an unmatched path gets a 404 when nothing else
listens. A request that cannot become a Fetch `Request`, such as one with a malformed
`Host` header or credentials in its URL, gets a 400 with the code `bad_request` through
the `onError` writer. `createChatServiceNodeHandler(handlerImpl)` returns `(req, res) => boolean`
for composing with your own request listener. Both take any object shaped like
`node:http`'s, so the generated server needs neither `@types/node` nor `ws`
unless it has `@Onkt.ws` methods.

### Multiplexed correlated calls with `@Onkt.wsId`

A single `@Onkt.ws` connection can carry many independent, concurrent exchanges -
for example a server that pushes an arbitrary number of asynchronous
`HostCall` frames while handling one long-running request, each of which the
client must answer with a matching `HostResult` before the server continues,
all interleaved and resolved out of order. Model the frame as a `oneof` and
mark whichever field carries the correlation key with `@Onkt.wsId`:

```typespec
model HostCall {
  @Onkt.wsId id: string;
  method: string;
}

model HostResult {
  @Onkt.wsId id: string;
  value: string;
}

@Onkt.discriminator("type")
union FramePayload {
  @Onkt.tag("run") run: RunRequest,
  @Onkt.tag("host_call") host_call: HostCall,
  @Onkt.tag("host_result") host_result: HostResult,
  @Onkt.tag("run_result") run_result: RunResult,
}

model Frame {
  payload: FramePayload;
}

@route("/v1")
interface Runtime {
  @Onkt.ws @route("/execute") execute(...Frame): Frame;
}
```

`@Onkt.wsId` is a plain field-level decorator (like `@Onkt.required`): put it on a
non-repeated `string`/`int32`/`int64`/`uint32`/`uint64` field, either directly
on a `@Onkt.ws` method's request/response message or inside one of its oneof
variants. Every `@Onkt.wsId` field a single method touches (across both
directions) must resolve to the same scalar type - `onkt check` rejects a
mix, and rejects more than one `@Onkt.wsId` field in the same message or variant.

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
otherwise applies to `@Onkt.ws` even without correlation: every send is now
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
one oneof variant with `@Onkt.wsCancel`. Its message must carry the `@Onkt.wsId`:

```typespec
model Cancel {
  @Onkt.wsId id: string;
}

@Onkt.discriminator("type")
union FramePayload {
  // ...the other variants
  @Onkt.tag("cancel") @Onkt.wsCancel cancel: Cancel,
}

model Frame {
  payload: FramePayload;
}
```

Every backend then sends `{"type": "cancel", "cancel": {"id": ...}}` when a
call is abandoned. A cancel frame is never treated as a reply: it arrives at
the peer's handler (server) or `receive()` (client) like any other frame, and
the peer decides what stopping means.

### Deadline propagation with `@Onkt.wsTimeout`

Mark an integer field next to a call's `@Onkt.wsId` with `@Onkt.wsTimeout`:

```typespec
model HostCall {
  @Onkt.wsId id: string;
  method: string;
  @Onkt.wsTimeout @encode(string) timeout_ms: int64;
}
```

When a call carries a deadline, the generated code writes the remaining
milliseconds into that field on a copy of the frame (the caller's value is
never modified, and an explicitly set value wins). The deadline comes from Go's
`ctx` deadline, TypeScript's `timeoutMs`, or Rust's `call_timeout`. The
receiving handler reads it and can give up on work the caller will no longer
wait for.

### Errors on the wire

A `@Onkt.ws` connection never carries off-schema frames. When the server rejects
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
Rust (`None` disables). Clients of methods that use `@Onkt.wsId` ping too, every
30 seconds by default, and fail in-flight calls with a closed error when a
pong goes missing: `WSPingInterval` in Go, `with_ws_ping_interval` in Rust.
Browsers and Node's built-in `WebSocket` offer no ping API, so TypeScript
clients rely on the server's pings. A client that receives a frame it can't decode fails
the socket (a 1007 `WSClosedError` in TypeScript, the decode error in Go)
rather than skipping it.

### Large payloads with `@Onkt.raw`

Mark a non-repeated, non-optional `string` or `bytes` field `@Onkt.raw` to carry it
outside the JSON:

```typespec
model RunResult {
  exit_code: int32;
  @Onkt.raw result_json: string;
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

| | JSON | `@Onkt.raw` |
| --- | --- | --- |
| Go encode | 2252 µs (v0.16), 635 µs now | 24.6 µs |
| Go decode | 3470 µs (v0.16), 1642 µs now | 0.6 µs |
| Node encode | 665 µs | 52 µs |
| Node decode | 667 µs | 25 µs |
| Go client ↔ Go server round trip | 3347 µs | 160 µs |

### Messages over 16 MiB

A message larger than 16 MiB (JSON text or a `@Onkt.raw` binary frame) is sent as
8 MiB binary chunks. Each chunk carries a 13-byte header: the marker
`0xFFFFFFFF`, a kind byte (0 text, 1 binary) and the total length as a
big-endian u64. The receiver reassembles them before decoding, so `@Onkt.raw`
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
and `@Onkt.rule`s, so forms can check input against the exact API contract without a
schema library. One companion module is opt-in on the `ts-client` target:

```toml
[generate.ts-client]
out = "./web/client"
msw = true         # msw.ts - Mock Service Worker handlers per route
```

- **msw.ts** emits deterministic fixtures derived from validators
  (`@format("uuid")` → real UUID shape, `@in(...)` → first allowed value, encodings
  honored) so component tests intercept fetch with contract-accurate data:
  `worker.use(...userServiceHandlers)`. The `msw` package is a peer dependency
  only when the flag is on.

The generated TypeScript depends on nothing but `fetch`. The client is a plain
class, so it slots into whatever data layer your app already uses: wrap a call
in your own `useQuery`, or validate with your own schema library, in a few lines.

### The mock server

`onkt mock` compiles your schema tree and serves every route with
schema-derived JSON - realistic values from validators, correct wire
encodings, declared error statuses, and SSE streams that emit three frames. A
route implemented as a plain handler (`@Onkt.http`) answers `501`, because the mock
cannot know what the handler writes:

```bash
onkt mock --dir api --addr :8080 \
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

### Nullable fields with `T | null`

An optional property (`?`) is omitted from the wire when unset. Make its type `T | null` and it has three states that survive a round trip: absent, an explicit `null`, and a value. This is what a PATCH body needs to tell "leave it alone" from "clear it".

```typespec
model UpdateEnvironment {
  id: int64;
  name?: string | null;
  folder_id?: int64 | null;
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

In a message that only responses use (some RPC returns it and no RPC accepts it), a nullable field is always on the wire: Go writes `null` for an unset field without needing the `Null` flag, so the TypeScript property is required and typed `T | null` (`folder_id: number | null`, `app: App | null`) and the decoder turns a missing key into `null`. A message that is also a request keeps the three-state form above. A plain `?` property without `| null` stays optional, since Go omits it when unset.

`| null` needs the `?` marker and a plain wire form: it cannot be combined with `@Onkt.required`, `@query`, `@Onkt.flatten`, `@Onkt.unwrap`, `@Onkt.empty`, an `@encode` on an enum, message or timestamp, or `bytes`, and a 64-bit integer must be numeric (a plain `int64`, not `@encode(string)`). Rules and validators treat `null` as absent.

### Wildcard path parameters and empty routes

A path parameter written `{+name}` captures the rest of the path, slashes included. It must be the last segment, bind to a `string` request field, and cannot be used on `@Onkt.ws` routes.

```typespec
@route("/files")
interface Files {
  @get @route("/{+path}") read(...FileRef): Content;
  @get root(...Empty): Content;
}
```

`GET /files/dir/sub/b.txt` delivers `dir/sub/b.txt` to `FileRef.path`, percent-decoded. Generated clients escape each segment separately and keep the slashes. OpenAPI documents the route as `/files/{path}` and marks the parameter with `x-onekit-wildcard: true`.

`@get("")` (any verb) is allowed when the service has a base path, and serves exactly the base path. An exact route wins over a wildcard route that could also match. The Go, TypeScript and mock servers match `/files/` with an empty `path`; the Rust server (axum) requires at least one character.

### Route metadata with `@Onkt.meta`

`@Onkt.meta(key, value)` attaches a free-form pair to a method and hands it to your own code at runtime, so facts about a route that your middleware needs, such as the permission it checks or the audit event it records, live in the schema next to the route instead of in a second table:

```typespec
@post @route("/apps/{slug}/edit")
@Onkt.meta("guard", "app/edit/:slug")
@Onkt.meta("audit.event", "app.update")
edit(...EditApp): App;
```

It is repeatable, keys are lower-case (`[a-z][a-z0-9_.-]*`) and unique per method, and values are 1 to 200 bytes. It is not supported on `@Onkt.ws` methods. onekit attaches no meaning to the pairs.

| Target | Where you read it |
| --- | --- |
| Go server | `RequestMetadata.Meta` / `MetaValue(key)`, in an `Authorizer`, in middleware via `RequestMetadataFromContext(ctx)`, or in the handler. Middleware runs outside the authorizer, so it also sees requests the authorizer denies, which is what an audit log wants |
| TypeScript server | `route.meta` in `authorize(req, route)`, and `context.meta` in the handler |
| Rust server | `context.meta_value(key)` and `context.meta` in the handler |
| OpenAPI | `x-onekit-meta` on the operation |
| `onkt compat` | adding, removing or changing a pair is reported as a contract change |

A permission guard keyed on a path parameter is then a few lines: read `guard` from the metadata, read the parameter with `r.PathValue("slug")`, and decide.

### Per-method body limits with `@Onkt.maxBody`

Servers cap request bodies at 8 MiB by default (`WithMaxRequestBodyBytes` in Go sets one value for a whole `Register...Server` call). `@Onkt.maxBody` sets a different cap for one method, so a large upload and a small form can live in the same service:

```typespec
interface Agent {
  @post @route("/turn") @Onkt.stream @Onkt.maxBody("64MiB") turn(...TurnRequest): TurnEvent;
  @post @route("/rename") rename(...RenameRequest): Renamed;
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
- **Guards.** `@Onkt.guard("object/level/:id")` on an RPC attaches permission patterns whose `:name` segments must be path parameters of the route. Guards are data for your authorizer, not a check on their own: a route with `@Onkt.guard` and no `WithAuthorizer` is not protected. `ResolveGuards` refuses a path value that is empty, `.`, `..` or contains `/`, `\` or a control character (a percent-encoded `%2F` decodes to a slash inside one path segment), so a caller cannot reshape the key you check. The server passes them to your authorizer as `RequestMetadata.Guards`, and `httpkit.ResolveGuards(meta.Guards, r.PathValue)` fills in the request's values:

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

Some routes do not fit a message-in, message-out shape: a JavaScript bundle with an `ETag`, an image, an upload that streams to object storage. Declare them in the schema with `@Onkt.http` and implement them as ordinary `http.Handler` code. They still get the route, the generated bind-and-validate step, `@Onkt.guard`, `@Onkt.meta`, `@Onkt.maxBody`, middleware, the authorizer and the request observer.

```typespec
model BundleRequest {
  slug: string;
  @query v: int32;
}

model UploadRequest {
  slug: string;
  key: string;
}

model Nothing {}

@route("/apps")
interface Apps {
  @get @route("/{slug}/bundle.js")
  @Onkt.http(#{ contentType: "application/javascript" })
  @Onkt.guard("apps/read/:slug")
  bundle(...BundleRequest): Nothing;

  @put @route("/{slug}/objects/{+key}") @Onkt.http @Onkt.maxBody("64MiB")
  put_object(...UploadRequest): Nothing;
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

An error returned before the handler has written anything goes through the error writer, so it uses the same envelope as every other route; an error returned after the response started is not written on top of it. Only path parameters and `@query` fields may be declared on the request message, because the handler reads the body itself (`r.Body`, already limited by `@Onkt.maxBody` or `WithMaxRequestBodyBytes`).

Clients return the raw response: the Go client returns `*http.Response` (and takes `body io.Reader, contentType string` for PUT, POST and PATCH), and the TypeScript client returns a `Response` (with `{ body, contentType }` in the options for routes with a body), so headers such as `ETag`, `res.blob()` and streaming all work. OpenAPI documents the route with a binary body and the declared content type.

`@Onkt.http` is generated by the `go-server`, `go-client`, `ts-client` and `openapi` targets. The `ts-server`, `python-client`, `dart-client`, `swift-client`, `rust-client` and `rust-server` targets leave raw routes out of their output, so a project can declare them next to typed routes and still generate every target. The message types are still generated everywhere.

#### Saying what the body is

`@Onkt.http` takes optional `request:` and `response:` arguments after the content type, so the clients know what to emit:

```typespec
model DraftRequest {
  slug: string;
  @body draft: DraftBody;
}

@route("/apps")
interface Apps {
  @post @route("/{slug}/bundle-draft.js")
  @Onkt.http(#{ contentType: "application/javascript", request: "json" })
  draft(...DraftRequest): Nothing;

  @get @route("/{slug}/local-execution") @Onkt.http(#{ response: "json" })
  policy(...Slug): Policy;

  @get @route("/{slug}/local-execution/run") @Onkt.http(#{ response: "websocket" })
  run(...Slug): Nothing;

  @get @route("/{slug}/oauth") @Onkt.http(#{ response: "redirect" })
  oauth(...Slug): Nothing;

  @post @route("/{slug}/uploads") @Onkt.http(#{ request: "multipart", response: "json" }) @Onkt.maxBody("64MiB")
  upload(...Slug): UploadResult;

  @Onkt.method("ANY") @route("/{slug}/proxy/{resource}/{+path}") @Onkt.http @Onkt.guard("app/use/:slug")
  proxy(...ProxyRequest): Nothing;
}
```

| `request:` | the server | the TypeScript client | the Go client |
| --- | --- | --- | --- |
| `none` | no body | no body | no body |
| `json` | leaves the body to the handler; the `@body` field is only for the clients | JSON-encodes the `@body` field | marshals the `@body` field |
| `multipart` | leaves the body to the handler | `opts: { body: FormData }`, content type left to `fetch` | `body io.Reader, contentType string` |
| `stream` (the default for a verb with a body) | leaves the body to the handler | `opts: { body?: BodyInit, contentType? }` | `body io.Reader, contentType string` |

| `response:` | the TypeScript client | the Go client |
| --- | --- | --- |
| `stream` (the default) | returns the `Response` | returns `*http.Response` |
| `json` | decodes the declared response message and throws the declared error types | decodes the declared response message |
| `redirect`, `websocket` | a URL builder only | a URL builder only |

Every other kind needs an empty response message; `response: "json"` takes the message the handler writes. `@Onkt.method("ANY")` makes a route answer every HTTP method: the Go server registers the pattern without a method (`RequestMetadata.HTTPMethod` is `"ANY"`), and the clients take the method as an argument (`method string` in Go, `opts.method` in TypeScript, default `GET`).

Every raw route also gets a URL builder in the TypeScript client, for the callers that need a string, such as a `<script src>`, a manifest link or an OAuth start. `appsBundleUrl("https://api.example.com", { slug })` builds it with the path parameters and `@query` fields encoded (and rejects `.` and `..` segments in a wildcard tail), and `client.bundleUrl({ slug })` does the same with the client's base URL. The Go client has the same for every raw route, including `redirect` and `websocket` routes that have no fetch method: `url, err := client.BundleURL(&api.BundleRequest{Slug: slug})`.

#### Wildcard tails, WebSockets and raw handlers

A `{+path}` tail is kept in the Go mux pattern, in `RequestMetadata.Route` and in the TypeScript URL. `net/http` answers a request path with `//` or `.` and `..` segments, such as `/proxy/pg/a//b/../c`, with a redirect to the cleaned path before any handler runs. To pass those paths to a handler as sent, wrap the mux: `http.ListenAndServe(addr, api.PreserveSegments(mux))`. Only requests that match a route with a wildcard tail are affected; every other route keeps the mux's cleaning and redirect. Routes with a wildcard tail then see the original `r.URL.Path`, and `r.PathValue` and the request message hold the tail unchanged. `PreserveSegments` is opt-in, and a tail can now contain `..` and empty segments, so a handler that maps it onto a filesystem or an upstream URL must treat it as untrusted. Until a route's handler runs, middleware and the authorizer see each such segment with a `\x00` prefix, so a path-based check never matches an unclean path by accident.

The `http.ResponseWriter` a raw handler receives still implements `http.Flusher` and `http.Hijacker`, so a WebSocket upgrade (`websocket.Accept(w, r, ...)`) and streamed responses work, and `ResponseHeader`, `SetCookie`, `Redirect`, `NotModified` and `SetResponseStatus` keep working through the request's context. A raw route never buffers a request or response body.

### Cookies, redirects, 204 and 304

A handler adjusts the response through its context:

- `api.ResponseHeader(ctx).Set("ETag", ...)` and `Cache-Control` set headers.
- `api.SetCookie(ctx, &http.Cookie{...})` adds a `Set-Cookie` header (invalid cookies are dropped).
- `api.Redirect(ctx, url, http.StatusFound)` answers with a `Location` header and no body.
- `api.NotModified(ctx)` answers `304` with no body, while the `200` case still uses the generated JSON encoding.
- `api.SetResponseStatus(ctx, status)` chooses any 2xx or 3xx status; `204` and `304` send no body.

Returning `{ @statusCode _: 204; @body body: T }` makes `204 No Content` the default status of the method (`201` works too), and OpenAPI documents it as such. `onkt compat` reports a change.

### Hand-written routes with the same pipeline

`api.Mount(mux, "/apps/{slug}/proxy/{resource}/{+path}", handler, api.RequestMetadata{Service: "Proxy", Method: "Forward", Route: "/apps/{slug}/proxy/{resource}/{+path}", Meta: map[string]string{"audit.event": "proxy"}, Guards: []string{"apps/use/:slug"}}, opts...)` registers any `http.Handler` with the options the generated routes get: middleware, `WithAuthorizer` and `WithScopes`, the request id and the request observer. Use it for routes the schema cannot describe, such as one that forwards every HTTP method. Pass the same `ServerOption` values you give `Register...Server`.

A `RequestObserver` sees what an audit log needs: `RequestResult` carries the status (500 when the handler panicked, with the panic in `Panic`), `Bytes`, `PathValues` and the `Request`. `RequestStarted` can call `httpkit.EnsureState(ctx, api.HTTPRequestFromContext(ctx))` so the request id, the principal (`httpkit.SetPrincipal`) and per-request flags (`httpkit.SetFlag(ctx, "skip-audit")`) are shared with the handlers and readable in `RequestFinished`.

`httpkit.Health(checks...)`, `httpkit.SPA(fsys, opts)` (client-side routes get `index.html`, a missing file with an extension is a 404, per-request headers through `OnServe`) and `httpkit.ETag` / `httpkit.IfNoneMatch` cover the usual non-schema routes.

### Shaping error responses

By default every error the generated servers produce themselves, a malformed body, a bad path or query parameter, a missing header, a failed validation or `@Onkt.authorize` rule, or a handler error with no declared body, is `{"message": "..."}` (with `"violations": [...]` when there are several). Errors a method declares with `@statusCode` keep their declared body. An error message in a method's union that has no `@statusCode` is sent with status `500`, so give every error you expect a `@statusCode`. To send a different shape, install one error writer per server:

| Target | How |
| --- | --- |
| Go | `WithErrorWriter(func(w, r, e *ServerError))`; wrap the mux in `ErrorHandler(mux, opts...)` to cover the router's own plain-text 404 and 405 too |
| TypeScript | `{ onError: (error, req) => Response }` on `createXFetchHandler`, `createXNodeHandler` and `attachXNodeHandlers` |
| Rust | `with_error_writer(router, Arc::new(\|info, headers\| ...))` |

The writer receives the status, a stable `code`, the default `message`, the `field` at fault (a path parameter, query parameter, header or JSON body path) and the `violations`. The codes are `invalid_request_body`, `request_body_too_large`, `invalid_path_parameter`, `invalid_query_parameter`, `missing_header`, `invalid_header`, `invalid_credentials`, `validation_failed`, `unauthorized`, `forbidden`, `not_found`, `method_not_allowed`, `bad_request` (TypeScript Node adapter only) and `internal`; a handler error uses the snake_case name of its status, or its own `PublicCode()`. The underlying error is available as `Cause` (Go) and is never sent by the default writer. In Go the request ID is `RequestIDFromContext(r.Context())`. A Go body that fails to decode keeps the `invalid_request_body` code, and its message names the JSON path and the problem, such as `type: "mysql" is not one of rest, s3, sql` or `options.sql.port: expected integer, got string`, with the path in `field`. Malformed JSON reports `invalid JSON: ` and the parser's reason.

```go
mux := http.NewServeMux()
api.RegisterThingsServer(mux, impl{}, api.WithRequestID("X-Request-ID"), api.WithErrorWriter(envelope))
http.ListenAndServe(addr, api.ErrorHandler(mux, api.WithRequestID("X-Request-ID"), api.WithErrorWriter(envelope)))
```

Errors that happen after a stream has started cannot change the HTTP status, so they travel as an `event: error` frame. That frame goes through the same writer: the body is whatever your error writer produces for the handler's error, so the code and message a handler exposes (`PublicCode` and `PublicMessage` in Go, `HttpError` in TypeScript) reach the client in your envelope. Errors a method declares with `@statusCode` keep their declared body, and an unexpected error still shows only the generic message. `error` stays a reserved event name, so a oneof variant of your own should use another name.

Parameter errors no longer include the Go parser's text: a non-numeric `{id}` is `invalid path parameter id: must be an integer`.

A message may have a field called `error` (the Go field is `Error_` on an error message, with the same JSON name), so an envelope such as `{"error": {"code", "message", "request_id"}}` can also be declared as a typed error with `@statusCode`.

The generated TypeScript client throws `ApiError` for any undeclared status. Besides `statusCode` and the raw `body` it now has the parsed `json`, `message`, `code` and `requestId`. The default parser reads `{message}`, `{code, message, request_id}` and the nested `{error: {...}}` envelope and falls back to the `X-Request-ID` header; pass `errorParser` in the client options for any other shape.

### The JSON content type

Generated Go servers answer JSON with `Content-Type: application/json`. To match an existing API that sends a charset, set it on the target:

```toml
[generate.go-server]
out = "./api"
json_content_type = "application/json; charset=utf-8"
```

It applies to every JSON response the server writes, errors included, and to the shared runtime when `runtime = "shared"`. The value must be a JSON media type (`application/json` or a `+json` type).

### 64-bit integers on the wire

A TypeSpec `int64` or `uint64` crosses the wire as a JSON number, as TypeSpec intends, and so does every item of an `int64[]`. Write `@encode(string) id: int64` to carry one as a JSON string instead, so JavaScript and other double-based parsers never lose precision on values beyond 2^53; on an array, `@Onkt.encode("string")` does the same. Map values are always numbers, and a oneof variant whose payload is a 64-bit integer keeps the string form.

A project migrated from `.onk` may set `int64_encoding = "number"` in `onkt.toml`, which sends every 64-bit integer as a JSON number, oneof payloads included:

```toml
int64_encoding = "number"   # "string" is the default
```

The schema must then not say otherwise: `@encode(string)` (or `@Onkt.encode("string")`) on a 64-bit integer is an error under that setting, so a `.tsp` never claims a string the wire does not carry. `onkt migrate` writes plain `int64` fields for such a project.

Changing a field's encoding on an existing API is a wire change, so `onkt compat` reports every affected field.

### Repeated messages as values in Go

By default a repeated message field is `[]*T` in Go. For large lists that costs an allocation per item and forces conversion helpers, so one setting changes the Go types to `[]T`:

```toml
repeated_style = "values"
```

Validation, JSON, the getters and `@Onkt.rule` expressions all follow, and the other targets are unchanged. Items are not pointers, so a nil item cannot occur. A message reachable from a `@Onkt.ws` method keeps `[]*T`, since the WebSocket frame codec works with pointers. The default is `"pointers"`.

### JSON objects with `@Onkt.object`

A plain `json` field holds any JSON value. Add `@Onkt.object` when it must be an object, as with a free-form settings blob: TypeScript types it `Record<string, unknown>` and validates it, Go keeps `json.RawMessage` (the bytes pass through untouched, so keys are never reordered, which a `map[string, json]` would not guarantee) and rejects arrays and scalars in `Validate()`, and OpenAPI declares `type: object`. `null` and an absent field stay allowed. The Python, Dart, Swift and Rust generators treat the field as any `json` value.

### Rejecting non-positive ids

Put `@Onkt.range(1, 9223372036854775807)` (or `@minValue(1)`) on an `int64` id field to reject zero and negative ids in the generated Go, TypeScript and Rust validators (use `4294967295` for a `uint32`). That is the one place the bound is spelled, and `onkt compat` reports it if it changes.

### Request fields on GET and DELETE

A GET or DELETE has no body, so a request field reaches the server only through the route (`{id}`) or the query string. Scalar fields (string, bool, integer, float) that are not in the route are bound to the query string automatically, as if they carried `@query`. A field that cannot be sent that way (a message, an enum or a repeated field) is a build error that names the field and the RPC, instead of arriving as a zero value. A request message that a body-carrying RPC also uses (a resource message shared by `get` and `update`) is left alone, since its other fields are meant for the body.

### Member names that are keywords

A field called `in`, `from`, `default`, `type` or `class` keeps that exact name on the wire and in Go and TypeScript (`In`, `from`). Names that a target language cannot use are rejected only when that target is configured: with `[generate.python-client]` present, `in` and `from` fail the build, because Python cannot use them as attribute names; a project that generates only Go and TypeScript can use them freely. Rust, Dart and Swift generate escaped names (`r#in`, `in_`). A project that lists no targets keeps every rule.

### Query parameters on POST, PUT and PATCH

`@query` also works on routes with a body. The field is sent in the query string, the rest of the message is the body (or the single `@body` field), and the generated Go and TypeScript servers, clients and OpenAPI follow. The python, dart, swift and rust targets do not support it yet, so a project that generates them is rejected with a message that says so.

### Integer map keys

A map key is a `string`, `int32`, `int64`, `uint32` or `uint64`: `map[int64, string]`. JSON object keys are strings on the wire, so the wire form is the same for every key type. Go generates `map[int64]string` and decoding rejects a key that is not an integer, Rust generates `HashMap<i64, String>`, and TypeScript keeps `Record<string, V>` and fails validation for a key that is not an integer (an unsigned key may not have a sign). Python, Dart and Swift keep string keys.

### Pinning the onkt version

Set `version` in `onkt.toml` and every machine builds with that release, with no Go toolchain and nothing to install beyond a first `onkt`:

```toml
version = "0.26.0"
module = "example.com/api"
```

When `onkt build`, `check`, `generate`, `watch` or `mock` runs with a different version, it downloads the pinned release from GitHub, checks its SHA-256 against the release's `checksums.txt`, keeps it in the cache directory (`ONKT_CACHE_DIR`, default the user cache dir) and hands the command over to it. The download happens once per version. Set `ONKT_NO_PIN=1` to skip the pin, or `ONKT_DOWNLOAD_BASE` to fetch from a mirror. A project can pin a release from before the rename to onkt; it is fetched and run the same way. A development build ignores the pin. Pin a release that already knows the `version` key (0.26.0 or newer), since older releases reject unknown keys.

### Writing zero values

By default the Go and TypeScript targets leave a field out of the JSON when it holds its zero value (`""`, `0`, `false`, an empty list), because on the wire an absent field and a zero field read the same. If consumers expect every field to be present, say so once for the project:

```toml
module = "example.com/api"
emit_zero_values = true
```

Every non-optional scalar, enum, `bytes`, repeated and map field is then always written, in every target: `""`, `0`, `false`, the enum's first name (or `0` for a number-encoded enum), `[]` and `{}`. A nil list or map in Go is written as `[]` or `{}`, never `null`. Optional (`?`) fields keep their meaning, absent when unset, and a singular message field is still omitted when it is not set. Timestamps and `json` values are left as they were, because a zero timestamp has no single form that every language agrees on.

Messages that need this (and other wire adjustments such as 64-bit strings) are encoded by a generated `AppendJSON` method, described below. The generated types import `encoding/json/jsontext` for it, which needs Go 1.27 or newer.

This applies to requests as well as responses, since both use the same types, so a message used as a partial update should declare its patchable fields optional (`name: string?`) to say "not provided". `onkt compat` reports the change.

### How generated Go messages are encoded and decoded

A message with a wire adjustment (64-bit strings, nullable fields, number enums, `@encode` on timestamps or bytes, a oneof, or `emit_zero_values`), and every message that holds one, gets `AppendJSON(b []byte) ([]byte, error)` and `DecodeJSON(data []byte) error`. So does every request, response and error message of an HTTP or SSE method, and every message those reach. `AppendJSON` writes the fields straight into one buffer: no reflection, no helper struct, and no second scan of what a nested message wrote. `DecodeJSON` reads the document in one pass, straight into the fields, with no `map[string]json.RawMessage` and no helper struct.

`MarshalJSON`, `MarshalJSONTo` and `UnmarshalJSON` call them for the messages that already had custom JSON, so `json.Marshal`, `json.Unmarshal` and the `jsonv2` package use them with no change to your code. A plain message that only sits inside such a message, or is only a request or response, keeps `encoding/json`'s own handling when you use it alone, and gets the fast methods when the generated server, client, SSE and WebSocket code move it. Call them yourself to skip `encoding/json`'s validation pass over a `Marshaler`'s output and a second scan of what an `Unmarshaler` is given: `buf, err := resp.AppendJSON(buf[:0])`, `err := req.DecodeJSON(body)`.

The generated server writes each response through `AppendJSON` into a pooled buffer and reads each request body into a pooled buffer for `DecodeJSON`. The generated client does the same with request and response bodies, and SSE events take the same route.

`DecodeJSON` handles ordinary documents on its fast path. Anything unusual (a key that differs only in case, a repeated key, a string that is not valid UTF-8, a number that does not fit, a value of the wrong type, malformed JSON) is handed to `encoding/json` from the start, so the result and the error are the ones `encoding/json` gives. The generated tests check this against the earlier decoder on random values, `null` in every field, mutated bytes and pretty-printed input.

Ten resources with nested messages, maps, a nullable field and a `json` field, on Go 1.27.1 (`go test ./internal/gengo -run TestAppendEncoderBenchmark -v`; best of several runs):

| | time | allocations |
| --- | --- | --- |
| encode: hand-written struct, `json.Marshal` | 21.3 µs | 61 |
| encode: generated before, `json.Marshal` | 23.8 µs | 103 |
| encode: generated now, `json.Marshal` | 17.1 µs | 11 |
| encode: generated now, `AppendJSON` into a reused buffer | 7.2 µs | 10 |
| decode: hand-written struct, `json.Unmarshal` | 53 µs | 226 |
| decode: generated before, `json.Unmarshal` | 82 µs | 236 |
| decode: generated now, `json.Unmarshal` | 42 µs | 217 |
| decode: generated now, `DecodeJSON` | 24 µs | 217 |

The output is the same JSON, with two differences. Keys come out in declaration order; before, fields with a wire adjustment came after the others. A `json` field is validated and written as stored, not compacted, and a field holding invalid JSON fails the encode. Reading a request body now rejects data after the first JSON value, where it used to be ignored. A message that uses `@Onkt.flatten` keeps the earlier encoder and decoder.

### Client speed notes

**Python keep-alive.** The generated Python client opens a connection for every call, because `urllib` does. `BookingsClient(url, keep_alive=True)` reuses one `http.client` connection per thread instead, which made a small JSON call 3.7x faster against a local server and saves the TCP and TLS handshake on a real network. Use it as a context manager or call `close()`. A request that finds its kept connection already closed by the server is sent again on a new one. The kept-alive path does not follow redirects (a 3xx raises `UnexpectedStatusError`) and does not read the `HTTP_PROXY` environment variables, which is why it is off by default. Streaming (SSE) calls always use `urllib`.

**Server-sent events.** The Go server builds each event in one reused buffer and writes it once (about half the allocations and a fifth of the bytes per event), and the Go client reads events without copying lines or joining strings. The TypeScript client no longer allocates a `TextEncoder` result for every line it checks against `maxSSELineBytes` (1.7x faster on a 5000-event stream). A `json` field that contains line breaks used to split an event in two when it was sent; the server now writes it as several `data:` lines, which every client joins back together.

### Declaring what a method requires

`@Onkt.requires("users:read", "users:write")` on an RPC declares the scopes (or
roles) a caller must hold; every listed scope is required. Onekit does not
decide where scopes come from, it makes the requirement part of the contract
and hands it to your authentication code in each target:

```typespec
@get @route("/users/{id}") @Onkt.requires("users:read") get(...GetUser): User;
```

| Target | What you get |
| --- | --- |
| Go server | `RequestMetadata.Scopes` in your `Authorizer`, plus `WithScopes(func(ctx, r) ([]string, error))`, which returns `403` with `missing required scope: ...` (a `*ScopeError`, through `WithErrorWriter` with `Code` `"forbidden"`) and leaves authentication failures as `401`. The callback runs only for routes that declare `@Onkt.requires` (`RequestMetadataFromContext(ctx).Scopes` lists what the route needs), after middleware and before the request is decoded. It composes with `WithAuthorizer`: the scope check runs first and the `Authorizer` only when it passes |
| TypeScript server | `scopes` on each `RouteDescriptor`, and `{ authorize: requireScopes((req) => grantedScopes) }` on `createXFetchHandler`, `createXNodeHandler` and `attachXNodeHandlers` (or your own `authorize(req, route)`) |
| Rust server | `context.required_scopes` and `context.missing_scopes(&granted)` on the `RequestContext` passed to every handler |
| OpenAPI | the scopes are listed under each auth scheme in the operation's `security` requirement (needs an `@auth` header on the service or method) |
| `onkt compat` | adding, removing or changing required scopes is reported as a contract change |
| `onkt import` | scopes in an OpenAPI operation's `security` become `@Onkt.requires(...)` |

`@Onkt.requires` is not supported on `@Onkt.ws` methods yet (the compiler says so rather
than generating a server that silently skips the check); authorize the
WebSocket upgrade request in middleware for now. The generated Go, TypeScript
and Rust clients do not check scopes, since a client does not know what its
server will grant.

### Authorizing callers with `@Onkt.authorize`

`@Onkt.requires` names scopes and leaves the decision to you. `@Onkt.authorize(expression, message)` states the decision itself, in the same expression language as `@Onkt.rule`, and the generated server enforces it:

```typespec
@Onkt.principal
model Principal {
  user_id: string;
  roles: string[];
  org: string;
}

interface Docs {
  @post @route("/docs/delete")
  @Onkt.authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")
  delete(...DeleteDoc): Ack;
}
```

`@Onkt.principal` marks the one message that describes the authenticated caller (a project may have only one). In an `@Onkt.authorize` expression `auth` is that message and `req` is the decoded request, so a rule can compare who is calling with what they are asking for. Both are type-checked when the schema compiles, with the same line and column diagnostics as `@Onkt.rule`. The decorator is repeatable, and every rule must hold.

The order on the server is: authenticate (`401` when no caller can be identified), decode and validate the request (`400`), then evaluate the rules (`403` with the first failed message in `message` and all of them in `violations`). A rule that cannot be evaluated counts as failed, so it can never silently allow a call. A method without `@Onkt.authorize` never asks who the caller is, and a server that has an `@Onkt.authorize` method but no way to identify callers answers `500` instead of letting the call through.

| Target | How callers are identified |
| --- | --- |
| Go server | `WithPrincipal(func(ctx, r) (*Principal, error))`; return a nil principal for a caller the credentials do not identify, which is a `401`, so a rule that never reads `auth` cannot let an unidentified caller through. An error that carries `HTTPStatusCode()` answers that status, and any other error is a server fault: a `500` with the error as `ServerError.Cause`, so an unreachable session store does not look like a signed-out caller. Handlers read the caller with `PrincipalFromContext(ctx)` |
| TypeScript server | the `principal` option of `createXFetchHandler`, `createXNodeHandler` and `attachXNodeHandlers`; return `undefined` or `null` for a caller the credentials do not identify (a `401`), throw an `HttpError` to choose the status, and any other thrown error is a `500` with the error as `ServerErrorInfo.cause`. Handlers read `context.principal` |
| Rust server | insert the `Principal` as a request extension from your authentication layer (`request.extensions_mut().insert(principal)`); a request without one is a `401`, so when the lookup itself fails the layer should answer a `5xx` rather than skip the extension. Handlers read `context.principal` |

`@Onkt.authorize` is enforced by Go, TypeScript and Rust servers, and is not supported on `@Onkt.ws` methods yet (the compiler says so). `onkt compat` reports a changed, added or removed authorization rule, and an `@Onkt.authorize` rule on a method is part of the contract the same way a required scope is.

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

`onkt check` performs semantic validation as well as parsing: unsupported or
misplaced decorators, invalid validator values, generated-name collisions,
malformed bindings, duplicate routes or headers, invalid error statuses, and
incompatible header/auth contracts are rejected before generation. Add
`--json` for editor/CI diagnostics with path, line, column, code, and message
fields. `onkt compat` compares nested types, fields, enums, oneofs, validators,
routes, bindings, headers, streams, and typed errors, including configured
route prefixes; `onkt compat --json` emits stable machine-readable findings.
Accept an intentional break with `--allow app.User.email` (repeatable; a
message or service name also covers its fields and routes).
Successful builds write an ignored `.onekit/manifest.json` containing the
schema fingerprint and expected generated outputs.

### Catching breaking schema changes in pull requests

The repository ships a GitHub Action that runs `onkt compat` against the pull
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
branch), `version` (onkt release to install, default `latest`; the download is
verified against the published checksums), `allow` (newline-separated paths to
accept), `comment` and `fail-on-breaking` (both default `true`). The action
runs on Linux and macOS runners, and exposes a `breaking` output with the
number of findings. PRs from forks cannot comment with the default token; the
job summary and the failing status still report the result.

Install the CLI from npm (see [Quick start](#quick-start)), or with Go:

```bash
go install github.com/1homsi/onekit/cmd/onkt@latest
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

With `"wire"`, `is_default` stays `is_default` in the types, requests, responses, validators and `@Onkt.rule` checks, and encoding and decoding become the identity for keys. Oneof variant payload keys follow the same rule. Set it on both targets if you generate both, so a client and a server agree on property names.

### Required fields in TypeScript responses

A TypeScript field is optional (`id?: number | undefined`) unless it is `@Onkt.required`, because a server may omit a field that holds its zero value. With `emit_zero_values = true` every non-optional scalar, enum, repeated and map field is always sent, so the types say so: in a message that some method returns (a response, a stream event or a declared error, directly or nested) and that no method accepts as a request, those fields are required (`id: number`, `is_default: boolean`). Timestamps and singular message fields are required there too: the Go server always writes a timestamp, and writes a message field that the handler left unset as an empty message. A message field whose type can lead back to itself (a tree node's `parent`) is not given this treatment and stays optional, as do `?` fields and `json`. Request messages keep optional fields so callers can send partial objects, and a message used as both a request and a response stays optional. Without `emit_zero_values` nothing changes.

Generated TypeScript compiles under `"strict": true` with `noUnusedLocals`, `noUnusedParameters`, `noUncheckedIndexedAccess` and `exactOptionalPropertyTypes`: imports, private helpers and parameters that a given schema never uses are not emitted, so the output can sit inside a project that type-checks it.

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

Each client method takes one request object holding every field, path parameters included, plus an optional `RequestOptions` (`signal`, `headers`, `timeoutMs`): `client.update({ id, name })`, not `update(id, input)`. A failed call throws `ApiError` carrying `statusCode`, the raw `body`, and, when the response follows the error envelope, `code`, `message` and `requestId`; set `errorParser` in the client options to read a custom envelope. Requests are checked against the schema before they are sent and an invalid one throws `RequestValidationError`; set `validateRequests: false` in the client options to send them anyway, so the server reports every violation in its own error envelope (WebSocket frames are still checked). Calls time out after 30 seconds and read at most 8 MiB; both are settable per client (`timeoutMs`, `maxResponseBodyBytes`) and `timeoutMs` per call.

`defaultHeaders` is a record or a function (sync or async) that runs before every request, so a token that changes is read fresh; headers passed per call win over it. `onResponse(res, { method, url })` sees every response before the body is read, and `onUnauthorized(res, { method, url })` runs on a 401 before the `ApiError` is thrown, which is where an app redirects to its sign-in page. A custom `fetch` is only needed for transport changes now.

The client builds every URL as `baseUrl` plus the route path, and the route path already includes the service's base path. So `baseUrl` is the origin the server answers on (`https://api.example.com`, or an empty string for same-origin calls), never the origin plus a prefix. If a service has `@route("/api/environments")` and the app is configured with `VITE_API_BASE_URL=/api`, pass `""`, or move the prefix into the interface's `@route`; passing `/api` would call `/api/api/environments`.

## Validation rules

`@Onkt.rule(expression, message)` attaches a rule written in a small expression language to a message or a field. The compiler type-checks every rule against the schema, so a typo or a type mismatch is a compile error with the line of the decorator and the column inside the expression. Rules are repeatable.

```typespec
@Onkt.rule("self.nights * self.rooms <= 60", "at most 60 room-nights per booking")
@Onkt.rule("size(self.guests) <= self.rooms * 4", "at most four guests per room")
model Booking {
  @Onkt.rule("value.matches('[0-9]{4}-[0-9]{2}-[0-9]{2}')", "dates look like 2026-01-31")
  checkin: string;
  @Onkt.rule("value >= 1", "book at least one night") nights: int32;
  @Onkt.rule("value >= 1", "book at least one room") rooms: int32;
  guests: string[];
  @Onkt.rule("!has(self.coupon) || size(value) == 8", "coupons have eight characters")
  coupon?: string;
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
- `matches` must match the whole string and its pattern must be a string literal in a portable regular-expression subset: literals, bracket classes, `|`, `(...)`, `(?:...)`, `* + ?` and `{n,m}`. Anchors, `.`, shorthand classes such as `\d`, lazy quantifiers, lookaround, repetition of a group that contains `|` or another repetition, `\-` outside a bracket class, and a doubled `&&`, `||` or `~~` inside one are rejected, because the supported targets disagree about them. To write a backslash in a pattern inside a TypeSpec string, double it twice: `'[0-9]+(\\\\.[0-9]+)?'`.
- An expression is at most 1024 bytes and 48 levels deep; the message is at most 200 characters.

`a ? b : c` produces a `bool`, `int`, `double`, `string` or enum, never a list, map or message.

Changing a rule counts as a breaking change in `onkt compat`.

### Where rules are enforced

| Target | Status |
| --- | --- |
| Go (`go-server`, `go-client`) | Enforced by the generated `Validate()`. Servers answer `400` with the failed rule messages in `violations`, and clients refuse to send an invalid request. |
| TypeScript (`ts-client`, `ts-server`) | Enforced by the generated `validate<Message>()`, which returns the failed rule messages. Clients throw `RequestValidationError` and servers answer `400`. Integers are evaluated as `bigint`, so the target must be ES2020 or newer, as the generated server already requires. |
| Python (`python-client`) | Enforced by the generated `validate()`, which raises `ValueError` with the failed rule messages joined by `"; "`; the generated client validates requests before sending. |
| Rust (`rust-client`, `rust-server`) | Enforced by the generated `validate()`, which returns the first failed rule as a `ValidationError` (the field's name for a field rule, empty for a message rule). `rule_violations()` returns every failed rule. A rule that uses `matches()` needs the `regex` crate, like `@pattern`. |
| Dart (`dart-client`) | Enforced by the generated `validate()`, which returns the failed rule messages along with its other violations. Integers use Dart's native 64-bit `int`, so rules need the Dart VM or Flutter on a device; Dart compiled to JavaScript has no 64-bit integers. |
| Swift (`swift-client`) | Enforced by the generated `validate()`, which returns the failed rule messages along with its other violations. Strings are compared by Unicode scalar rather than by Swift's canonical equivalence, so `"\u{e9}"` and `"e\u{301}"` differ here exactly as they do everywhere else. A rule that reads a `#deprecated` field of a message declared in a different namespace sees that field as unset, because the field's storage is private to its own file. |

Every target is held to the same conformance suite (`internal/onkexpr/conformance`): about a hundred rules run over the same inputs and must fail exactly the rules the reference evaluator fails. Go, TypeScript and Python compile each rule to native expressions; Rust, Dart and Swift embed a small evaluator that is a port of the reference one.

## Repository layout

| Path | Contents |
| --- | --- |
| `cmd/onkt/` | CLI entrypoint |
| `typespec/` | The `@onkt/typespec` npm package: the `Onkt` decorators and the emitter that writes onekit's schema document |
| `internal/onkschema/` | The versioned schema document every frontend writes and the compiler reads |
| `internal/onkcompile/` | Checks the declarations and compiles them into the IR, resolving cross-file type references |
| `internal/onkir/` | The native intermediate representation every generator consumes |
| `internal/onek/` | `onkt.toml` parsing and the `build`/`check`/`migrate` orchestration |
| `internal/onkmigrate/`, `internal/onklang/` | `onkt migrate`: the `.onk` parser and its conversion to TypeSpec |
| `internal/gengo/`, `internal/gents/`, `internal/genpy/`, `internal/gendart/`, `internal/genswift/`, `internal/genrust/`, `internal/genopenapi/` | Generator backends |
| `examples/simple-api/` | A complete, working example with committed generated output |

## Status

onekit is a young project and pre-1.0. It supports messages (scalars including arbitrary `json`, repeated, optional, maps, nested types), enums, discriminated oneofs, field validation (formats, patterns, lengths, ranges, allowed values on strings and integers, `@Onkt.required`, item counts), HTTP path/query/body binding, typed headers and error unions, SSE clients in Go, TypeScript, Python, Dart, and Rust, and Go/TypeScript/Python/Dart/Rust/OpenAPI generators.

JSON mapping is supported through `@Onkt.flatten`, root-level `@Onkt.unwrap`, and `@encode(...)` for safe integer, enum, timestamp, and byte representations. Map-value messages must not use `@Onkt.unwrap`; `onkt check` rejects that shape consistently instead of allowing generators to diverge. Generated clients validate requests before sending, generated servers validate decoded requests, and nested validation is emitted consistently across targets. Generated Go servers also provide functional registration options for mux selection, middleware, request IDs, authorization, route metadata, and lifecycle observation.

## AI agents and editors

TypeSpec's own language server gives editors and agents completion,
navigation and diagnostics for `.tsp` files; install the TypeSpec extension
for VS Code, or point any LSP client at `tsp-server` from
`@typespec/compiler`. `onkt check --json` reports compile errors from both
TypeSpec and onekit with path, line and column, which is what an agent needs
to fix a schema.

## License

onekit is released under the [MIT License](LICENSE).
