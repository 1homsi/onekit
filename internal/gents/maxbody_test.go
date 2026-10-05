package gents

import "testing"

func TestTSMaxBodyDecoratorSetsAPerMethodLimit(t *testing.T) {
	runTSSchema(t, `package app

message Blob { data: string }
message Done { ok: bool }

service Uploads {
  small(Blob) -> Done @post("/small")
  big(Blob) -> Done @post("/big") @max_body("20MiB")
}
`, `
import { createUploadsFetchHandler } from "./server.ts";

const handler = createUploadsFetchHandler({
  async small() { return { ok: true }; },
  async big() { return { ok: true }; },
});
const post = async (path: string, size: number) => {
  const res = await handler(new Request("http://x" + path, { method: "POST", body: JSON.stringify({ data: "a".repeat(size) }), headers: { "content-type": "application/json" } }));
  return res.status;
};
const cases: [string, number, number][] = [["/small", 100, 200], ["/small", 9 * 1024 * 1024, 413], ["/big", 9 * 1024 * 1024, 200], ["/big", 21 * 1024 * 1024, 413]];
for (const [path, size, want] of cases) {
  const got = await post(path, size);
  if (got !== want) throw new Error(path + " " + size + ": " + got + " want " + want);
}
console.log("OK");
`)
}
