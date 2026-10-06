export interface RouteDescriptor {
method: string;
path: string;
scopes?: readonly string[];
meta?: Readonly<Record<string, string>>;
authorize?: boolean;
handler: (req: Request) => Promise<Response>;
}

function matchPath(pattern: string, pathname: string): Record<string, string> | null {
const patternParts = pattern.split("/").filter((s) => s.length > 0);
const pathParts = pathname.split("/").filter((s) => s.length > 0);
const last = patternParts[patternParts.length - 1] ?? "";
const wildcard = last.startsWith("{") && last.endsWith("...}");
if (wildcard ? pathParts.length < patternParts.length - 1 : patternParts.length !== pathParts.length) return null;
if (wildcard && pathParts.length === patternParts.length - 1 && !pathname.endsWith("/")) return null;
const params: Record<string, string> = {};
for (let i = 0; i < patternParts.length; i++) {
const part = patternParts[i] ?? "";
if (wildcard && i === patternParts.length - 1) {
params[part.slice(1, -4)] = pathParts.slice(i).map(decodeURIComponent).join("/");
break;
}
const actual = pathParts[i] ?? "";
if (part.startsWith("{") && part.endsWith("}")) {
params[part.slice(1, -1)] = decodeURIComponent(actual);
} else if (part !== actual) {
return null;
}
}
return params;
}

function wildcardLast(routes: RouteDescriptor[]): RouteDescriptor[] {
const isWildcard = (route: RouteDescriptor) => route.path.endsWith("...}");
return [...routes].sort((a, b) => Number(isWildcard(a)) - Number(isWildcard(b)));
}

function jsonResponse(body: unknown, status = 200): Response {
return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const maxRequestBodyBytes = 8 * 1024 * 1024;
async function readJSONBody(req: Request, limit: number = maxRequestBodyBytes): Promise<unknown> {
const declaredLength = req.headers.get("content-length");
if (declaredLength !== null && Number.isFinite(Number(declaredLength)) && Number(declaredLength) > limit) throw requestError(413, "request_body_too_large", "request body too large");
if (!req.body) return undefined;
const reader = req.body.getReader();
const chunks: Uint8Array[] = []; let total = 0;
while (true) {
const { done, value } = await reader.read();
if (done) break;
if (value) { total += value.byteLength; if (total > limit) { await reader.cancel(); throw requestError(413, "request_body_too_large", "request body too large"); } chunks.push(value); }
}
const bytes = new Uint8Array(total); let offset = 0;
for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
const text = new TextDecoder().decode(bytes);
if (text.trim() === "") return undefined;
try { return JSON.parse(text); } catch { throw requestError(400, "invalid_request_body", "invalid request body"); }
}

function errorResponse(err: unknown): Response {
if (err instanceof HttpError) {
const response = jsonResponse(err.body, err.status);
if (err.code !== undefined) errorInfos.set(response, { status: err.status, code: err.code, message: (err.body as { message?: string } | null)?.message ?? "", ...(err.field !== undefined ? { field: err.field } : {}), ...(err.violations !== undefined ? { violations: err.violations } : {}), cause: err });
return response;
}
return registeredError(500, "internal", "internal server error", err);
}
