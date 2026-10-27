package gents

import "testing"

func TestTSSSEClientFramingAndEarlyBreak(t *testing.T) {
	runTSSchema(t, `namespace app;

model Req {
  id: string;
}

model Tick {
  n: int32;
  text: string;
}

interface Feed {
  @get
  @route("/feed/{id}")
  @Onkt.stream
  watch(...Req): Tick;
}
`, `
import { FeedClient } from "./client.ts";

let cancelled = false;
const body = ': keepalive\n\nevent: tick\n\ndata: {"n":1,\ndata: "text":"a"}\r\n\r\nid: 7\ndata: {"n":2}\n\ndata: {"n":3}\n\n';
const client = new FeedClient("http://api", {
  fetch: (async () => new Response(new ReadableStream({
    start(controller) { controller.enqueue(new TextEncoder().encode(body)); },
    cancel() { cancelled = true; },
  }), { status: 200, headers: { "Content-Type": "text/event-stream" } })) as typeof fetch,
});
const seen: Array<[number, string]> = [];
for await (const tick of client.watch({ id: "1" })) {
  seen.push([tick.n, tick.text ?? ""]);
  if (seen.length === 2) break;
}
if (JSON.stringify(seen) !== JSON.stringify([[1, "a"], [2, ""]])) throw new Error("framing: " + JSON.stringify(seen));
if (!cancelled) throw new Error("stream not cancelled after break");
console.log("OK");
`)
}
