package gents

import "testing"

const tsNullableSchema = `package app

message Item { name: string }
message Patch {
  id: int32
  folder_id: int64? @nullable @encode("number")
  note: string? @nullable
  item: Item? @nullable
}

service Patches {
  patch(Patch) -> Patch @patch("/patch")
}
`

func TestTSNullableFieldsKeepAbsentNullAndValueApart(t *testing.T) {
	runTSSchema(t, tsNullableSchema, `
import { encodePatch, decodePatch } from "./types.ts";
import type { Patch } from "./types.ts";
import { createPatchesFetchHandler } from "./server.ts";
import { PatchesClient } from "./client.ts";

const wire = (p: Patch) => JSON.stringify(encodePatch(p));
if (wire({ id: 1 }) !== '{"id":1}') throw new Error("absent must be omitted: " + wire({ id: 1 }));
if (wire({ id: 1, folderId: null, note: null, item: null }) !== '{"id":1,"folder_id":null,"note":null,"item":null}') throw new Error("null must be sent: " + wire({ id: 1, folderId: null, note: null, item: null }));
if (wire({ id: 1, folderId: 7, note: "n", item: { name: "x" } }) !== '{"id":1,"folder_id":7,"note":"n","item":{"name":"x"}}') throw new Error("values");

const decoded = decodePatch({ id: 1, folder_id: null, note: "x" });
if (decoded.folderId !== null || decoded.note !== "x" || decoded.item !== undefined) throw new Error("decode tri-state: " + JSON.stringify(decoded));

const handler = createPatchesFetchHandler({ async patch(req) { return req; } });
const client = new PatchesClient("http://x", { fetch: (input, init) => handler(new Request(input, init)) });
const got = await client.patch({ id: 2, folderId: null });
if (got.folderId !== null || got.note !== undefined) throw new Error("round trip: " + JSON.stringify(got));
console.log("OK");
`)
}
