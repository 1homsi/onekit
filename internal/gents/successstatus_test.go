package gents

import "testing"

func TestTSServerAnswersTheDeclaredSuccessStatus(t *testing.T) {
	runTSSchema(t, `
package app
message Item { id: string }
message Empty {}
service Items {
  create(Item) -> Item @post("/items") @success(201)
  remove(Item) -> Empty @delete("/items/{id}") @success(204)
  get(Item) -> Item @get("/items/{id}")
}
`, `
import { createItemsFetchHandler } from "./server.ts";

const check = (ok: boolean, message: string) => { if (!ok) throw new Error(message); };
const handle = createItemsFetchHandler({ create: async (i: any) => i, remove: async () => ({}), get: async (i: any) => i } as any);
const created = await handle(new Request("http://x/items", { method: "POST", body: JSON.stringify({ id: "a" }), headers: { "Content-Type": "application/json" } }));
check(created.status === 201 && (await created.json()).id === "a", "@success(201): " + created.status);
const removed = await handle(new Request("http://x/items/a", { method: "DELETE" }));
check(removed.status === 204 && (await removed.text()) === "", "@success(204) sends no body: " + removed.status);
const fetched = await handle(new Request("http://x/items/a"));
check(fetched.status === 200, "the default stays 200: " + fetched.status);
console.log("OK");
`)
}
