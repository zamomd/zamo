import { topicName } from "./refs";

// Trigger describes an in-progress reference the caret currently sits inside:
// an unclosed "[[" or "((" on the caret's own line, with nothing yet typed
// past it that would close it. The popup that completes it lives in
// RefAutocomplete.svelte.
export type Trigger = { kind: "topic" | "block"; query: string; start: number };

const openers: Record<string, "topic" | "block"> = { "[[": "topic", "((": "block" };
const closers: Record<"topic" | "block", string> = { topic: "]]", block: "))" };

// triggerAt scans backwards from caret to the nearest "[[" or "((" on the
// same line. A closing "]]"/"))" between that opener and caret means the
// reference is already complete — not a trigger — and the scan does not look
// past it for an earlier, enclosing opener.
export function triggerAt(text: string, caret: number): Trigger | null {
	const lineStart = text.lastIndexOf("\n", caret - 1) + 1;
	let start = -1;
	let kind: "topic" | "block" | null = null;
	for (let i = caret - 2; i >= lineStart; i--) {
		const two = text.slice(i, i + 2);
		const found = openers[two];
		if (found !== undefined) {
			start = i;
			kind = found;
			break;
		}
	}
	if (start < 0 || kind === null) {
		return null;
	}
	const query = text.slice(start + 2, caret);
	if (query.includes(closers[kind])) {
		return null;
	}
	return { kind, query, start };
}

const maxLabelLength = 80;

// refLabel derives the label baked into a ((ULID|label)) reference from its
// target's content: the first line with markdown markers stripped, its
// whitespace collapsed, the syntax-breaking characters "|" and ")" removed —
// a pipe would be read as the label separator, a close-paren as the
// reference's own terminator — and the result truncated at 80 characters on a
// word boundary so a long block still leaves a readable sentence.
export function refLabel(content: string): string {
	const collapsed = topicName(content)
		.split(/\s+/)
		.filter((w) => w !== "")
		.join(" ");
	const safe = collapsed.replaceAll("|", " ").replaceAll(")", "");
	return truncate(safe, maxLabelLength);
}

function truncate(s: string, max: number): string {
	if (s.length <= max) {
		return s;
	}
	const cut = s.slice(0, max);
	const lastSpace = cut.lastIndexOf(" ");
	return (lastSpace > 0 ? cut.slice(0, lastSpace) : cut).trimEnd();
}

// topicReferenceable reports whether name can be written as [[name]] at all.
// topicRefAt (refs.ts) rejects any body containing "[" or "]" outright — no
// escaping makes it through — so a title or a typed query carrying either can
// never resolve as a topic reference. The autocomplete popup must not offer
// such a name, whether as an existing topic to link to or as a target to
// create: unlike a block's label (see refLabel), a topic's name has no safe
// substitute — sanitizing it would insert a reference to a *different* name
// than the one shown, silently pointing at nothing.
export function topicReferenceable(name: string): boolean {
	return !name.includes("[") && !name.includes("]");
}
