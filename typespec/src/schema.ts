// The onkt schema document, version 1: the JSON contract between a frontend
// and the onkt compiler. It mirrors internal/onkschema in the Go module;
// change both together.

export interface Document {
  version: 1;
  files: SourceFile[];
}

export interface SourceFile {
  /** The frontend source the declarations came from; its directory decides the generated package. */
  path: string;
  package: string;
  doc?: string;
  imports?: string[];
  messages?: MessageDecl[];
  enums?: EnumDecl[];
  services?: ServiceDecl[];
}

export interface Arg {
  name?: string;
  value: string;
  quoted?: boolean;
}

export interface Decorator {
  name: string;
  args?: Arg[];
  line?: number;
  col?: number;
}

/** One-based line and column; the end is exclusive. */
export interface Span {
  line?: number;
  col?: number;
  endLine?: number;
  endCol?: number;
}

export interface TypeRef {
  span: Span;
  /** A scalar (string, bool, int32, ...) or a message or enum name, qualified as `package.Name`. */
  name?: string;
  map?: boolean;
  mapKey?: string;
  mapValue?: TypeRef;
}

export interface FieldDecl {
  name: string;
  doc?: string;
  type?: TypeRef;
  repeated?: boolean;
  optional?: boolean;
  decorators?: Decorator[];
  oneof?: OneofDecl;
  line?: number;
  col?: number;
}

export interface OneofVariant {
  name: string;
  type: TypeRef;
  decorators?: Decorator[];
  line?: number;
  col?: number;
}

export interface OneofDecl {
  args?: Arg[];
  variants: OneofVariant[];
  line?: number;
  col?: number;
}

export interface MessageDecl {
  name: string;
  doc?: string;
  decorators?: Decorator[];
  fields?: FieldDecl[];
  messages?: MessageDecl[];
  enums?: EnumDecl[];
  line?: number;
  col?: number;
}

export interface EnumValueDecl {
  name: string;
  doc?: string;
  decorators?: Decorator[];
  line?: number;
  col?: number;
}

export interface EnumDecl {
  name: string;
  doc?: string;
  values: EnumValueDecl[];
  line?: number;
  col?: number;
}

export interface HeaderDecl {
  name: string;
  type: string;
  decorators?: Decorator[];
  line?: number;
  col?: number;
}

export interface RPCDecl {
  name: string;
  doc?: string;
  requestSpan: Span;
  responseSpan: Span;
  errorSpans?: Span[];
  request: string;
  response: string;
  errors?: string[];
  decorators?: Decorator[];
  headers?: HeaderDecl[];
  line?: number;
  col?: number;
}

export interface ServiceDecl {
  name: string;
  doc?: string;
  basePath?: string;
  headers?: HeaderDecl[];
  rpcs?: RPCDecl[];
  line?: number;
  col?: number;
}
