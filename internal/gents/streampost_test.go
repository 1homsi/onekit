package gents

import "testing"

const tsStreamPostSchema = `package app

message TurnRequest { prompt: string }
message Text { text: string }
message Done { reason: string }
message TurnEvent {
  payload: oneof(discriminator: "type") {
    text: Text @tag("text")
    done: Done @tag("done")
  }
}

service Agent {
  turn(TurnRequest) -> TurnEvent @post("/turn") @stream
}
`

func TestTSPostStreamWithTypedEventsAndHeartbeat(t *testing.T) {
	runTSSchema(t, tsStreamPostSchema, `
import { createAgentFetchHandler } from "./server.ts";
import { AgentClient } from "./client.ts";

const handler = createAgentFetchHandler({
  turn(req) {
    return new ReadableStream({
      async start(c) {
        await new Promise((r) => setTimeout(r, 300));
        c.enqueue({ payload: { type: "text", text: { text: "hi " + req.prompt } } });
        c.enqueue({ payload: { type: "done", done: { reason: "stop" } } });
        c.close();
      },
    });
  },
}, { sseHeartbeatMs: 50 });

const start = Date.now();
const res = await handler(new Request("http://x/turn", { method: "POST", body: JSON.stringify({ prompt: "bob" }), headers: { "content-type": "application/json" } }));
if (Date.now() - start > 250) throw new Error("heartbeat must commit headers before the first slow event");
const text = await res.text();
const lines = text.split("\n").filter((l) => l !== "" && !l.startsWith(":"));
const want = ['event: text', 'data: {"payload":{"type":"text","text":{"text":"hi bob"}}}', 'event: done', 'data: {"payload":{"type":"done","done":{"reason":"stop"}}}'];
if (JSON.stringify(lines) !== JSON.stringify(want)) throw new Error("wire: " + JSON.stringify(lines));

const client = new AgentClient("http://x", { fetch: (input, init) => handler(new Request(input, init)) });
const seen: string[] = [];
for await (const event of client.turn({ prompt: "amy" })) seen.push(event.payload.type);
if (seen.join(",") !== "text,done") throw new Error("client: " + seen.join(","));
console.log("OK");
`)
}
