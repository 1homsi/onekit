import "../dist/src/index.js";

using TypeSpec.Reflection;

/**
 * Decorators for the onkt features TypeSpec has no standard decorator for.
 * Everything TypeSpec already says (routes, verbs, headers, constraints,
 * formats, encodings, docs, deprecation, `@error` models) is read from the
 * standard decorators.
 */
namespace Onkt;

/** Marks the one model that describes the authenticated caller; `@authorize` expressions see it as `auth`. */
extern dec principal(target: Model);

/**
 * A validation rule in the onkt expression language. On a model the
 * expression sees `self`; on a property it also sees `value`.
 */
extern dec rule(target: Model | ModelProperty, expression: valueof string, message: valueof string);

/** The value must be present and non-empty, not just present on the wire. */
extern dec required(target: ModelProperty);

/** Inlines a message property's fields into its parent object, optionally under a key prefix. */
extern dec flatten(target: ModelProperty, prefix?: valueof string);

/** How an empty message property is written: `null`, omitted, or preserved as `{}`. */
extern dec empty(target: ModelProperty, mode: valueof "null" | "omit" | "preserve");

/** Writes the property's value in place of the whole message on the wire. */
extern dec unwrap(target: ModelProperty);

/** An `unknown` property that must hold a JSON object. */
extern dec object(target: ModelProperty);

/** Correlates WebSocket request and response frames by this property. */
extern dec wsId(target: ModelProperty);

/** The deadline of a WebSocket call, in milliseconds. */
extern dec wsTimeout(target: ModelProperty);

/** Carries this WebSocket property as raw binary frames. */
extern dec raw(target: ModelProperty);

/** Names the discriminator key of a union whose variants are keyed by name (default `type`). */
extern dec discriminator(target: Union, name: valueof string);

/** The discriminator value of a union variant when it differs from the variant name. */
extern dec tag(target: UnionVariant, value: valueof string);

/** The operation answers with a stream of server-sent events of its return type. */
extern dec stream(target: Operation);

/** The operation is a bidirectional WebSocket call on its route. */
extern dec ws(target: Operation);

/** A rule the server enforces after authentication; `auth` is the `@principal` model and `req` the request. */
extern dec authorize(target: Operation, expression: valueof string, message: valueof string);

/** Scopes the caller must hold; every listed scope is required. */
extern dec requires(target: Operation, ...scopes: valueof string[]);

/** A free-form key and value handed to server code with the route. */
extern dec meta(target: Operation, key: valueof string, value: valueof string);

/** A permission guard handed to server code with the route. */
extern dec guard(target: Operation, ...guards: valueof string[]);

/** Caps this operation's request body, such as "64MiB". */
extern dec maxBody(target: Operation, size: valueof string);

/**
 * The route is handled by hand-written code instead of a JSON handler.
 * `contentType` is the response content type, `request` is none, json,
 * multipart or stream, and `response` is stream, json, redirect or websocket.
 */
extern dec http(
  target: Operation,
  options?: valueof {
    contentType?: string;
    request?: "none" | "json" | "multipart" | "stream";
    response?: "stream" | "json" | "redirect" | "websocket";
  }
);

/** The header carries credentials: an API key, a bearer token or basic credentials. */
extern dec auth(target: ModelProperty, scheme: valueof "api_key" | "bearer" | "basic", schemeName?: valueof string);

/** An example value of a header, for documentation. */
extern dec example(target: ModelProperty, value: valueof string);

/**
 * Serves the route on an HTTP method TypeSpec has no decorator for: `ANY`
 * answers every method, `QUERY` is the safe method that carries a body.
 */
extern dec method(target: Operation, method: valueof "ANY" | "QUERY");

/**
 * The values a property may hold. A union of string literals says the same
 * for a string property that has no other string constraint.
 */
extern dec values(target: ModelProperty, ...values: valueof (string | numeric)[]);

/**
 * An inclusive numeric range, checked as one bound pair. `@minValue` and
 * `@maxValue` check each bound on its own.
 */
extern dec range(target: ModelProperty, min: valueof numeric, max: valueof numeric);

/** The JSON key of a union variant when it differs from the variant name. */
extern dec json(target: UnionVariant, key: valueof string);

/** A WebSocket client sends this variant to cancel a call. */
extern dec wsCancel(target: UnionVariant);

/** The key type of a `Record` property whose JSON object keys are integers. */
extern dec mapKey(target: ModelProperty, key: valueof "int32" | "int64" | "uint32" | "uint64");

/**
 * The wire encoding of a property when TypeSpec's `@encode` has no
 * equivalent: hex, base64, base64_raw, base64url_raw, unix_millis, or
 * number on an enum.
 */
extern dec encode(target: ModelProperty, encoding: valueof string);

/** Declares this model or enum inside another model, as a nested type. */
extern dec parent(target: Model | Enum, parent: Model);

/**
 * The service name onkt generates from, when the interface's own name is
 * taken: TypeSpec gives an interface and a model or enum one namespace, so
 * `@Onkt.name("Branding") interface BrandingService` serves `Branding`
 * beside a `Branding` model.
 */
extern dec name(target: Interface, name: valueof string);
