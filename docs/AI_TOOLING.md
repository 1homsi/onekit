# OneKit language tools for AI agents and editors

OneKit provides two local, read-only interfaces backed by the same parser,
compiler, and source index:

- `onek mcp [--dir DIR]`: MCP tools for Codex, Claude Code, and other MCP clients.
- `onek lsp [--dir DIR]`: a stdio language server for Claude Code and editors.

Neither server generates code or writes schema files. Both honor `schema_root`
and `allow_legacy_contracts` in `onekit.toml`, and refresh from disk on queries.
No model API key or remote service is needed to run these servers.

## From this repository

Run your agent from the repository root. The committed `.codex/config.toml`
and `.mcp.json` launch `go run ./cmd/onek mcp --dir .`. This uses the checked-out
compiler, so an installed `onek` binary is not required. Install the Go version
specified in `go.mod`; the first startup can download/build Go dependencies.

Trust the project in Codex, or approve the project MCP server in Claude Code
when prompted, and restart the agent session after adding the configuration.
Check the client's `/mcp` view to confirm that `onekit` connected.

This repository contains an example schema project. Pass
`project: "examples/onk-simple-api"` to the tools when inspecting that API.
The root of a monorepo is not necessarily one compilable schema project.

## In a repository that uses OneKit

Build/install the `onek` binary from this revision (or a release containing
these commands) and make it available on PATH. Commit the following files in
the consuming project's root. Launch the agent from that root.

`.codex/config.toml`:

```toml
[mcp_servers.onekit]
command = "onek"
args = ["mcp", "--dir", "."]
```

`.mcp.json`:

```json
{
  "mcpServers": {
    "onekit": {
      "type": "stdio",
      "command": "onek",
      "args": ["mcp", "--dir", "."]
    }
  }
}
```

Document in `AGENTS.md` and `CLAUDE.md` that agents should use these tools for
.onk navigation and validate after editing. A checked-in server configuration
still requires the client's trust/setup step; it does not install the binary.

## MCP tools

| Tool | Result |
| --- | --- |
| `onekit_project` | Compiler diagnostics, files, package names, import paths, and declarations |
| `onekit_symbols` | Declarations filtered by qualified-name substring and optional file path |
| `onekit_definition` | Resolved declaration at a source position |
| `onekit_references` | Type references to the declaration at a source position |
| `onekit_hover` | Declaration signature, documentation, and location |
| `onekit_check_source` | Diagnostics for proposed file contents, without saving them |
| `onekit_format` | The `onek fmt` result for a source string |

All tools accept optional `project`, relative to the server's startup root.
It must stay inside that root. File `path` arguments are relative to the
selected project (or absolute paths inside it).

Navigation takes `path`, `line`, and `character`. Lines and UTF-16 columns are
**zero-based**, matching LSP. Returned ranges use the same convention. Compiler
`diagnostics` retain the CLI's **one-based byte** line/column convention.
`onekit_references` accepts `includeDeclaration: true` to include the definition.
For example, first call `onekit_symbols` with `query: "User"`, then pass a
returned declaration's path and `range.start` to `onekit_references`.

References use resolved compiler object identities, covering field types, map
values, oneof variants, and RPC request/response/error types. Same-named types
in different directories remain distinct; import scopes follow the compiler.
Hover describes declarations, their documentation and their decorators, with an
explanation of the WebSocket ones (`@ws`, `@ws_id`, `@ws_cancel`, `@ws_timeout`,
`@raw`) and other common decorators. Typing `@` offers decorator completions.
Primitive scalar hover and call hierarchy are not yet implemented.

MCP reads saved files only. LSP additionally overlays open documents, including
unsaved new .onk files inside the schema tree. It advertises full-document
synchronization and UTF-16 positions, publishes diagnostics on open/change/save/
close, and clears stale diagnostics. It supports definition, references, hover,
decorator completion, document symbols, workspace symbol search, document
formatting, and renaming messages and enums across every file that references
them (qualified references keep their package prefix).

On a syntax/semantic error, declarations from successfully parsed files remain
available, but type-reference bindings are withheld until compilation succeeds.
Syntax diagnostics can cover multiple files; semantic validation currently
reports the first compiler error, just like `onek check`. Analysis is rebuilt
per query; very large schema trees may benefit from future incremental indexing.

## Claude Code native LSP plugin

The local plugin in `editors/claude-onekit` connects `.onk` files to `onek lsp`.
From this repository, install the current binary and launch Claude Code with it:

```sh
go install ./cmd/onek
# Ensure $(go env GOPATH)/bin is on PATH.
claude --plugin-dir ./editors/claude-onekit
```

For a consuming repository, use the absolute plugin directory with
`--plugin-dir`. The plugin is optional when using MCP. Its `.lsp.json` belongs
at the **plugin root**, not at an arbitrary project root.

The LSP uses the client's root URI (or first workspace folder). To select a
schema project within a monorepo, configure its server arguments as
`["lsp", "--dir", "/absolute/path/to/schema-project"]`. Only one schema project
is analyzed by each LSP process; run separate processes for independent projects.

Other LSP clients can launch `onek lsp` over stdio with the same settings.

## References

- [Codex MCP configuration](https://developers.openai.com/codex/mcp)
- [Claude Code project MCP configuration](https://code.claude.com/docs/en/mcp)
- [Claude Code LSP plugins](https://code.claude.com/docs/en/plugins-reference#lsp-servers)
- [MCP stdio transport](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)
