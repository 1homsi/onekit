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

The package runs a prebuilt binary for macOS, Linux (glibc and musl, so Alpine
works) and Windows, on x64 and arm64, from an optional dependency such as
`@onkt/cli-linux-x64`. npm installs only the one for the machine it runs on,
so do not install with `--omit=optional`.

See the [onekit README](https://github.com/1homsi/onekit#readme) for the
schema, the configuration and every command.
