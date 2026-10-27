import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { after, describe, it } from "node:test";
import { NodeHost, compile } from "@typespec/compiler";
import { buildDocument } from "../src/emitter.js";
import type { Document, MessageDecl, ServiceDecl } from "../src/schema.js";

const dirs: string[] = [];
after(() => dirs.forEach((dir) => rmSync(dir, { recursive: true, force: true })));

// Compiles a TypeSpec source and returns its onkt schema document. The file
// lives under the package so `@typespec/http` resolves from the workspace.
const scratch = join(import.meta.dirname, "..", "..", ".test-scratch");
mkdirSync(scratch, { recursive: true });

async function emit(source: string): Promise<Document> {
  const dir = mkdtempSync(join(scratch, "case-"));
  dirs.push(dir);
  const main = join(dir, "main.tsp");
  const lib = join(import.meta.dirname, "..", "..", "lib", "main.tsp");
  writeFileSync(main, `import "@typespec/http";\nimport "${lib.replaceAll("\\", "/")}";\nusing TypeSpec.Http;\n${source}`);
  const program = await compile(NodeHost, main, { noEmit: true });
  const errors = program.diagnostics.filter((d) => d.severity === "error");
  assert.deepEqual(errors.map((d) => d.message), []);
  return buildDocument(program);
}

const messages = (doc: Document): MessageDecl[] => doc.files.flatMap((f) => f.messages ?? []);
const services = (doc: Document): ServiceDecl[] => doc.files.flatMap((f) => f.services ?? []);

describe("@onkt/typespec emitter", () => {
  it("uses a spread model as the request and hoists spread headers to the service", async () => {
    const doc = await emit(`
      namespace app;
      alias Auth = { @header("X-Key") key: string };
      model GetUser { id: string; }
      model User { id: string; }
      @route("/v1") interface Users {
        @get @route("/users/{id}") get(...Auth, ...GetUser): User;
        @get @route("/me") me(...Auth, @header("X-Trace") trace?: string, ...GetUser): User;
      }`);
    const [service] = services(doc);
    assert.equal(service.basePath, "/v1");
    assert.deepEqual(service.headers?.map((h) => h.name), ["X-Key"]);
    const [get, me] = service.rpcs!;
    assert.equal(get.request, "app.GetUser");
    assert.equal(get.headers, undefined);
    assert.deepEqual(me.headers?.map((h) => [h.name, h.decorators?.map((d) => d.name) ?? []]), [["X-Trace", []]]);
    const verb = get.decorators![0];
    assert.deepEqual([verb.name, verb.args], ["get", [{ value: "/users/{id}", quoted: true }]]);
    // The verb reports where @get is written, ahead of the operation name.
    assert.equal(verb.line, get.line);
    assert.ok(verb.col! < get.col!);
  });

  it("keeps constraint decorators in source order and maps @minLength/@maxLength to len", async () => {
    const doc = await emit(`
      namespace app;
      model Pet {
        @maxValue(30) @minValue(0) age: int32;
        @minLength(1) @maxLength(40) name: string;
        @encode(string) big: int64;
        small: int64;
      }`);
    const fields = messages(doc)[0].fields!;
    assert.deepEqual(fields[0].decorators?.map((d) => d.name), ["lte", "gte"]);
    assert.deepEqual(fields[1].decorators?.map((d) => [d.name, d.args?.map((a) => a.value)]), [["len", ["1", "40"]]]);
    assert.equal(fields[2].decorators, undefined);
    assert.deepEqual(fields[3].decorators?.map((d) => [d.name, d.args?.[0].value]), [["encode", "number"]]);
  });

  it("turns @error models into messages with @status and errors on the operation", async () => {
    const doc = await emit(`
      namespace app;
      model Req { id: string; }
      model Item { id: string; }
      @error model NotFound { @statusCode _: 404; message: string; }
      interface Items { @get @route("/items/{id}") get(...Req): Item | NotFound; }`);
    const notFound = messages(doc).find((m) => m.name === "NotFound")!;
    assert.deepEqual(notFound.decorators?.map((d) => [d.name, d.args?.[0].value]), [["status", "404"]]);
    assert.deepEqual(notFound.fields?.map((f) => f.name), ["message"]);
    assert.deepEqual(services(doc)[0].rpcs?.[0].errors, ["app.NotFound"]);
  });

  it("maps unions to oneofs, nullable properties and literal unions", async () => {
    const doc = await emit(`
      namespace app;
      model A { a: string; }
      model B { b: string; }
      @Onkt.discriminator("kind") union Choice { a: A, b: B }
      @discriminated(#{ discriminatorPropertyName: "type", envelope: "none" }) union Flat { a: A, b: B }
      model M {
        choice: Choice;
        flat: Flat;
        note?: string | null;
        color: "red" | "blue";
      }`);
    const fields = messages(doc).find((m) => m.name === "M")!.fields!;
    assert.deepEqual(fields[0].oneof?.args, [{ name: "discriminator", value: "kind", quoted: true }]);
    assert.deepEqual(fields[1].oneof?.args, [
      { name: "discriminator", value: "type", quoted: true },
      { name: "flatten", value: "true" },
    ]);
    assert.deepEqual(fields[2].decorators?.map((d) => d.name), ["nullable"]);
    assert.equal(fields[2].optional, true);
    assert.deepEqual(fields[3].decorators?.map((d) => [d.name, d.args?.map((a) => a.value)]), [["in", ["red", "blue"]]]);
  });

  it("serves an interface under its @Onkt.name", async () => {
    const doc = await emit(`
      namespace app;
      model Branding { name: string; }
      model Lookup { slug: string; }
      @Onkt.name("Branding") interface BrandingService {
        @get @route("/branding/{slug}") get(...Lookup): Branding;
      }`);
    assert.deepEqual(services(doc).map((s) => s.name), ["Branding"]);
  });
  it("nests @Onkt.parent types and spells {+path} as a wildcard", async () => {
    const doc = await emit(`
      namespace app;
      model Outer { inner: Inner; }
      @Onkt.parent(Outer) model Inner { x: string; }
      model FileRef { path: string; }
      @route("/files") interface Files { @get @route("/{+path}") read(...FileRef): FileRef; }`);
    const outer = messages(doc).find((m) => m.name === "Outer")!;
    assert.deepEqual(outer.messages?.map((m) => m.name), ["Inner"]);
    assert.equal(messages(doc).some((m) => m.name === "Inner"), false);
    assert.equal(services(doc)[0].rpcs?.[0].decorators?.[0].args?.[0].value, "/{path...}");
  });
});

describe("@useAuth", () => {
  it("puts credentials every operation shares on the service", async () => {
    const doc = await emit(`
      namespace app;
      model Req { id: string; }
      @useAuth(BearerAuth)
      @route("/v1") interface Items {
        @get @route("/a") a(...Req): Req;
        @get @route("/b") b(...Req): Req;
      }`);
    const [service] = services(doc);
    assert.deepEqual(
      service.headers?.map((h) => [h.name, h.decorators?.map((d) => [d.name, d.args?.[0]?.value ?? null])]),
      [["Authorization", [["required", null], ["auth", "bearer"]]]],
    );
    assert.equal(service.rpcs?.[0].headers, undefined);
  });

  it("gives operations their own credentials when one overrides the service", async () => {
    const doc = await emit(`
      namespace app;
      model Req { id: string; }
      @useAuth(ApiKeyAuth<ApiKeyLocation.header, "X-API-Key">)
      interface Items {
        @get @route("/a") a(...Req): Req;
        @useAuth(NoAuth) @post @route("/login") login(...Req): Req;
      }`);
    const [service] = services(doc);
    assert.equal(service.headers, undefined);
    assert.deepEqual(service.rpcs?.[0].headers?.map((h) => h.name), ["X-API-Key"]);
    assert.equal(service.rpcs?.[1].headers, undefined);
  });
});

describe("64-bit integers", () => {
  it("writes repeated int64 as numbers unless told to keep the default", async () => {
    const doc = await emit(`
      namespace app;
      model M {
        ids: int64[];
        @Onkt.encode("string") legacy: uint64[];
        counts: int32[];
      }`);
    const fields = messages(doc)[0].fields!;
    assert.deepEqual(fields[0].decorators?.map((d) => [d.name, d.args?.[0].value]), [["encode", "number"]]);
    assert.equal(fields[1].decorators, undefined);
    assert.equal(fields[2].decorators, undefined);
  });
});
