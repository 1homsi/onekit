package gents

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const tsWireNamesSchema = `namespace app;

enum Level {
  LOW,
  HIGH,
}

model Inner {
  @minLength(1)
  @maxLength(5)
  v: string;
}

@Onkt.discriminator("kind")
union EntryChoice {
  @Onkt.tag("a") alpha_one: Inner,
  @Onkt.tag("b") beta: Level,
}

@Onkt.rule("self.is_default || self.rev_no > 0", "a non-default record needs a revision")
model Entry {
  @minLength(1)
  @maxLength(10)
  folder_id: string;
  is_default: boolean;
  rev_no: int32;
  created_at?: utcDateTime;
  note?: string | null;
  tags: string[];
  by_name: Record<Inner>;
  inner?: Inner;
  choice: EntryChoice;
}

model Lookup {
  folder_id: string;
  @query("rev_no")
  rev_no: int32;
}

@route("/v1")
interface Entries {
  @get
  @route("/f/{folder_id}")
  get(...Lookup): Entry;

  @post
  @route("/f")
  save(...Entry): Entry;
}
`

func TestTSWireFieldNamesKeepSnakeCaseInTypesClientAndServer(t *testing.T) {
	runTSSchemaWithOptions(t, tsWireNamesSchema, Options{WireFieldNames: true}, `
import { encodeEntry, decodeEntry, validateEntry } from "./types.ts";
import type { Entry } from "./types.ts";
import { createEntriesFetchHandler } from "./server.ts";
import { EntriesClient } from "./client.ts";

const wire = {
  folder_id: "f1", is_default: true, rev_no: 3, note: null,
  tags: ["a"], by_name: { x: { v: "ab" } }, inner: { v: "cd" },
  choice: { kind: "a", alpha_one: { v: "ef" } },
};
const decoded: Entry = decodeEntry(wire);
if (decoded.folder_id !== "f1" || decoded.is_default !== true || decoded.rev_no !== 3 || decoded.note !== null) throw new Error("decode keeps wire names: " + JSON.stringify(decoded));
if (decoded.by_name?.x?.v !== "ab" || decoded.inner?.v !== "cd") throw new Error("nested: " + JSON.stringify(decoded));
if (decoded.choice?.kind !== "a" || (decoded.choice as any).alpha_one?.v !== "ef") throw new Error("oneof variant name: " + JSON.stringify(decoded.choice));
if (JSON.stringify(encodeEntry(decoded)) !== JSON.stringify(wire)) throw new Error("encode round trip: " + JSON.stringify(encodeEntry(decoded)));

if (validateEntry({ ...decoded, is_default: false, rev_no: 0 }).length === 0) throw new Error("the rule must see wire-named fields");
if (validateEntry(decoded).length !== 0) throw new Error("valid record rejected: " + validateEntry(decoded).join(","));
if (validateEntry({ ...decoded, folder_id: "" }).some((m) => !m.includes("folder_id"))) throw new Error("violations name the wire field");

const seen: string[] = [];
const handler = createEntriesFetchHandler({
  async get(req) { seen.push("get:" + req.folder_id + ":" + req.rev_no); return decoded; },
  async save(req) { seen.push("save:" + req.folder_id + ":" + req.is_default); return req; },
});
const client = new EntriesClient("http://x", { fetch: (input, init) => handler(new Request(input, init)) });
const got = await client.get({ folder_id: "a b", rev_no: 7 });
if (got.folder_id !== "f1" || got.is_default !== true) throw new Error("client response: " + JSON.stringify(got));
const saved = await client.save(decoded);
if (saved.folder_id !== "f1" || saved.rev_no !== 3) throw new Error("save round trip: " + JSON.stringify(saved));
if (seen.join("|") !== "get:a b:7|save:f1:true") throw new Error("server saw: " + seen.join("|"));
console.log("OK");
`)
}

func TestTSDefaultFieldNamesStayCamelCase(t *testing.T) {
	runTSSchema(t, tsWireNamesSchema, `
import { decodeEntry } from "./types.ts";
const r = decodeEntry({ folder_id: "f1", is_default: true, rev_no: 3 });
if (r.folderId !== "f1" || r.isDefault !== true || r.revNo !== 3) throw new Error(JSON.stringify(r));
console.log("OK");
`)
}

func TestTSWireFieldNamesTypeCheckUnderStrict(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	file := compileTSSchema(t, tsWireNamesSchema+`
interface Feed {
  @get
  @route("/watch/{folder_id}")
  @Onkt.stream
  watch(...Lookup): Entry;

  @post
  @route("/push")
  @Onkt.stream
  push(...Entry): Entry;
}
`)
	for name, opts := range map[string]Options{"wire": {WireFieldNames: true}, "camel": {}} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypesWithOptions(file, nil, opts)))
			writeFile(t, filepath.Join(dir, "client.ts"), string(GenerateClientWithOptions(file, nil, opts)))
			writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServerWithOptions(file, nil, opts)))
			writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {"target": "ES2022", "module": "ES2022", "moduleResolution": "bundler", "strict": true, "noEmit": true, "lib": ["ES2022", "DOM"]}
}
`)
			cmd := exec.Command("tsc", "-p", "tsconfig.json")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("tsc failed: %v\n%s", err, out)
			}
		})
	}
}
