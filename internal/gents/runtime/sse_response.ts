const defaultSSEHeartbeatMs = 15000;

async function sseResponse<T>(req: Request, stream: ReadableStream<T>, encode: (v: T) => unknown, eventName?: (encoded: any) => string | undefined): Promise<Response> {
const reader = stream.getReader();
const interval = sseHeartbeats.get(req) ?? defaultSSEHeartbeatMs;
const firstRead = reader.read();
let first: ReadableStreamReadResult<T> | undefined;
try {
if (interval > 0) {
let timer: ReturnType<typeof setTimeout> | undefined;
const timeout = new Promise<"timeout">((resolve) => { timer = setTimeout(() => resolve("timeout"), interval); });
const raced = await Promise.race([firstRead, timeout]);
clearTimeout(timer);
if (raced !== "timeout") first = raced;
} else {
first = await firstRead;
}
} catch (err) {
return errorResponse(err);
}

const encoder = new TextEncoder();
let ping: ReturnType<typeof setInterval> | undefined;
const body = new ReadableStream<Uint8Array>({
async start(controller) {
const sendPing = () => { try { controller.enqueue(encoder.encode(": ping\n\n")); } catch { } };
if (interval > 0) ping = setInterval(sendPing, interval);
if (first === undefined) sendPing();
try {
let current = first ?? (await firstRead);
while (!current.done) {
const encoded = encode(current.value);
const raw = eventName?.(encoded);
const name = typeof raw === "string" && /^[A-Za-z0-9_.-]+$/.test(raw) ? raw : undefined;
controller.enqueue(encoder.encode((name ? "event: " + name + "\n" : "") + "data: " + JSON.stringify(encoded) + "\n\n"));
current = await reader.read();
}
} catch (err) {
controller.enqueue(encoder.encode("event: error\ndata: " + JSON.stringify(await midStreamErrorBody(req, err)) + "\n\n"));
} finally {
if (ping !== undefined) clearInterval(ping);
try { controller.close(); } catch { }
}
},
cancel() {
if (ping !== undefined) clearInterval(ping);
return reader.cancel();
},
});

return new Response(body, {
status: 200,
headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache", "Connection": "keep-alive" },
});
}

async function midStreamErrorBody(req: Request, err: unknown): Promise<unknown> {
const response = errorResponse(err);
const info = errorInfos.get(response);
const write = sseErrorWriters.get(req);
if (info && write) {
try {
const rendered = await write(info, req);
return JSON.parse(await rendered.text());
} catch {
}
}
return await response.json();
}

