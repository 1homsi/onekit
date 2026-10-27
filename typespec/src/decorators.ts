import type { DecoratorContext, Program, Type } from "@typespec/compiler";
import { StateKeys } from "./lib.js";

/** One decorator argument as the onkt schema document writes it. */
export interface OnktArg {
  name?: string;
  value: string;
  quoted?: boolean;
}

/** An onkt decorator recorded on a type, already in schema-document form. */
export interface OnktDecorator {
  name: string;
  args: OnktArg[];
}

// The decorators each application wrote, keyed by the application's node and
// its target (spreading a model re-applies its decorators to the copies), so
// the emitter can walk a property's decorators in source order.
const byApplication = new WeakMap<object, WeakMap<Type, OnktDecorator[]>>();

function record(context: DecoratorContext, target: Type, name: string, args: OnktArg[]): void {
  // TypeSpec applies a declaration's decorators bottom-up; prepending keeps
  // the list in source order.
  const state = context.program.stateMap(StateKeys.onktDecorators);
  const list: OnktDecorator[] = state.get(target) ?? [];
  list.unshift({ name, args });
  state.set(target, list);
  const node = context.decoratorTarget as object;
  const targets = byApplication.get(node) ?? new WeakMap<Type, OnktDecorator[]>();
  const applied = targets.get(target) ?? [];
  applied.unshift({ name, args });
  targets.set(target, applied);
  byApplication.set(node, targets);
}

/** The onkt decorators one decorator application recorded on a target. */
export function getAppliedOnktDecorators(node: object | undefined, target: Type): OnktDecorator[] {
  return node ? (byApplication.get(node)?.get(target) ?? []) : [];
}

/** The onkt decorators on a type, in source order. */
export function getOnktDecorators(program: Program, target: Type): OnktDecorator[] {
  return program.stateMap(StateKeys.onktDecorators).get(target) ?? [];
}

/** The model a model or enum is declared inside, from `@Onkt.parent`. */
export function getOnktParent(program: Program, target: Type): Type | undefined {
  return program.stateMap(StateKeys.onktParent).get(target);
}

/** The service name an interface generates under, from `@Onkt.name`. */
export function getOnktName(program: Program, target: Type): string | undefined {
  return program.stateMap(StateKeys.onktName).get(target);
}

export function findOnktDecorator(program: Program, target: Type, name: string): OnktDecorator | undefined {
  return getOnktDecorators(program, target).find((d) => d.name === name);
}

const str = (value: string): OnktArg => ({ value, quoted: true });

// Decorator names here are the schema-document spellings, which differ from
// the TypeSpec names only where TypeSpec wants camelCase.
export const $decorators = {
  Onkt: {
    principal: (ctx: DecoratorContext, target: Type) => record(ctx, target, "principal", []),
    rule: (ctx: DecoratorContext, target: Type, expression: string, message: string) =>
      record(ctx, target, "rule", [str(expression), str(message)]),
    required: (ctx: DecoratorContext, target: Type) => record(ctx, target, "required", []),
    flatten: (ctx: DecoratorContext, target: Type, prefix?: string) =>
      record(ctx, target, "flatten", prefix === undefined ? [] : [{ name: "prefix", value: prefix, quoted: true }]),
    empty: (ctx: DecoratorContext, target: Type, mode: string) => record(ctx, target, "empty", [str(mode)]),
    unwrap: (ctx: DecoratorContext, target: Type) => record(ctx, target, "unwrap", []),
    object: (ctx: DecoratorContext, target: Type) => record(ctx, target, "object", []),
    wsId: (ctx: DecoratorContext, target: Type) => record(ctx, target, "ws_id", []),
    wsTimeout: (ctx: DecoratorContext, target: Type) => record(ctx, target, "ws_timeout", []),
    raw: (ctx: DecoratorContext, target: Type) => record(ctx, target, "raw", []),
    discriminator: (ctx: DecoratorContext, target: Type, name: string) => record(ctx, target, "discriminator", [str(name)]),
    tag: (ctx: DecoratorContext, target: Type, value: string) => record(ctx, target, "tag", [str(value)]),
    stream: (ctx: DecoratorContext, target: Type) => record(ctx, target, "stream", []),
    ws: (ctx: DecoratorContext, target: Type) => record(ctx, target, "ws", []),
    authorize: (ctx: DecoratorContext, target: Type, expression: string, message: string) =>
      record(ctx, target, "authorize", [str(expression), str(message)]),
    requires: (ctx: DecoratorContext, target: Type, ...scopes: string[]) => record(ctx, target, "requires", scopes.map(str)),
    meta: (ctx: DecoratorContext, target: Type, key: string, value: string) => record(ctx, target, "meta", [str(key), str(value)]),
    guard: (ctx: DecoratorContext, target: Type, ...guards: string[]) => record(ctx, target, "guard", guards.map(str)),
    maxBody: (ctx: DecoratorContext, target: Type, size: string) => record(ctx, target, "max_body", [str(size)]),
    http: (ctx: DecoratorContext, target: Type, options?: { contentType?: string; request?: string; response?: string }) => {
      const args: OnktArg[] = options?.contentType === undefined ? [] : [str(options.contentType)];
      if (options?.request !== undefined) args.push({ name: "request", value: options.request, quoted: true });
      if (options?.response !== undefined) args.push({ name: "response", value: options.response, quoted: true });
      record(ctx, target, "http", args);
    },
    example: (ctx: DecoratorContext, target: Type, value: string) => record(ctx, target, "example", [str(value)]),
    method: (ctx: DecoratorContext, target: Type, method: string) => record(ctx, target, "method", [str(method)]),
    values: (ctx: DecoratorContext, target: Type, ...values: unknown[]) =>
      record(ctx, target, "in", values.map((v) => (typeof v === "string" ? str(v) : { value: String(v) }))),
    range: (ctx: DecoratorContext, target: Type, min: unknown, max: unknown) =>
      record(ctx, target, "range", [{ value: String(min) }, { value: String(max) }]),
    json: (ctx: DecoratorContext, target: Type, key: string) => record(ctx, target, "json", [str(key)]),
    wsCancel: (ctx: DecoratorContext, target: Type) => record(ctx, target, "ws_cancel", []),
    mapKey: (ctx: DecoratorContext, target: Type, key: string) => record(ctx, target, "map_key", [str(key)]),
    encode: (ctx: DecoratorContext, target: Type, encoding: string) => record(ctx, target, "encode", [str(encoding)]),
    parent: (ctx: DecoratorContext, target: Type, parent: Type) => {
      const state = ctx.program.stateMap(StateKeys.onktParent);
      state.set(target, parent);
    },
    name: (ctx: DecoratorContext, target: Type, name: string) => {
      ctx.program.stateMap(StateKeys.onktName).set(target, name);
    },
    auth: (ctx: DecoratorContext, target: Type, scheme: string, schemeName?: string) => {
      // Recorded in reverse: each record goes in front of the last.
      if (schemeName !== undefined) record(ctx, target, "auth_scheme_name", [str(schemeName)]);
      record(ctx, target, "auth", [{ value: scheme }]);
    },
  },
};
