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
../../bin/onek build .   # regenerates api/*.gen.go and docs/openapi.yaml from models.onk + service.onk
```

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
(axum / Web-standard `WebSocketPair`).

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

See [`COMPATIBILITY.md`](COMPATIBILITY.md) for the schema-change policy and
the required pre-release compatibility workflow.

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
