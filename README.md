# onekit

onekit is a protobuf-first HTTP toolkit.

Define your API once in `.proto` files, annotate the HTTP surface, and generate the boring pieces around it: Go handlers, Go clients, TypeScript clients, TypeScript server routes, Python clients, mock handlers, and OpenAPI documents.

The goal is simple: keep transport code, validation, clients, and docs in sync without making every service hand-roll the same glue.

## What it generates

| Plugin | Purpose |
| --- | --- |
| `protoc-gen-onekit-go-http` | Go HTTP server bindings, request parsing, response writing, validation, and mocks |
| `protoc-gen-onekit-go-client` | Go clients with typed requests, typed responses, headers, and call options |
| `protoc-gen-onekit-ts-client` | TypeScript clients for browser, Node, Deno, Bun, and Workers-style runtimes |
| `protoc-gen-onekit-ts-server` | TypeScript route adapters built around the Web Fetch API |
| `protoc-gen-onekit-py-client` | Python clients with typed models and typed proto error exceptions |
| `protoc-gen-onekit-openapiv3` | OpenAPI 3.1 files from the same protobuf contracts |

## Why use it

- API contracts live in protobuf instead of being copied between backend, frontend, and docs.
- HTTP routes, path params, query params, headers, request bodies, and responses are generated from annotations.
- Validation rules from `buf.validate` are enforced at the HTTP boundary.
- Generated clients know about service-level and method-level headers.
- OpenAPI output includes routes, schemas, examples, headers, and validation metadata.
- Local mock servers can be generated from the same service definitions.

## Quick start

Clone the repo and build the plugins:

```bash
git clone https://github.com/corezio/onekit.git
cd onekit
make build
```

Run the smallest example:

```bash
cd examples/simple-api
make demo
```

The example builds the local generators into `../../bin`, runs `buf generate`, and starts a Go HTTP server from generated code.

`bin/` is intentionally local build output and is ignored by git.

## Install the plugins

For use outside this repository:

```bash
go install github.com/corezio/onekit/cmd/protoc-gen-onekit-go-http@latest
go install github.com/corezio/onekit/cmd/protoc-gen-onekit-go-client@latest
go install github.com/corezio/onekit/cmd/protoc-gen-onekit-ts-client@latest
go install github.com/corezio/onekit/cmd/protoc-gen-onekit-ts-server@latest
go install github.com/corezio/onekit/cmd/protoc-gen-onekit-py-client@latest
go install github.com/corezio/onekit/cmd/protoc-gen-onekit-openapiv3@latest
```

## A small contract

```proto
syntax = "proto3";

package example.users.v1;

import "buf/validate/validate.proto";
import "onekit/http/annotations.proto";
import "onekit/http/headers.proto";

service UserService {
  option (onekit.http.service_headers) = {
    required_headers: [
      {
        name: "X-API-Key"
        type: "string"
        format: "uuid"
        required: true
      }
    ]
  };

  rpc CreateUser(CreateUserRequest) returns (User) {
    option (onekit.http.config) = {
      path: "/v1/users"
      method: HTTP_METHOD_POST
    };
  }
}

message CreateUserRequest {
  string name = 1 [(buf.validate.field).string = {
    min_len: 2
    max_len: 100
  }];

  string email = 2 [(buf.validate.field).string.email = true];
}

message User {
  string id = 1;
  string name = 2;
  string email = 3;
}
```

From that file, onekit can produce:

```go
// Server registration.
api.RegisterUserServiceServer(userService, api.WithMux(mux))

// Typed Go client.
client := api.NewUserServiceClient(
    "https://api.example.com",
    api.WithUserServiceAPIKey(apiKey),
)

user, err := client.CreateUser(ctx, &api.CreateUserRequest{
    Name:  "Moe",
    Email: "moe@example.com",
})
```

```ts
const client = new UserServiceClient("https://api.example.com", {
  apiKey,
});

const user = await client.createUser({
  name: "Moe",
  email: "moe@example.com",
});
```

## Local development

Common targets:

```bash
make build       # build all generator binaries into ./bin
make proto       # regenerate onekit annotation Go files
make test-fast   # run tests without coverage
make test        # run the full test script
make clean       # remove ./bin
```

Some examples reference plugins through `../../bin/protoc-gen-onekit-*`. Run `make build` at the repository root before generating those examples.

## Repository layout

| Path | Contents |
| --- | --- |
| `cmd/` | protoc plugin entrypoints |
| `internal/` | generator implementations and shared compiler helpers |
| `proto/onekit/http/` | protobuf annotations exposed to users |
| `http/` | generated Go package for the annotations |
| `examples/` | small services that exercise the generators |
| `scripts/` | test and golden-file helper scripts |

## Examples

Start with:

- `examples/simple-api` for basic Go server generation
- `examples/restful-crud` for path params and REST-style routing
- `examples/validation-showcase` for request validation
- `examples/python-client-demo` for Python client output
- `examples/ts-client-demo` for TypeScript client output
- `examples/ts-fullstack-demo` for TypeScript server and client output
- `examples/sse-streaming` for streaming-style responses

## Protobuf dependencies

onekit is built around standard protobuf tooling and Buf-compatible generation. The annotations live under:

```proto
import "onekit/http/annotations.proto";
import "onekit/http/headers.proto";
import "onekit/http/errors.proto";
```

Validation support comes from:

```proto
import "buf/validate/validate.proto";
```
