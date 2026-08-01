// A TypeScript port of internal/knowledge/refs.go, kept structurally
// parallel to it: same function names, same order, so a future change to the
// grammar there is obvious to mirror here. internal/knowledge/testdata/refs.json
// is the contract between the two parsers — both must agree on every case in
// it, or a reference resolves differently depending on which side read the
// text. Rewriting references on rename is a Go-only concern with no GUI
// affordance, so RewriteTopicRef has no port here.
//
// The parity is bounded by character set, not absolute. Go's strings.Fields
// splits on unicode.IsSpace, which counts U+0085 and not U+FEFF, while JS's \s
// is the reverse; strings.ToLower maps U+0130 to "i" where JS toLowerCase maps
// it to "i" plus U+0307. A topic name carrying one of those would index under
// one key and resolve against another. Judged not worth a normalization layer
// on both sides — recorded so the next person does not have to rediscover it.

// RefKind distinguishes the two reference forms found in block content.
export type RefKind = "topic" | "block";

// Ref is one outgoing reference parsed out of a block's content. targetKey is
// the normalized topic name for "topic" and the upper-cased target ULID for
// "block"; the block reference's label is a reading aid inside the text and
// is deliberately not carried here.
export type Ref = { kind: RefKind; targetKey: string };

const ulidLen = 26;

// crockford is the Crockford base32 alphabet, which excludes I, L, O and U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

// parseRefs extracts every reference from a block's content, in source order
// and deduplicated. Text inside fenced code blocks and inline code spans is
// skipped: the base is largely about code, where both bracket forms occur
// literally.
export function parseRefs(content: string): Ref[] {
	const out: Ref[] = [];
	const seen = new Set<string>();
	const add = (ref: Ref): void => {
		if (ref.targetKey === "") {
			return;
		}
		const key = `${ref.kind}:${ref.targetKey}`;
		if (seen.has(key)) {
			return;
		}
		seen.add(key);
		out.push(ref);
	};
	for (const [line, fenced] of fencedLines(content)) {
		if (fenced) {
			continue;
		}
		scanLine(stripInlineCode(line), add);
	}
	return out;
}

// scanLine reports every reference on one line, code already removed.
function scanLine(line: string, add: (ref: Ref) => void): void {
	for (let i = 0; i < line.length; i++) {
		const topic = topicRefAt(line, i);
		if (topic !== null) {
			add({ kind: "topic", targetKey: normalizeTitle(topic.body) });
			i = topic.end - 1;
			continue;
		}
		const block = blockRefAt(line, i);
		if (block !== null) {
			add({ kind: "block", targetKey: block.target });
			i = block.end - 1;
		}
	}
}

// topicRefAt reports the [[Topic]] reference starting at i: its raw body and
// the offset just past the closing brackets, or null if there is none. An
// unterminated pair is plain text. Nested brackets mean the outer pair is not
// a reference either; the inner one is found by continuing the scan from just
// past "[[".
//
// Both the reader (scanLine) and the markdown-it plugin (markdown.ts) go
// through here, so what counts as a topic reference is defined once.
export function topicRefAt(line: string, i: number): { body: string; end: number } | null {
	if (i + 1 >= line.length || line[i] !== "[" || line[i + 1] !== "[") {
		return null;
	}
	const u = until(line, i + 2, "]]");
	if (u === null || u.body.includes("[") || u.body.includes("]")) {
		return null;
	}
	return u;
}

// blockRefAt reports the ((ULID|label)) reference starting at i: its
// upper-cased target id, its label (trimmed, "" if omitted — a bare
// ((ULID)) is valid), and the offset just past the closing parens, or null
// if there is none. A body whose target is not a ULID is plain text, and the
// scan continues inside it.
//
// The label is free text — it may hold brackets and pipes — so the span is
// skipped whole by every caller: a [[Topic]] written inside a label is
// neither indexed nor rendered as a link. It is returned (unlike Go's
// blockRefAt, which only ParseRefs calls) because the markdown-it plugin
// needs it too, for the fallback text an unresolved reference falls back to;
// deriving it a second time there would split the ((...)) grammar across two
// places.
export function blockRefAt(line: string, i: number): { target: string; label: string; end: number } | null {
	if (i + 1 >= line.length || line[i] !== "(" || line[i + 1] !== "(") {
		return null;
	}
	const u = until(line, i + 2, "))");
	if (u === null) {
		return null;
	}
	const pipe = u.body.indexOf("|");
	const raw = pipe >= 0 ? u.body.slice(0, pipe) : u.body;
	const label = pipe >= 0 ? u.body.slice(pipe + 1).trim() : "";
	const target = raw.trim().toUpperCase();
	if (!validULID(target)) {
		return null;
	}
	return { target, label, end: u.end };
}

// until returns the text between start and the next occurrence of closer,
// plus the offset just past closer. A missing terminator returns null.
function until(line: string, start: number, closer: string): { body: string; end: number } | null {
	const rest = line.slice(start);
	const j = rest.indexOf(closer);
	if (j < 0) {
		return null;
	}
	return { body: rest.slice(0, j), end: start + j + closer.length };
}

// fencedLines splits content into lines, reporting for each whether it is
// code: a fence delimiter itself or a line inside one. An unterminated fence
// swallows the rest of the content, matching how a markdown renderer reads
// it.
//
// The single definition of a code fence: the reader drops what it reports,
// and nothing here can drift from that.
function* fencedLines(content: string): Generator<[line: string, fenced: boolean]> {
	let fence = "";
	for (const line of content.split("\n")) {
		const trimmed = line.trim();
		if (fence !== "") {
			if (trimmed.startsWith(fence)) {
				fence = "";
			}
		} else if (trimmed.startsWith("```")) {
			fence = "```";
		} else if (trimmed.startsWith("~~~")) {
			fence = "~~~";
		} else {
			yield [line, false];
			continue;
		}
		yield [line, true];
	}
}

// stripInlineCode replaces every closed backtick span with spaces, preserving
// offsets so the caller can scan the line as-is.
//
// Spans are matched by backtick run, as CommonMark and therefore markdown-it
// do: a run of n backticks opens a span that the next run of exactly n closes,
// and a run with no matching closer is literal text the scan continues past.
// Counting runs is what makes ``[[Zig]]`` code to the reader and to the index
// alike — the single-backtick reading indexed it and left a backlink pointing
// into a code sample nobody could see.
//
// The grammar stops there. It has no escape mechanism, so a backslash before
// [[ escapes the bracket for markdown's renderer but not here, and \[[Zig]]
// still indexes: matching markdown's escaping too would mean a second markdown
// implementation in two languages, and the reference forms are rare enough
// characters that quoting them in code — which is handled — is how they
// actually turn up.
function stripInlineCode(line: string): string {
	const chars = line.split("");
	let i = 0;
	while (i < chars.length) {
		if (chars[i] !== "`") {
			i++;
			continue;
		}
		const open = i;
		i = runEnd(chars, i);
		const closeEnd = matchingRun(chars, i, i - open);
		if (closeEnd >= 0) {
			for (let k = open; k < closeEnd; k++) {
				chars[k] = " ";
			}
			i = closeEnd;
		}
	}
	return chars.join("");
}

// runEnd returns the offset just past the backtick run starting at i.
function runEnd(chars: string[], i: number): number {
	while (i < chars.length && chars[i] === "`") {
		i++;
	}
	return i;
}

// matchingRun finds the next backtick run of exactly n after from, returning
// the offset just past it, or -1 when there is none.
function matchingRun(chars: string[], from: number, n: number): number {
	for (let i = from; i < chars.length; ) {
		if (chars[i] !== "`") {
			i++;
			continue;
		}
		const run = runEnd(chars, i);
		if (run - i === n) {
			return run;
		}
		i = run;
	}
	return -1;
}

// validULID reports whether s is 26 Crockford base32 characters.
function validULID(s: string): boolean {
	if (s.length !== ulidLen) {
		return false;
	}
	for (const ch of s) {
		if (!crockford.includes(ch)) {
			return false;
		}
	}
	return true;
}

// normalizeTitle reduces a topic's content to the key references resolve
// against: its first line, without leading markdown heading or bullet
// markers, with inner whitespace collapsed and casefolded. The same function
// produces a topic's title_key on the Go side and the target key of a
// [[Topic]] reference — they must not drift apart or resolution fails
// silently.
export function normalizeTitle(s: string): string {
	return topicName(s)
		.split(/\s+/)
		.filter((f) => f !== "")
		.join(" ")
		.toLowerCase();
}

// topicName is the text a [[Topic]] reference to this content should carry:
// its first line without the markdown markers normalizeTitle also drops,
// with the author's casing and spacing kept.
export function topicName(s: string): string {
	return firstLine(s).replace(/^[#>*+ \t-]+/, "");
}

// firstLine is a block's display title: its first line, trimmed. Unlike
// normalizeTitle it preserves the author's casing and punctuation, because it
// is shown to a reader rather than matched against anything.
export function firstLine(s: string): string {
	const nl = s.indexOf("\n");
	return (nl >= 0 ? s.slice(0, nl) : s).trim();
}
