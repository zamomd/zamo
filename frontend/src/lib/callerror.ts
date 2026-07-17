// The Go side wraps every binding error into {code, message} — see
// internal/wailsbind MarshalError, the single place errors get codes. The
// Wails runtime delivers that payload as the `cause` field of its CallError
// envelope; the current alpha's JS runtime rejects with a plain Error whose
// message is the envelope's JSON body, while newer runtimes promise a
// RuntimeError carrying the parsed payload in `cause`. callError accepts
// both, never throws, and downgrades anything unrecognised to "internal".
export type CallError = { code: string; message: string };

function payload(v: unknown): CallError | null {
	if (typeof v !== "object" || v === null) return null;
	const o = v as Record<string, unknown>;
	return typeof o.code === "string" && typeof o.message === "string" ? { code: o.code, message: o.message } : null;
}

export function callError(err: unknown): CallError {
	if (err instanceof Error) {
		const direct = payload(err.cause);
		if (direct) return direct;
		try {
			const envelope = JSON.parse(err.message) as { message?: unknown; cause?: unknown };
			const p = payload(envelope.cause);
			if (p) return p;
			if (typeof envelope.message === "string") {
				return { code: "internal", message: envelope.message };
			}
		} catch {
			// Not an envelope (e.g. a network failure): the raw message is
			// the best text there is.
		}
		return { code: "internal", message: err.message };
	}
	return { code: "internal", message: String(err) };
}
