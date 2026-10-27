# @onkt/typespec

Generate Go servers and clients, TypeScript clients and server routes, Python clients, Dart/Flutter and Swift clients, Rust clients and Axum servers, and OpenAPI 3.1 documents from a TypeSpec API, with [onekit](https://github.com/1homsi/onekit).

This package is the TypeSpec side of onekit: the `Onkt` decorators for what TypeSpec has no word for (server-sent events, WebSockets, authorization rules, validation rules, hand-written routes), and the emitter that hands the compiled API to the `onkt` CLI.

## Use it

```bash
npm install --save-dev @typespec/compiler @typespec/http @onkt/typespec
onkt init .        # or write onkt.toml and main.tsp yourself
onkt build
```

`onkt build` runs `tsp compile` with this emitter and generates every target listed in `onkt.toml`. Install the `onkt` CLI from the [onekit releases](https://github.com/1homsi/onekit/releases).

```typespec
import "@typespec/http";
import "@onkt/typespec";

using TypeSpec.Http;

namespace todos;

model Todo {
  id: string;
  @Onkt.required @minLength(1) @maxLength(200) title: string;
}

model GetTodo {
  id: string;
}

@error
model NotFound {
  @statusCode _: 404;
  message: string;
}

@route("/v1")
interface Todos {
  @get @route("/todos/{id}") get(...GetTodo): Todo | NotFound;
  @get @route("/watch") @Onkt.stream watch(...GetTodo): Todo;
}
```

Standard TypeSpec (routes, verbs, headers, query and body binding, constraints, formats, encodings, doc comments, deprecation, error models) is read as written. See the [onekit README](https://github.com/1homsi/onekit#writing-an-api-in-typespec) for how TypeSpec maps onto the generated code, and [`lib/main.tsp`](lib/main.tsp) for every `Onkt` decorator.

## The emitter on its own

The emitter writes `schema.json`, onekit's versioned schema document, to its output directory:

```bash
npx tsp compile main.tsp --emit @onkt/typespec
```

`onkt` reads that document; the format is described in `src/schema.ts`.
