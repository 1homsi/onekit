package gents

import "testing"

func TestTSServerBodyParsing(t *testing.T) {
	runTSSchema(t, `
package app
message Note { text: string }
service Notes { create(Note) -> Note @post("/notes") }
`, `
import { createNotesRoutes } from "./server.ts";

const [route] = createNotesRoutes({ createNote: async (req) => req, create: async (req) => req } as any);
const post = (body?: string, headers: Record<string, string> = {}) => route.handler(new Request("http://x/notes", { method: "POST", body, headers }));

const empty = await post();
if (empty.status !== 200) throw new Error("empty body rejected: " + empty.status);
const blank = await post("");
if (blank.status !== 200) throw new Error("blank body rejected: " + blank.status);
const broken = await post("{nope");
if (broken.status !== 400) throw new Error("malformed JSON not 400: " + broken.status);
const huge = await post("x", { "content-length": String(64 * 1024 * 1024) });
if (huge.status !== 413) throw new Error("oversized body not 413: " + huge.status);
console.log("OK");
`)
}
