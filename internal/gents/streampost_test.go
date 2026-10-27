package gents

import "testing"

const tsStreamPostSchema = `namespace app;

model TurnRequest {
  prompt: string;
}

model Text {
  text: string;
}

model Done {
  reason: string;
}

@Onkt.discriminator("type")
union TurnEventPayload {
  @Onkt.tag("text") text: Text,
  @Onkt.tag("done") done: Done,
}

model TurnEvent {
  payload: TurnEventPayload;
}

interface Agent {
  @post
  @route("/turn")
  @Onkt.stream
  turn(...TurnRequest): TurnEvent;
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
        await new Promise((r) => setTimeout(r, 1500));
        c.enqueue({ payload: { type: "text", text: { text: "hi " + req.prompt } } });
        c.enqueue({ payload: { type: "done", done: { reason: "stop" } } });
        c.close();
      },
    });
  },
}, { sseHeartbeatMs: 50 });

const start = Date.now();
const res = await handler(new Request("http://x/turn", { method: "POST", body: JSON.stringify({ prompt: "bob" }), headers: { "content-type": "application/json" } }));
if (Date.now() - start > 1000) throw new Error("heartbeat must commit headers before the first slow event");
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
