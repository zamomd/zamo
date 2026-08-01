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
		} catch {}
		return { code: "internal", message: err.message };
	}
	return { code: "internal", message: String(err) };
}

// readable maps each wire code (assigned in internal/wailsbind MarshalError) to
// a short, actionable sentence for the user. The Go message is a developer
// string — wrapped, id-bearing, sometimes a raw driver error — so it must never
// be shown directly; anything unmapped, including "internal", gets a generic
// line rather than leaking that text.
const readable: Record<string, string> = {
	not_found: "That block no longer exists — reload and try again.",
	cycle: "That move would put a block inside its own subtree.",
	invalid_ops: "That change is malformed and can't be applied.",
	empty_ops: "There's nothing to apply.",
	invalid_rollback: "That version can't be rolled back to.",
	duplicate_topic: "A topic with that name already exists.",
	unreferenceable_topic:
		"That name can't be linked to. A topic name can't contain [ or ], and a topic other blocks link to can't be left unnamed.",
	atomic_group: "This is part of a topic rename — accept or reject the whole batch on the Changes screen.",
	busy: "The database is busy. Try again in a moment.",
	llm_unavailable: "The AI provider is unreachable. Check your provider and key in Settings.",
	llm_bad_response: "The AI returned something unusable. Try again.",
	declarative: "Settings are locked by your config file (declarative mode).",
};

// messageFor turns a binding error into the sentence to show the user: a mapped,
// actionable message by code, never the raw developer/driver text.
export function messageFor(err: unknown): string {
	return readable[callError(err).code] ?? "Something went wrong. Please try again.";
}
