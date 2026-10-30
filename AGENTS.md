# Working with onekit schemas

- Schemas are TypeSpec (`.tsp`). Read README.md for how TypeSpec maps onto onekit and for the `@Onkt.*` decorators; `typespec/lib/main.tsp` documents each decorator.
- A project compiles from `main.tsp` at its schema root with the project's own TypeSpec toolchain. In this repository, run `make typespec` once (it installs the npm workspace and builds the `@onkt/typespec` emitter); `examples/simple-api` is the example project.
- After changing a schema, validate it with `go run ./cmd/onkt check --json --dir <project>`. Diagnostics carry one-based lines and columns, from TypeSpec and from onekit's own checks alike.
- Make schema changes in .tsp sources; regenerate their outputs with `go run ./cmd/onkt build --dir <project>` (`make check-generated` for the example).
