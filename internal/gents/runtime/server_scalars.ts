function parseScalar(value: string, kind: string, name: string): string | number | boolean {
const bad = (detail = ""): HttpError => requestError(400, name.startsWith("path parameter") ? "invalid_path_parameter" : "invalid_query_parameter", "invalid " + name + detail, { field: name.slice(name.lastIndexOf(" ") + 1) });
if (kind === "string" || kind === "bytes" || kind === "timestamp") return value;
if (kind === "bool") {
if (value === "true") return true;
if (value === "false") return false;
throw bad(": must be true or false");
}
if (kind === "int64" || kind === "uint64") {
if (!/^-?(0|[1-9][0-9]*)$/.test(value) || (kind === "uint64" && value.startsWith("-"))) throw bad();
try { const parsed = BigInt(value); const min = kind === "int64" ? -(2n ** 63n) : 0n; const max = kind === "int64" ? (2n ** 63n) - 1n : (2n ** 64n) - 1n; if (parsed < min || parsed > max) throw new Error(); } catch { throw bad(); }
return value;
}
const parsed = Number(value);
if (!Number.isFinite(parsed) || ((kind.startsWith("int") || kind.startsWith("uint")) && !Number.isInteger(parsed))) {
throw bad();
}
if (kind === "int32" && (parsed < -2147483648 || parsed > 2147483647)) throw bad();
if (kind === "uint32" && (parsed < 0 || parsed > 4294967295)) throw bad();
if (kind.startsWith("uint") && parsed < 0) throw bad();
return parsed;
}

function validHeaderFormat(value: string, format: string): boolean {
if (format === "uuid") return /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(value);
if (format === "email") return /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(value);
if (format === "uri") { try { new URL(value); return true; } catch { return false; } }
return true;
}

