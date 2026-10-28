import {
  type EmitContext,
  type Interface,
  type Enum,
  type Model,
  type ModelProperty,
  type Namespace,
  type Operation,
  type Program,
  type Scalar,
  type Type,
  type Union,
  emitFile,
  getDeprecationDetails,
  getDiscriminatedUnion,
  getDoc,
  getEncode,
  getFormat,
  getLocationContext,
  getMaxItems,
  getMaxLength,
  getMaxValue,
  getMaxValueExclusive,
  getMinItems,
  getMinLength,
  getMinValue,
  getMinValueExclusive,
  getNamespaceFullName,
  getPattern,
  getSourceLocation,
  isArrayModelType,
  isErrorModel,
  isNullType,
  isRecordModelType,
  isTemplateDeclaration,
  navigateProgram,
  resolveEncodedName,
  resolvePath,
} from "@typespec/compiler";
import {
  type Authentication,
  type HttpOperation,
  getAuthenticationForOperation,
  getHttpOperation,
  getHeaderFieldName,
  getRoutePath,
  isBody,
  isBodyRoot,
  isHeader,
  getQueryParamName,
  isStatusCode,
  getStatusCodes,
} from "@typespec/http";
import { type OnktArg, findOnktDecorator, getAppliedOnktDecorators, getOnktDecorators, getOnktName, getOnktParent } from "./decorators.js";
import { type OnktEmitterOptions, reportDiagnostic } from "./lib.js";
import type * as S from "./schema.js";

export async function $onEmit(context: EmitContext<OnktEmitterOptions>): Promise<void> {
  const document = buildDocument(context.program);
  if (context.program.compilerOptions.noEmit || context.program.hasError()) {
    return;
  }
  await emitFile(context.program, {
    path: resolvePath(context.emitterOutputDir, context.options["output-file"] ?? "schema.json"),
    content: JSON.stringify(document, null, 2) + "\n",
  });
}

/** Builds the onkt schema document for every declaration in the project's own sources. */
export function buildDocument(program: Program): S.Document {
  return new DocumentBuilder(program).build();
}

const str = (value: string): OnktArg => ({ value, quoted: true });
const num = (value: number | bigint): OnktArg => ({ value: String(value) });

interface Position {
  line?: number;
  col?: number;
}

class DocumentBuilder {
  private readonly files = new Map<string, S.SourceFile>();
  private readonly messageNames = new Map<Model, string>();
  private readonly enumNames = new Map<Enum, string>();
  private readonly messageDecls = new Map<Model, S.MessageDecl>();
  private readonly pending: Array<() => void> = [];
  /** Source offset of each declaration's anchor, so declarations keep source order. */
  private readonly offsets = new WeakMap<object, number>();

  constructor(private readonly program: Program) {}

  build(): S.Document {
    const operations: Operation[] = [];
    const interfaces: Interface[] = [];
    navigateProgram(
      this.program,
      {
        model: (model) => {
          if (this.isProjectDeclaration(model) && model.name !== "" && !isArrayModelType(model) && !isRecordModelType(model)) {
            this.messageRef(model, undefined);
          }
        },
        enum: (en) => {
          if (this.isProjectDeclaration(en)) this.enumRef(en);
        },
        operation: (op) => {
          if (this.isProjectOperation(op)) operations.push(op);
        },
        interface: (iface) => {
          if (!iface.templateMapper && !isTemplateDeclaration(iface) && getLocationContext(this.program, iface).type === "project") {
            interfaces.push(iface);
          }
        },
      },
      { includeTemplateDeclaration: false },
    );
    this.emitServices(operations, interfaces);
    while (this.pending.length > 0) this.pending.shift()!();
    const files = [...this.files.values()].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
    const sortDecls = (messages?: S.MessageDecl[], enums?: S.EnumDecl[]) => {
      messages?.sort((a, b) => this.order(a) - this.order(b));
      enums?.sort((a, b) => this.order(a) - this.order(b));
      for (const m of messages ?? []) sortDecls(m.messages, m.enums);
    };
    for (const file of files) sortDecls(file.messages, file.enums);
    return { version: 1, files };
  }

  // ---------------------------------------------------------------- files

  private isProjectDeclaration(type: Model | Enum | Union | Operation): boolean {
    if ("templateNode" in type && type.templateNode && isTemplateDeclaration(type as Model)) return false;
    if (type.kind === "Model" && type.templateMapper) return false;
    return getLocationContext(this.program, type).type === "project";
  }

  private isProjectOperation(op: Operation): boolean {
    if (op.templateMapper || (op.node && isTemplateDeclaration(op))) return false;
    if (op.interface && (op.interface.templateMapper || isTemplateDeclaration(op.interface))) return false;
    return getLocationContext(this.program, op).type === "project";
  }

  private namespaceOf(type: { namespace?: Namespace }): string {
    return type.namespace ? getNamespaceFullName(type.namespace) : "";
  }

  /** The schema file a declaration belongs in: its source file and namespace. */
  private fileFor(type: Type, pkg: string): S.SourceFile {
    const location = getSourceLocation(type);
    let path = location.file.path;
    let file = this.files.get(path);
    if (file && file.package !== pkg) {
      // A source file declaring several namespaces becomes one schema file
      // per namespace; the suffix keeps them in the source's directory.
      path = `${location.file.path}#${pkg}`;
      file = this.files.get(path);
    }
    if (!file) {
      file = { path, package: pkg };
      this.files.set(path, file);
    }
    return file;
  }

  private position(type: Type): Position {
    const location = getSourceLocation(type, { locateId: true });
    if (!location || location.isSynthetic) return {};
    const { line, character } = location.file.getLineAndCharacterOfPosition(location.pos);
    return { line: line + 1, col: character + 1 };
  }

  /** Where a decorator application sits, falling back to its target. */
  private nodePosition(node: object | undefined, fallback: Type): Position {
    const location = node ? getSourceLocation(node as Parameters<typeof getSourceLocation>[0]) : undefined;
    if (!location || location.isSynthetic) return this.position(fallback);
    const { line, character } = location.file.getLineAndCharacterOfPosition(location.pos);
    return { line: line + 1, col: character + 1 };
  }

  /** The position of the decorator named `name` (such as "@post") on a type. */
  private decoratorPosition(type: Type & { decorators?: { definition?: { name: string }; node?: object }[] }, name: string): Position {
    const app = type.decorators?.find((d) => d.definition?.name === name);
    return this.nodePosition(app?.node, type);
  }

  /** A type's onkt decorators in source order, each at its own position. */
  private onktWithPositions(type: Model | Operation): S.Decorator[] {
    const out: S.Decorator[] = [];
    for (const app of [...type.decorators].reverse()) {
      if (app.definition?.namespace?.name !== "Onkt") continue;
      const position = this.nodePosition(app.node, type);
      for (const d of getAppliedOnktDecorators(app.node, type)) out.push({ ...d, ...position });
    }
    return out;
  }

  private span(type: Type): S.Span {
    const location = getSourceLocation(type);
    if (!location || location.isSynthetic) return {};
    const start = location.file.getLineAndCharacterOfPosition(location.pos);
    const end = location.file.getLineAndCharacterOfPosition(location.end);
    return { line: start.line + 1, col: start.character + 1, endLine: end.line + 1, endCol: end.character + 1 };
  }

  private place<T extends object>(decl: T, anchor: Type): T {
    this.offsets.set(decl, getSourceLocation(anchor)?.pos ?? Number.MAX_SAFE_INTEGER);
    return decl;
  }

  private order(decl: object): number {
    return this.offsets.get(decl) ?? Number.MAX_SAFE_INTEGER;
  }

  private qualify(pkg: string, name: string): string {
    return pkg === "" ? name : `${pkg}.${name}`;
  }

  // ------------------------------------------------------------- messages

  /**
   * Returns the qualified schema name of a model, declaring it on first use.
   * Anonymous and template-instance models are declared next to the
   * declaration that first refers to them, under `fallbackName`.
   */
  private messageRef(model: Model, fallback: { name: string; owner: Type; pkg: string } | undefined): string {
    const known = this.messageNames.get(model);
    if (known) return known;

    let name = model.name;
    let pkg = this.namespaceOf(model);
    let anchor: Type = model;
    if (name === "" || model.templateMapper || getLocationContext(this.program, model).type !== "project") {
      if (model.templateMapper && model.name !== "") {
        name = model.name + model.templateMapper.args.map((arg) => typeDisplayName(arg)).join("");
      } else if (fallback) {
        name = fallback.name;
      }
      if (fallback) {
        pkg = fallback.pkg;
        anchor = fallback.owner;
      }
    }
    const qualified = this.qualify(pkg, name);
    this.messageNames.set(model, qualified);

    const decl: S.MessageDecl = this.place({ name, ...this.position(model) }, anchor);
    this.messageDecls.set(model, decl);
    const parent = this.parentDecl(model);
    if (parent) {
      (parent.messages ??= []).push(decl);
    } else {
      const file = this.fileFor(anchor, pkg);
      (file.messages ??= []).push(decl);
    }
    this.pending.push(() => this.fillMessage(model, decl, pkg, anchor));
    return qualified;
  }

  /** The declaration of the model `@Onkt.parent` nests a type inside. */
  private parentDecl(type: Model | Enum): S.MessageDecl | undefined {
    const parent = getOnktParent(this.program, type);
    if (!parent || parent.kind !== "Model") return undefined;
    this.messageRef(parent, undefined);
    return this.messageDecls.get(parent);
  }

  private fillMessage(model: Model, decl: S.MessageDecl, pkg: string, anchor: Type): void {
    decl.doc = this.doc(model);
    const decorators: S.Decorator[] = [];
    if (isErrorModel(this.program, model)) {
      const status = this.errorStatus(model);
      if (status !== undefined) decorators.push({ name: "status", args: [num(status)], ...this.position(model) });
    }
    for (const d of this.onktWithPositions(model)) decorators.push(d);
    if (decorators.length > 0) decl.decorators = decorators;

    const fields: S.FieldDecl[] = [];
    for (const prop of allProperties(model)) {
      if (isStatusCode(this.program, prop)) continue;
      const field = this.field(prop, decl.name, pkg, anchor);
      if (field) fields.push(field);
    }
    if (fields.length > 0) decl.fields = fields;
  }

  private errorStatus(model: Model): number | undefined {
    for (const prop of allProperties(model)) {
      if (!isStatusCode(this.program, prop)) continue;
      const codes = getStatusCodes(this.program, prop);
      const first = codes[0];
      if (codes.length === 1 && typeof first === "number") return first;
      if (prop.type.kind === "Number") return prop.type.value;
      reportDiagnostic(this.program, { code: "unsupported", format: { what: "a status code range on an error model" }, target: prop });
    }
    return undefined;
  }

  private field(prop: ModelProperty, ownerName: string, pkg: string, anchor: Type): S.FieldDecl | undefined {
    const name = resolveEncodedName(this.program, prop as ModelProperty & { name: string }, "application/json");
    const position = this.position(prop);
    const field: S.FieldDecl = { name, ...position };
    const doc = this.doc(prop);
    if (doc) field.doc = doc;
    if (prop.optional) field.optional = true;

    const decorators: S.Decorator[] = [];
    const add = (decoratorName: string, args: OnktArg[] = []) =>
      decorators.push({ name: decoratorName, ...(args.length > 0 ? { args } : {}), ...position });

    let type = prop.type;
    let nullable = false;
    if (type.kind === "Union") {
      const nonNull = [...type.variants.values()].filter((v) => !isNullType(v.type));
      if (nonNull.length === 1 && nonNull.length < type.variants.size) {
        type = nonNull[0].type;
        nullable = true;
      }
    }

    if (type.kind === "Union" && !this.isLiteralUnion(type)) {
      field.oneof = this.oneof(type, prop, ownerName, pkg, anchor);
    } else if (type.kind === "Model" && isArrayModelType(this.program, type)) {
      field.repeated = true;
      field.type = this.typeRef(type.indexer.value, prop, `${ownerName}${pascal(prop.name)}Item`, pkg, anchor);
    } else {
      field.type = this.typeRef(type, prop, `${ownerName}${pascal(prop.name)}`, pkg, anchor);
    }

    this.appliedDecorators(prop, type, field, add);
    if (prop.type.kind === "Union" && this.isLiteralUnion(prop.type)) {
      add("in", [...prop.type.variants.values()].map((v) => literalArg(v.type)));
    }
    const deprecated = getDeprecationDetails(this.program, prop);
    if (deprecated) add("deprecated", deprecated.message ? [str(deprecated.message)] : []);
    if (nullable) add("nullable");
    if (decorators.length > 0) field.decorators = decorators;
    return field;
  }

  /**
   * Translates a property's decorators in the order they are written, since
   * generated validation checks follow that order. `@minLength` and
   * `@maxLength` written next to each other become one `len`.
   */
  private appliedDecorators(prop: ModelProperty, type: Type, field: S.FieldDecl, add: (name: string, args?: OnktArg[]) => void): void {
    const itemType = type.kind === "Model" && isArrayModelType(this.program, type) ? type.indexer.value : type;
    const scalar = itemType.kind === "Scalar" ? stdScalar(itemType) : undefined;
    const mapValue = type.kind === "Model" && isRecordModelType(this.program, type) ? type.indexer.value : undefined;
    const mapScalar = mapValue?.kind === "Scalar" ? stdScalar(mapValue) : undefined;
    // TypeSpec lists decorators in the order it applied them, bottom-up.
    const apps = [...prop.decorators].reverse();
    const nameOf = (i: number) => apps[i]?.definition?.name;
    const arg = (i: number, j = 0) => numericArg(apps[i].args[j]?.jsValue);
    let sawLength = false;
    let sawPattern = false;
    let sawFormat = false;
    let sawEncode = false;
    for (let i = 0; i < apps.length; i++) {
      const app = apps[i];
      if (app.definition?.namespace?.name === "Onkt") {
        for (const d of getAppliedOnktDecorators(app.node, prop)) {
          if (d.name === "map_key") {
            if (field.type?.map) field.type.mapKey = d.args[0].value;
            continue;
          }
          if (d.name === "encode") sawEncode = true;
          add(d.name, d.args);
        }
        continue;
      }
      switch (nameOf(i)) {
        case "@minLength":
          sawLength = true;
          if (nameOf(i + 1) === "@maxLength") add("len", [arg(i), arg(++i)]);
          else add("len", [arg(i), num(Number.MAX_SAFE_INTEGER)]);
          break;
        case "@maxLength":
          sawLength = true;
          if (nameOf(i + 1) === "@minLength") add("len", [arg(i + 1), arg(i++)]);
          else add("len", [num(0), arg(i)]);
          break;
        case "@minValue":
          add("gte", [arg(i)]);
          break;
        case "@maxValue":
          add("lte", [arg(i)]);
          break;
        case "@minValueExclusive":
          add("gt", [arg(i)]);
          break;
        case "@maxValueExclusive":
          add("lt", [arg(i)]);
          break;
        case "@minItems":
          add("min_items", [arg(i)]);
          break;
        case "@maxItems":
          add("max_items", [arg(i)]);
          break;
        case "@pattern":
          sawPattern = true;
          add("pattern", [str(String(app.args[0]?.jsValue))]);
          break;
        case "@format": {
          sawFormat = true;
          const format = String(app.args[0]?.jsValue);
          if (format === "email" || format === "uuid" || format === "uri") add(format);
          else if (format === "url") add("uri");
          break;
        }
        case "@query": {
          const query = getQueryParamName(this.program, prop);
          add("query", query === undefined || query === prop.name ? [] : [str(query)]);
          break;
        }
        case "@encode": {
          sawEncode = true;
          const encode = this.encoding(prop, field.type?.map ? mapScalar : scalar, field);
          if (encode) add("encode", [str(encode)]);
          break;
        }
      }
    }

    // Constraints declared on a custom scalar apply to every property of it.
    if (type.kind === "Scalar") {
      const p = this.program;
      const minLength = getMinLength(p, type);
      const maxLength = getMaxLength(p, type);
      if (!sawLength && (minLength !== undefined || maxLength !== undefined)) {
        add("len", [num(minLength ?? 0), num(maxLength ?? Number.MAX_SAFE_INTEGER)]);
      }
      const pattern = getPattern(p, type);
      if (!sawPattern && pattern !== undefined) add("pattern", [str(pattern)]);
      const format = getFormat(p, type);
      if (!sawFormat && (format === "email" || format === "uuid" || format === "uri")) add(format);
    }
    if (!sawFormat && scalar === "url") add("uri");
    if (!sawEncode) {
      const encode = this.encoding(prop, field.type?.map ? mapScalar : scalar, field);
      if (encode) add("encode", [str(encode)]);
    }
  }

  /**
   * Maps TypeSpec's `@encode` onto onkt's wire encodings. `scalar` is the
   * item scalar of a list and the value scalar of a map: a collection carries
   * an encoding only where onkt applies it to every item, which is a list of
   * 64-bit integers sent as numbers and a list or map of dates.
   */
  private encoding(prop: ModelProperty, scalar: string | undefined, field: S.FieldDecl): string | undefined {
    if (!field.type) return undefined;
    if (field.type.map && scalar !== "plainDate") return undefined;
    if (field.repeated && scalar !== "int64" && scalar !== "uint64" && scalar !== "plainDate") return undefined;
    if (findOnktDecorator(this.program, prop, "encode")) return undefined;
    const encode = getEncode(this.program, prop);
    switch (scalar) {
      case "int64":
      case "uint64":
        // TypeSpec writes 64-bit integers as JSON numbers unless they are
        // `@encode(string)`. The string form is onkt's default, but it is
        // written out so onkt can check it against int64_encoding.
        return encode?.type && stdScalar(encode.type) === "string" ? "string" : "number";
      case "utcDateTime":
      case "offsetDateTime":
        if (encode?.encoding === "unixTimestamp") return "unix_seconds";
        return undefined;
      case "plainDate":
        return "date";
      case "bytes":
        return encode?.encoding !== undefined && BYTES_ENCODINGS.has(encode.encoding) ? encode.encoding : undefined;
    }
    return undefined;
  }

  private isLiteralUnion(union: Union): boolean {
    const variants = [...union.variants.values()];
    return variants.length > 0 && variants.every((v) => v.type.kind === "String" || v.type.kind === "Number");
  }

  private oneof(union: Union, prop: ModelProperty, ownerName: string, pkg: string, anchor: Type): S.OneofDecl {
    const decl: S.OneofDecl = { variants: [], ...this.position(union.name ? union : prop) };
    const [discriminated] = getDiscriminatedUnion(this.program, union);
    if (discriminated) {
      if (discriminated.options.envelope !== "none") {
        reportDiagnostic(this.program, {
          code: "unsupported",
          format: { what: `the "object" envelope of @discriminated (use envelope: "none", or a plain union keyed by variant name)` },
          target: union,
        });
      }
      decl.args = [
        { name: "discriminator", value: discriminated.options.discriminatorPropertyName, quoted: true },
        { name: "flatten", value: "true" },
      ];
    } else {
      const discriminator = findOnktDecorator(this.program, union, "discriminator");
      if (discriminator) decl.args = [{ name: "discriminator", value: discriminator.args[0].value, quoted: true }];
    }
    for (const [key, variant] of union.variants) {
      if (isNullType(variant.type)) {
        reportDiagnostic(this.program, { code: "unsupported", format: { what: "null in a union with several variants" }, target: variant });
        continue;
      }
      const variantName = typeof key === "string" ? key : variantKeyName(variant.type);
      const decorators: S.Decorator[] = [];
      for (const d of getOnktDecorators(this.program, variant)) decorators.push(d);
      const typeName = `${ownerName}${pascal(prop.name)}${pascal(variantName)}`;
      decl.variants.push({
        name: variantName,
        type: this.typeRef(variant.type, variant, typeName, pkg, anchor),
        ...(decorators.length > 0 ? { decorators } : {}),
        ...this.position(variant),
      });
    }
    return decl;
  }

  // ----------------------------------------------------------------- types

  private typeRef(type: Type, at: Type, inlineName: string, pkg: string, anchor: Type): S.TypeRef {
    const span = this.span(at);
    switch (type.kind) {
      case "Scalar": {
        const scalar = stdScalar(type);
        const mapped = scalar === undefined ? undefined : SCALARS[scalar];
        if (mapped === undefined) {
          reportDiagnostic(this.program, { code: "unsupported", format: { what: `the scalar ${type.name}` }, target: at });
          return { span, name: "string" };
        }
        return { span, name: mapped };
      }
      case "Intrinsic":
        if (type.name === "unknown") return { span, name: "json" };
        break;
      case "String":
        return { span, name: "string" };
      case "Boolean":
        return { span, name: "bool" };
      case "Number":
        return { span, name: Number.isInteger(type.value) ? "int64" : "float64" };
      case "Union":
        if (this.isLiteralUnion(type)) {
          const kinds = new Set([...type.variants.values()].map((v) => v.type.kind));
          return { span, name: kinds.has("Number") ? "int64" : "string" };
        }
        break;
      case "Enum":
        return { span, name: this.enumRef(type) };
      case "Model":
        if (isRecordModelType(this.program, type)) {
          return { span, map: true, mapKey: "string", mapValue: this.typeRef(type.indexer.value, at, `${inlineName}Value`, pkg, anchor) };
        }
        if (isArrayModelType(this.program, type)) break;
        return { span, name: this.messageRef(type, { name: inlineName, owner: anchor, pkg }) };
    }
    reportDiagnostic(this.program, { code: "unsupported", format: { what: `this type in this position (${type.kind})` }, target: at });
    return { span, name: "json" };
  }

  // ------------------------------------------------------------------ enums

  private enumRef(en: Enum): string {
    const known = this.enumNames.get(en);
    if (known) return known;
    const pkg = this.namespaceOf(en);
    const qualified = this.qualify(pkg, en.name);
    this.enumNames.set(en, qualified);
    const decl: S.EnumDecl = this.place({ name: en.name, values: [], ...this.position(en) }, en);
    const doc = this.doc(en);
    if (doc) decl.doc = doc;
    for (const member of en.members.values()) {
      const value: S.EnumValueDecl = { name: member.name, ...this.position(member) };
      const memberDoc = this.doc(member);
      if (memberDoc) value.doc = memberDoc;
      if (typeof member.value === "string" && member.value !== member.name) {
        value.decorators = [{ name: "json", args: [str(member.value)], ...this.position(member) }];
      } else if (typeof member.value === "number") {
        reportDiagnostic(this.program, { code: "unsupported", format: { what: "numeric enum values" }, target: member });
      }
      decl.values.push(value);
    }
    const parent = this.parentDecl(en);
    if (parent) {
      (parent.enums ??= []).push(decl);
    } else {
      const file = this.fileFor(en, pkg);
      (file.enums ??= []).push(decl);
    }
    return qualified;
  }

  // -------------------------------------------------------------- services

  private emitServices(operations: Operation[], interfaces: Interface[]): void {
    const byContainer = new Map<Type, HttpOperation[]>();
    for (const iface of interfaces) byContainer.set(iface, []);
    for (const operation of operations) {
      const [op, diagnostics] = getHttpOperation(this.program, operation);
      if (diagnostics.length > 0) this.program.reportDiagnostics(diagnostics);
      const list = byContainer.get(op.container) ?? [];
      list.push(op);
      byContainer.set(op.container, list);
    }
    for (const [container, ops] of byContainer) {
      this.emitService(container as Namespace, ops);
    }
  }

  private emitService(container: Namespace | Type, ops: HttpOperation[]): void {
    const namespace = container.kind === "Namespace" ? container : (container as { namespace?: Namespace }).namespace;
    const pkg = namespace ? getNamespaceFullName(namespace) : "";
    const name = getOnktName(this.program, container) ?? ((container as { name: string }).name || "Service");
    const decl: S.ServiceDecl = { name, ...this.position(container) };
    const doc = this.doc(container);
    if (doc) decl.doc = doc;

    const rpcs = ops.map((op) => this.rpc(op, pkg));
    // Without a @route the service is served under its source directory,
    // which onkt infers when the document leaves the base path out;
    // @route("/") serves it at the root.
    const base = this.containerRoute(container);
    if (base !== undefined) decl.basePath = wildcards(base) || "/";

    // A header every operation spreads in from the same model property
    // (`...ApiKeyHeaders`) belongs to the service; a header an operation
    // declares itself stays on the operation.
    const first = rpcs[0];
    const sharedOrigins = first
      ? first.headerOrigins.filter((origin) => origin !== undefined && rpcs.every((r) => r.headerOrigins.includes(origin)))
      : [];
    if (first && sharedOrigins.length > 0) {
      decl.headers = sharedOrigins.map((origin) => first.decl.headers![first.headerOrigins.indexOf(origin)]);
      for (const r of rpcs) {
        r.decl.headers = r.decl.headers?.filter((_, i) => !sharedOrigins.includes(r.headerOrigins[i]));
        if (r.decl.headers?.length === 0) delete r.decl.headers;
      }
    }
    this.applyAuthentication(decl, ops, rpcs.map((r) => r.decl));
    decl.rpcs = rpcs.map((r) => r.decl);
    const file = this.fileFor(container, pkg);
    (file.services ??= []).push(decl);
  }

  /**
   * Turns `@useAuth` into credential headers. When every operation of the
   * service authenticates the same way the headers belong to the service;
   * otherwise (an operation overriding the service's, or `NoAuth` on a public
   * one) each operation gets its own.
   */
  private applyAuthentication(service: S.ServiceDecl, ops: HttpOperation[], rpcs: S.RPCDecl[]): void {
    const perOperation = ops.map((op) => this.authHeaders(getAuthenticationForOperation(this.program, op.operation), op.operation));
    if (perOperation.every((headers) => headers.length === 0)) return;
    const key = (headers: S.HeaderDecl[]) => JSON.stringify(headers.map((h) => [h.name, h.decorators?.map((d) => [d.name, d.args])]));
    if (perOperation.every((headers) => key(headers) === key(perOperation[0]))) {
      service.headers = [...perOperation[0], ...(service.headers ?? [])];
      return;
    }
    perOperation.forEach((headers, i) => {
      if (headers.length > 0) rpcs[i].headers = [...headers, ...(rpcs[i].headers ?? [])];
    });
  }

  /** The headers one `@useAuth` configuration sends credentials in. */
  private authHeaders(auth: Authentication | undefined, target: Operation): S.HeaderDecl[] {
    if (!auth || auth.options.length === 0) return [];
    if (auth.options.length > 1) {
      reportDiagnostic(this.program, {
        code: "unsupported",
        format: { what: "alternative authentication schemes (`@useAuth(A | B)`); list the schemes a caller sends together as `@useAuth([A, B])`" },
        target,
      });
    }
    const headers: S.HeaderDecl[] = [];
    const position = this.position(target);
    const header = (name: string, scheme: string): S.HeaderDecl => ({
      name,
      type: "string",
      decorators: [
        { name: "required", ...position },
        { name: "auth", args: [{ value: scheme }], ...position },
      ],
      ...position,
    });
    for (const scheme of auth.options[0].schemes) {
      switch (scheme.type) {
        case "noAuth":
          break;
        case "http":
          if (scheme.scheme === "Bearer") headers.push(header("Authorization", "bearer"));
          else if (scheme.scheme === "Basic") headers.push(header("Authorization", "basic"));
          else reportDiagnostic(this.program, { code: "unsupported", format: { what: `the HTTP auth scheme ${(scheme as { scheme: string }).scheme}` }, target });
          break;
        case "apiKey":
          if (scheme.in === "header") headers.push(header(scheme.name, "api_key"));
          else reportDiagnostic(this.program, { code: "unsupported", format: { what: `an API key in the ${scheme.in}; send it in a header` }, target });
          break;
        case "oauth2":
        case "openIdConnect":
          // Both send their access token as a bearer token.
          headers.push(header("Authorization", "bearer"));
          break;
      }
    }
    return headers;
  }

  /**
   * The route an operation container adds: its own `@route` after its
   * namespaces', or undefined when none of them declares one.
   */
  private containerRoute(container: Type): string | undefined {
    const parts: string[] = [];
    let declared = false;
    let current: Namespace | Interface | undefined = container.kind === "Interface" || container.kind === "Namespace" ? container : undefined;
    while (current) {
      const route = getRoutePath(this.program, current)?.path;
      if (route !== undefined) {
        declared = true;
        parts.unshift(route);
      }
      current = current.namespace;
    }
    return declared ? parts.map((part) => part.replace(/\/+$/, "")).join("") : undefined;
  }

  private rpc(op: HttpOperation, pkg: string): { decl: S.RPCDecl; headerOrigins: Array<ModelProperty | undefined> } {
    const operation = op.operation;
    const position = this.position(operation);
    const route = wildcards(getRoutePath(this.program, operation)?.path ?? "");

    const decl: S.RPCDecl = {
      name: operation.name,
      request: "",
      response: "",
      requestSpan: {},
      responseSpan: {},
      ...position,
    };
    const doc = this.doc(operation);
    if (doc) decl.doc = doc;

    const decorators: S.Decorator[] = [];
    const onkt = getOnktDecorators(this.program, operation);
    const isWS = onkt.some((d) => d.name === "ws");
    const method = onkt.find((d) => d.name === "method")?.args[0].value.toLowerCase();
    const routeDecorator = isWS ? "@ws" : method ? "@method" : `@${op.verb}`;
    decorators.push({ name: isWS ? "ws" : (method ?? op.verb), args: [str(route)], ...this.decoratorPosition(operation, routeDecorator) });
    const deprecated = getDeprecationDetails(this.program, operation);
    if (deprecated) decorators.push({ name: "deprecated", ...(deprecated.message ? { args: [str(deprecated.message)] } : {}), ...position });

    const request = this.request(op, pkg);
    decl.request = request.name;
    decl.requestSpan = this.span(operation);
    if (request.bodyField) decorators.push({ name: "body", args: [str(request.bodyField)], ...position });
    if (request.headers.length > 0) decl.headers = request.headers;

    const response = this.response(op, pkg);
    decl.response = response.name;
    decl.responseSpan = this.span(operation);
    if (response.errors.length > 0) {
      decl.errors = response.errors;
      decl.errorSpans = response.errors.map(() => this.span(operation));
    }
    if (response.success !== undefined && response.success !== 200) {
      decorators.push({ name: "success", args: [num(response.success)], ...position });
    }
    for (const d of this.onktWithPositions(operation)) {
      if (d.name !== "ws" && d.name !== "method") decorators.push(d);
    }
    decl.decorators = decorators;
    return { decl, headerOrigins: request.headerOrigins };
  }

  /**
   * Finds or builds the request message. Parameters spread from one model
   * (`op f(...M)`, header-only models aside) or a single `@body` /
   * `@bodyRoot` model use that model; anything else gets a
   * `<Operation>Request` message next to the operation.
   */
  private request(
    op: HttpOperation,
    pkg: string,
  ): { name: string; bodyField?: string; headers: S.HeaderDecl[]; headerOrigins: Array<ModelProperty | undefined> } {
    const operation = op.operation;
    const headers: S.HeaderDecl[] = [];
    const headerOrigins: Array<ModelProperty | undefined> = [];
    const params: ModelProperty[] = [];
    for (const prop of operation.parameters.properties.values()) {
      if (isHeader(this.program, prop)) {
        headers.push(this.header(prop));
        headerOrigins.push(prop.sourceProperty);
      } else {
        params.push(prop);
      }
    }
    const result = (name: string, bodyField?: string) => ({ name, bodyField, headers, headerOrigins });

    const spreads = (operation.parameters.sourceModels ?? [])
      .filter((source) => source.usage === "spread")
      .map((source) => source.model)
      .filter((model) => {
        const props = allProperties(model);
        return props.length === 0 || !props.every((p) => isHeader(this.program, p));
      });
    const [spread] = spreads;
    if (spreads.length === 1 && spread.name !== "" && params.every((p) => p.sourceProperty?.model === spread)) {
      const body = allProperties(spread).find((p) => isBody(this.program, p));
      return result(this.messageRef(spread, undefined), body?.name);
    }
    if (spreads.length === 0 && params.length === 1 && (isBody(this.program, params[0]) || isBodyRoot(this.program, params[0])) && params[0].type.kind === "Model" && params[0].type.name !== "" && !isArrayModelType(this.program, params[0].type)) {
      return result(this.messageRef(params[0].type, undefined));
    }

    const name = `${pascal(operation.name)}Request`;
    const file = this.fileFor(operation, pkg);
    const decl: S.MessageDecl = this.place({ name, ...this.position(operation) }, operation);
    const fields: S.FieldDecl[] = [];
    let bodyField: string | undefined;
    for (const prop of params) {
      if (isBody(this.program, prop) && prop.type.kind === "Model" && !isArrayModelType(this.program, prop.type) && !isRecordModelType(this.program, prop.type)) {
        bodyField = prop.name;
      }
      const field = this.field(prop, name, pkg, operation);
      if (field) fields.push(field);
    }
    if (fields.length > 0) decl.fields = fields;
    (file.messages ??= []).push(decl);
    return result(this.qualify(pkg, name), bodyField);
  }

  private header(prop: ModelProperty): S.HeaderDecl {
    const position = this.position(prop);
    const decorators: S.Decorator[] = [];
    if (!prop.optional) decorators.push({ name: "required", ...position });
    const format = getFormat(this.program, prop);
    if (format === "uuid" || format === "email" || format === "uri") decorators.push({ name: "format", args: [str(format)], ...position });
    const deprecated = getDeprecationDetails(this.program, prop);
    if (deprecated) decorators.push({ name: "deprecated", ...(deprecated.message ? { args: [str(deprecated.message)] } : {}), ...position });
    for (const d of getOnktDecorators(this.program, prop)) decorators.push({ ...d, ...position });
    return {
      name: getHeaderFieldName(this.program, prop),
      type: "string",
      ...(decorators.length > 0 ? { decorators } : {}),
      ...position,
    };
  }

  private response(op: HttpOperation, pkg: string): { name: string; errors: string[]; success?: number } {
    const operation = op.operation;
    const errors: string[] = [];
    let success: number | undefined;
    let name: string | undefined;
    const returns = operation.returnType.kind === "Union" ? [...operation.returnType.variants.values()].map((v) => v.type) : [operation.returnType];
    for (const type of returns) {
      if (type.kind === "Model" && isErrorModel(this.program, type)) {
        errors.push(this.messageRef(type, undefined));
        continue;
      }
      if (name !== undefined) {
        reportDiagnostic(this.program, { code: "request-shape", format: { message: `${operation.name} returns more than one success type` }, target: operation });
        continue;
      }
      if (type.kind === "Intrinsic" && type.name === "void") {
        name = this.emptyMessage(`${pascal(operation.name)}Response`, operation, pkg);
        continue;
      }
      if (type.kind !== "Model") {
        reportDiagnostic(this.program, { code: "unsupported", format: { what: `returning ${type.kind} (return a model)` }, target: operation });
        continue;
      }
      const statusProp = allProperties(type).find((p) => isStatusCode(this.program, p));
      const bodyProp = allProperties(type).find((p) => isBody(this.program, p) || isBodyRoot(this.program, p));
      if (statusProp) {
        const codes = getStatusCodes(this.program, statusProp);
        if (typeof codes[0] === "number") success = codes[0];
        else if (statusProp.type.kind === "Number") success = statusProp.type.value;
      }
      if (bodyProp && bodyProp.type.kind === "Model") {
        name = this.messageRef(bodyProp.type, { name: `${pascal(operation.name)}Response`, owner: operation, pkg });
      } else if (statusProp && allProperties(type).length === 1) {
        name = this.emptyMessage(`${pascal(operation.name)}Response`, operation, pkg);
      } else {
        name = this.messageRef(type, { name: `${pascal(operation.name)}Response`, owner: operation, pkg });
      }
    }
    return { name: name ?? this.emptyMessage(`${pascal(operation.name)}Response`, operation, pkg), errors, success };
  }

  private emptyMessage(name: string, owner: Operation, pkg: string): string {
    const file = this.fileFor(owner, pkg);
    if (!file.messages?.some((m) => m.name === name)) {
      (file.messages ??= []).push(this.place({ name, ...this.position(owner) }, owner));
    }
    return this.qualify(pkg, name);
  }

  private doc(type: Type): string | undefined {
    return getDoc(this.program, type) || undefined;
  }
}

// ------------------------------------------------------------------ helpers

/** Byte encodings TypeSpec's `@encode` may name that onkt understands. */
const BYTES_ENCODINGS = new Set(["base64url"]);

/** TypeSpec standard scalars and the onkt scalar each one maps to. */
const SCALARS: Record<string, string> = {
  string: "string",
  url: "string",
  boolean: "bool",
  int8: "int32",
  int16: "int32",
  int32: "int32",
  int64: "int64",
  safeint: "int64",
  integer: "int64",
  uint8: "uint32",
  uint16: "uint32",
  uint32: "uint32",
  uint64: "uint64",
  float32: "float32",
  float64: "float64",
  float: "float64",
  numeric: "float64",
  decimal: "float64",
  decimal128: "float64",
  bytes: "bytes",
  utcDateTime: "timestamp",
  offsetDateTime: "timestamp",
  plainDate: "timestamp",
  plainTime: "string",
  duration: "string",
};

/** The standard-library scalar a scalar derives from. */
function stdScalar(type: Type): string | undefined {
  let scalar: Scalar | undefined = type.kind === "Scalar" ? type : undefined;
  while (scalar) {
    if (scalar.namespace?.name === "TypeSpec" && SCALARS[scalar.name] !== undefined) return scalar.name;
    scalar = scalar.baseScalar;
  }
  return undefined;
}

/** A model's properties including inherited ones, base properties first. */
function allProperties(model: Model): ModelProperty[] {
  const chain: Model[] = [];
  for (let m: Model | undefined = model; m; m = m.baseModel) chain.unshift(m);
  return chain.flatMap((m) => [...m.properties.values()]);
}

/** Spells RFC 6570 reserved expansions (`{+path}`) as onkt wildcards (`{path...}`). */
function wildcards(route: string): string {
  return route.replace(/\{\+([A-Za-z0-9_]+)\}/g, "{$1...}");
}

/** A decorator's numeric argument, spelled exactly (64-bit bounds included). */
function numericArg(value: unknown): OnktArg {
  if (value !== null && typeof value === "object" && "asBigInt" in value) return { value: String(value) };
  return { value: String(value) };
}

function literalArg(type: Type): OnktArg {
  if (type.kind === "String") return str(type.value);
  if (type.kind === "Number") return num(type.value);
  return str(String(type.kind));
}

function typeDisplayName(type: Type | { entityKind?: string }): string {
  const t = type as Type;
  if ("name" in t && typeof t.name === "string") return pascal(t.name);
  return "";
}

function variantKeyName(type: Type): string {
  if ("name" in type && typeof type.name === "string" && type.name !== "") return snake(type.name);
  return "value";
}

function pascal(name: string): string {
  return name
    .split(/[_\-\s]+/)
    .filter(Boolean)
    .map((part) => part[0].toUpperCase() + part.slice(1))
    .join("");
}

function snake(name: string): string {
  return name.replace(/([a-z0-9])([A-Z])/g, "$1_$2").toLowerCase();
}
