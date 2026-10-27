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

function record(context: DecoratorContext, target: Type, name: string, args: OnktArg[]): void {
  const state = context.program.stateMap(StateKeys.onktDecorators);
  const list: OnktDecorator[] = state.get(target) ?? [];
  list.push({ name, args });
  state.set(target, list);
}

/** The onkt decorators on a type, in source order. */
export function getOnktDecorators(program: Program, target: Type): OnktDecorator[] {
  return program.stateMap(StateKeys.onktDecorators).get(target) ?? [];
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
    http: (
      ctx: DecoratorContext,
      target: Type,
      contentType?: string,
      options?: { request?: string; response?: string },
    ) => {
      const args: OnktArg[] = contentType === undefined ? [] : [str(contentType)];
      if (options?.request !== undefined) args.push({ name: "request", value: options.request, quoted: true });
      if (options?.response !== undefined) args.push({ name: "response", value: options.response, quoted: true });
      record(ctx, target, "http", args);
    },
    auth: (ctx: DecoratorContext, target: Type, scheme: string, schemeName?: string) => {
      record(ctx, target, "auth", [{ value: scheme }]);
      if (schemeName !== undefined) record(ctx, target, "auth_scheme_name", [str(schemeName)]);
    },
  },
};
