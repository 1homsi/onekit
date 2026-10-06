export class HttpError extends Error {
status: number;
body: unknown;
code?: string;
field?: string;
violations?: readonly string[];
constructor(status: number, body: unknown, info?: { code?: string; field?: string; violations?: readonly string[] }) {
super(`http error ${status}`);
this.status = status;
this.body = body;
if (info?.code !== undefined) this.code = info.code;
if (info?.field !== undefined) this.field = info.field;
if (info?.violations !== undefined) this.violations = info.violations;
}
}

function requestError(status: number, code: string, message: string, extra?: { field?: string; violations?: readonly string[] }): HttpError {
const body: { message: string; violations?: readonly string[] } = { message };
if (extra?.violations !== undefined) body.violations = extra.violations;
return new HttpError(status, body, { code, ...extra });
}

export interface ServerErrorInfo {
status: number;
code: string;
message: string;
field?: string;
violations?: readonly string[];
cause?: unknown;
}

const errorInfos = new WeakMap<Response, ServerErrorInfo>();

function registeredError(status: number, code: string, message: string, cause?: unknown): Response {
const response = jsonResponse({ message }, status);
errorInfos.set(response, { status, code, message, ...(cause !== undefined ? { cause } : {}) });
return response;
}

