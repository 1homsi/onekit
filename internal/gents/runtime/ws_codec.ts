const wsTextEncoder = new TextEncoder();
const wsTextDecoder = new TextDecoder();

function wsMalformedFrame(): Error {
	return new Error("malformed binary frame");
}

function wsBytes(data: unknown): Uint8Array {
	if (data instanceof Uint8Array) return data;
	if (data instanceof ArrayBuffer) return new Uint8Array(data);
	if (ArrayBuffer.isView(data))
		return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
	throw wsMalformedFrame();
}

export function wsEncodeRawFrame(
	header: string,
	raw: (Uint8Array | string)[],
): ArrayBuffer {
	const head = wsTextEncoder.encode(header);
	let size = 0;
	for (const segment of raw)
		size +=
			typeof segment === "string" ? segment.length : segment.byteLength;
	const out = new Uint8Array(8 + head.byteLength + 4 * raw.length + size);
	const view = new DataView(out.buffer);
	view.setUint32(0, head.byteLength);
	out.set(head, 4);
	let offset = 4 + head.byteLength;
	view.setUint32(offset, raw.length);
	offset += 4;
	let data = offset + 4 * raw.length;
	for (const segment of raw) {
		if (typeof segment === "string") {
			const written = wsTextEncoder.encodeInto(
				segment,
				out.subarray(data, data + segment.length),
			);
			if (written.read !== segment.length)
				return wsEncodeRawFrame(
					header,
					raw.map((s) =>
						typeof s === "string" ? wsTextEncoder.encode(s) : s,
					),
				);
			view.setUint32(offset, written.written);
			data += written.written;
		} else {
			view.setUint32(offset, segment.byteLength);
			out.set(segment, data);
			data += segment.byteLength;
		}
		offset += 4;
	}
	return out.buffer;
}

export function wsDecodeRawFrame(data: Uint8Array): {
	header: string;
	raw: Uint8Array[];
} {
	const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
	if (data.byteLength < 4) throw wsMalformedFrame();
	const headerLen = view.getUint32(0);
	let offset = 4;
	if (offset + headerLen + 4 > data.byteLength) throw wsMalformedFrame();
	const header = wsTextDecoder.decode(
		data.subarray(offset, offset + headerLen),
	);
	offset += headerLen;
	const count = view.getUint32(offset);
	offset += 4;
	if (offset + count * 4 > data.byteLength) throw wsMalformedFrame();
	const lengths: number[] = [];
	for (let i = 0; i < count; i++) {
		lengths.push(view.getUint32(offset));
		offset += 4;
	}
	const raw: Uint8Array[] = [];
	for (const n of lengths) {
		if (offset + n > data.byteLength) throw wsMalformedFrame();
		raw.push(data.subarray(offset, offset + n));
		offset += n;
	}
	if (offset !== data.byteLength) throw wsMalformedFrame();
	return { header, raw };
}

export const DEFAULT_MAX_WS_MESSAGE_BYTES = 256 * 1024 * 1024;
const WS_CHUNK_BYTES = 8 * 1024 * 1024;
const WS_CHUNK_THRESHOLD = 16 * 1024 * 1024;

export class WSChunkError extends Error {
	readonly code: number;
	constructor(code: number, message: string) {
		super(message);
		this.name = "WSChunkError";
		this.code = code;
	}
}

export class WSAssembler {
	private buf: Uint8Array | undefined = undefined;
	private filled = 0;
	private kind = 0;
	private limit: number;
	constructor(limit: number) {
		this.limit = limit;
	}

	feed(data: unknown): string | Uint8Array | undefined {
		const malformed = () =>
			new WSChunkError(1007, "malformed chunked message");
		if (typeof data === "string") {
			if (this.buf) throw malformed();
			return data;
		}
		const bytes = wsBytes(data);
		const view = new DataView(
			bytes.buffer,
			bytes.byteOffset,
			bytes.byteLength,
		);
		if (bytes.byteLength < 13 || view.getUint32(0) !== 0xffffffff) {
			if (this.buf) throw malformed();
			return bytes;
		}
		const kind = bytes[4] ?? 0;
		const total = Number(view.getBigUint64(5));
		const chunk = bytes.subarray(13);
		if (kind > 1) throw malformed();
		if (!this.buf) {
			if (this.limit >= 0 && total > this.limit)
				throw new WSChunkError(
					1009,
					"chunked message exceeds the size limit",
				);
			this.buf = new Uint8Array(total);
			this.filled = 0;
			this.kind = kind;
		} else if (total !== this.buf.byteLength || kind !== this.kind) {
			throw malformed();
		}
		if (this.filled + chunk.byteLength > total) throw malformed();
		this.buf.set(chunk, this.filled);
		this.filled += chunk.byteLength;
		if (this.filled < total) return undefined;
		const out = this.buf;
		this.buf = undefined;
		return this.kind === 1 ? out : wsTextDecoder.decode(out);
	}
}

export function wsSend(
	socket: { send(data: string | ArrayBuffer): void },
	payload: string | ArrayBuffer,
): void {
	let bytes: Uint8Array;
	let kind = 1;
	if (typeof payload === "string") {
		if (payload.length <= WS_CHUNK_THRESHOLD / 3) {
			socket.send(payload);
			return;
		}
		bytes = wsTextEncoder.encode(payload);
		kind = 0;
	} else {
		bytes = new Uint8Array(payload);
	}
	if (bytes.byteLength <= WS_CHUNK_THRESHOLD) {
		socket.send(payload);
		return;
	}
	for (let offset = 0; offset < bytes.byteLength; offset += WS_CHUNK_BYTES) {
		const part = bytes.subarray(
			offset,
			Math.min(offset + WS_CHUNK_BYTES, bytes.byteLength),
		);
		const out = new Uint8Array(13 + part.byteLength);
		const view = new DataView(out.buffer);
		view.setUint32(0, 0xffffffff);
		out[4] = kind;
		view.setBigUint64(5, BigInt(bytes.byteLength));
		out.set(part, 13);
		socket.send(out.buffer);
	}
}

export function wsEncodeMessage<T>(
	value: T,
	encode: (v: T) => unknown,
	split: ((v: T, raw: (Uint8Array | string)[]) => T) | undefined,
): string | ArrayBuffer {
	if (split) {
		const raw: (Uint8Array | string)[] = [];
		const header = split(value, raw);
		if (
			raw.some(
				(segment) =>
					(typeof segment === "string"
						? segment.length
						: segment.byteLength) > 0,
			)
		)
			return wsEncodeRawFrame(JSON.stringify(encode(header)), raw);
	}
	return JSON.stringify(encode(value));
}

export function wsDecodeMessage<T>(
	data: unknown,
	decode: (v: any) => T,
	join: ((v: T, raw: Uint8Array[], at: { i: number }) => boolean) | undefined,
): T {
	if (typeof data === "string") return decode(JSON.parse(data));
	const bytes = wsBytes(data);
	if (!join) return decode(JSON.parse(wsTextDecoder.decode(bytes)));
	const { header, raw } = wsDecodeRawFrame(bytes);
	const value = decode(JSON.parse(header));
	const at = { i: 0 };
	if (!join(value, raw, at) || at.i !== raw.length) throw wsMalformedFrame();
	return value;
}
