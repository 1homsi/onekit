package gents

import "testing"

func TestTSMaxBodyDecoratorSetsAPerMethodLimit(t *testing.T) {
	runTSSchema(t, `namespace app;

model Blob {
  data: string;
}

model Done {
  ok: boolean;
}

interface Uploads {
  @post
  @route("/small")
  small(...Blob): Done;

  @post
  @route("/big")
  @Onkt.maxBody("20MiB")
  big(...Blob): Done;
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
