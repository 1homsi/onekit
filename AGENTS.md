# Working with OneKit schemas

- Read README.md for .onk syntax and language-tool setup.
- Use the OneKit MCP tools to inspect declarations, imports, documentation, definitions, and type references. In this repository, select `project: "examples/onk-simple-api"` for the example API; select the relevant project directory in a multi-project checkout.
- Tool positions are zero-based lines and UTF-16 columns. Compiler diagnostic line/column values are one-based byte positions.
- MCP reads saved files. After changing a schema, use `onekit_project` or `go run ./cmd/onek check --json --dir <project>` to validate it. Resolve diagnostics before relying on type navigation: reference bindings are withheld when compilation fails.
- Make schema changes in .onk sources; regenerate their outputs with the existing OneKit build workflow.
