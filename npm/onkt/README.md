# onkt

The onkt CLI generates Go, TypeScript, Python, Rust, Dart and Swift servers and
clients, and OpenAPI documents, from a TypeSpec API. Install it next to the
TypeSpec toolchain so a project pins one version of both:

```bash
npm install --save-dev onkt @onkt/typespec @typespec/compiler @typespec/http
npx onkt build
```

Keep `onkt` and `@onkt/typespec` on the same version: they are released
together.

This package is a launcher. On first use it downloads the onkt binary for the
machine (macOS, Linux and Windows, on x64 and arm64; the Linux binary is
static, so Alpine works) from the GitHub release of the same version, checks it
against the release's `checksums.txt`, and caches it under the user cache
directory (`~/.cache/onekit/releases/<version>` on Linux,
`~/Library/Caches/onekit/releases/<version>` on macOS,
`%LocalAppData%\onekit\releases\<version>` on Windows). Later runs start the
cached binary directly. It needs no install script, so it works with
`--ignore-scripts` and pnpm.

- `ONKT_DOWNLOAD_BASE` points the download at a mirror of
  `https://github.com/1homsi/onekit/releases/download`.
- `ONKT_CACHE_DIR` replaces the cache directory.
- Behind a proxy, run Node with `NODE_USE_ENV_PROXY=1` and `HTTPS_PROXY` set.

See the [onekit README](https://github.com/1homsi/onekit#readme) for the
schema, the configuration and every command.
