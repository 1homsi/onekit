package gents

import "testing"

func TestTSMidStreamErrorsUseTheConfiguredErrorWriter(t *testing.T) {
	runTSSchema(t, `package app

message Req { id: string }
message Tick { n: int32 }

service Feed {
  watch(Req) -> Tick @get("/watch") @stream
}
`, `
import { createFeedFetchHandler, HttpError } from "./server.ts";

function failing(error: unknown) {
  return {
    watch() {
      return new ReadableStream({
        async start(c) {
          c.enqueue({ n: 1 });
          await new Promise((resolve) => setTimeout(resolve, 30));
          c.error(error);
        },
      });
    },
  };
}

const envelope = (info: { status: number; code: string; message: string }) =>
  new Response(JSON.stringify({ error: { code: info.code, message: info.message } }), { status: info.status, headers: { "content-type": "application/json" } });

const read = async (error: unknown, onError?: typeof envelope) => {
  const handler = createFeedFetchHandler(failing(error) as never, onError ? { onError } : undefined);
  return await (await handler(new Request("http://x/watch"))).text();
};

const coded = new HttpError(429, { message: "daily quota reached" }, { code: "quota_exceeded", message: "daily quota reached" });
let got = await read(coded, envelope);
if (!got.includes('event: error\ndata: {"error":{"code":"quota_exceeded","message":"daily quota reached"}}')) throw new Error("custom envelope: " + got);

got = await read(new Error("db password is hunter2"), envelope);
if (got.includes("hunter2") || !got.includes('"code":"internal","message":"internal server error"')) throw new Error("unexpected errors stay generic: " + got);

const declared = new HttpError(404, { code: "gone" });
got = await read(declared, envelope);
if (!got.includes('event: error\ndata: {"code":"gone"}') || got.includes('"error":{')) throw new Error("a declared typed error keeps its payload even with an error writer: " + got);

got = await read(coded);
if (!got.includes('event: error\ndata: {"message":"daily quota reached"}')) throw new Error("default body: " + got);
console.log("OK");
`)
}
