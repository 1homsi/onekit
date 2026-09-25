package gents

import "testing"

func TestTSSocketIteratesUntilNormalClose(t *testing.T) {
	runTSSchema(t, `
package app
message Msg { text: string }
service Chat { chat(Msg) -> Msg @ws("/chat") }
`, `
import { ChatSocket } from "./client.ts";

const listeners: Record<string, Array<(event: any) => void>> = {};
let closedWith: [number, string | undefined] | undefined;
const fake = {
  readyState: 1,
  bufferedAmount: 0,
  binaryType: "arraybuffer",
  addEventListener(type: string, fn: (event: any) => void) { (listeners[type] ??= []).push(fn); },
  send() {},
  close(code: number, reason?: string) { closedWith = [code, reason]; },
};
const socket = new ChatSocket(fake as any);
const emit = (type: string, event: any) => (listeners[type] ?? []).forEach((fn) => fn(event));
setTimeout(() => {
  emit("message", { data: JSON.stringify({ text: "a" }) });
  emit("message", { data: JSON.stringify({ text: "b" }) });
  emit("close", { code: 1000, reason: "" });
}, 10);
const seen: string[] = [];
for await (const msg of socket) seen.push(msg.text);
if (seen.join(",") !== "a,b") throw new Error("iteration: " + seen.join(","));
socket.close(4000, "bye");
if (!closedWith || closedWith[0] !== 4000 || closedWith[1] !== "bye") throw new Error("close code not passed");
console.log("OK");
`)
}
