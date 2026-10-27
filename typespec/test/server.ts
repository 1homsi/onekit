// A long-running TypeSpec compiler for onekit's Go tests: each request is
// one JSON line holding a project's .tsp files, each response one JSON line
// holding the onkt schema document (paths relative to the project) or the
// compiler's diagnostics. Loading TypeSpec once makes a compile take
// milliseconds instead of the second a `tsp compile` process costs.
//
// Files are written under a scratch directory inside this package, so
// `@typespec/http` and `@onkt/typespec` resolve from the workspace whatever
// directory the test project itself lives in.

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { createInterface } from "node:readline";
import { NodeHost, compile, getSourceLocation } from "@typespec/compiler";
import { buildDocument } from "../src/emitter.js";

interface Request {
  id: number;
  /** Project-relative path (forward slashes) to source text. */
  files: Record<string, string>;
}

interface Response {
  id: number;
  document?: unknown;
  diagnostics?: string[];
  error?: string;
}

const scratch = join(import.meta.dirname, "..", "..", ".test-scratch", "server");
mkdirSync(scratch, { recursive: true });

/** The entry a project compiles from: its main.tsp, or one importing every file. */
function entryFor(dir: string, files: string[]): string {
  if (files.includes("main.tsp")) return join(dir, "main.tsp");
  const entry = join(dir, ".onkt-entry.tsp");
  writeFileSync(entry, files.map((f) => `import "./${f}";\n`).join(""));
  return entry;
}

async function handle(request: Request): Promise<Response> {
  const dir = mkdtempSync(join(scratch, "p-"));
  try {
    const names = Object.keys(request.files).sort();
    for (const name of names) {
      const path = join(dir, ...name.split("/"));
      mkdirSync(dirname(path), { recursive: true });
      writeFileSync(path, request.files[name]);
    }
    const program = await compile(NodeHost, entryFor(dir, names), { noEmit: true });
    const rel = (path: string) => relative(dir, path).split(sep).join("/");
    const errors = program.diagnostics.filter((d) => d.severity === "error");
    if (errors.length > 0) {
      return {
        id: request.id,
        diagnostics: errors.map((d) => {
          const location = getSourceLocation(d.target);
          if (!location) return `- error ${d.code}: ${d.message}`;
          const { line, character } = location.file.getLineAndCharacterOfPosition(location.pos);
          return `${rel(location.file.path)}:${line + 1}:${character + 1} - error ${d.code}: ${d.message}`;
        }),
      };
    }
    const document = buildDocument(program);
    for (const file of document.files) {
      const [path, suffix] = file.path.split("#");
      file.path = rel(path) + (suffix === undefined ? "" : `#${suffix}`);
    }
    return { id: request.id, document };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

const lines = createInterface({ input: process.stdin });
for await (const line of lines) {
  if (line.trim() === "") continue;
  let response: Response;
  try {
    response = await handle(JSON.parse(line) as Request);
  } catch (err) {
    response = { id: (JSON.parse(line) as Request).id ?? 0, error: String((err as Error)?.stack ?? err) };
  }
  process.stdout.write(JSON.stringify(response) + "\n");
}
