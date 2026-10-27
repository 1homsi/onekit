import { createTypeSpecLibrary, paramMessage, type JSONSchemaType } from "@typespec/compiler";

export interface OnktEmitterOptions {
  /** File name of the schema document, relative to the emitter output directory. */
  "output-file"?: string;
}

const EmitterOptionsSchema: JSONSchemaType<OnktEmitterOptions> = {
  type: "object",
  additionalProperties: false,
  properties: {
    "output-file": { type: "string", nullable: true },
  },
  required: [],
};

export const $lib = createTypeSpecLibrary({
  name: "@onkt/typespec",
  diagnostics: {
    unsupported: {
      severity: "error",
      messages: {
        default: paramMessage`onkt does not support ${"what"} yet.`,
      },
    },
    "request-shape": {
      severity: "error",
      messages: {
        default: paramMessage`${"message"}`,
      },
    },
  },
  emitter: {
    options: EmitterOptionsSchema,
  },
  state: {
    onktDecorators: { description: "onkt decorators applied to a type, in source order" },
    onktParent: { description: "the model a nested model or enum is declared inside" },
    onktName: { description: "the service name an interface generates under" },
  },
} as const);

export const { reportDiagnostic, createDiagnostic, stateKeys: StateKeys } = $lib;
