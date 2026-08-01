import MarkdownIt from "markdown-it";
import { blockRefAt, normalizeTitle, type Ref, topicRefAt } from "./refs";

// Block content is authored by an LLM, so it is untrusted. `html: false` makes
// markdown-it escape any raw HTML in the source and never emit input-derived
// tags, and its default link validation rejects javascript:, vbscript: and
// data: hrefs — so the rendered string is safe to inject with {@html}. That
// guarantee holds only as long as html stays off and no raw-HTML plugin is
// added here; keep this module the single place markdown becomes HTML. The
// reference rule below follows the same rule: it only pushes tokens through
// state.push and sets attributes with attrSet, so its output goes through the
// same escaping as every other token — never an assembled HTML string.
const md = new MarkdownIt({
	html: false,
	linkify: true,
	breaks: true,
});

// Mark every link external. The rendered href is already scheme-checked by
// markdown-it; the app additionally intercepts the click (see TreeNode) to open
// the system browser instead of navigating the webview, and target/rel make the
// intent explicit and safe even if an anchor is ever activated another way.
// With no prior link_open rule, the renderer's default is self.renderToken, so
// setting attributes and delegating to it is the canonical override.
md.renderer.rules.link_open = (tokens, idx, options, _env, self) => {
	tokens[idx].attrSet("target", "_blank");
	tokens[idx].attrSet("rel", "noopener noreferrer");
	return self.renderToken(tokens, idx, options);
};

// RefResolver supplies the live display text for a reference. Returning null
// means unresolved: a topic that does not exist yet, or a block that is gone.
// The renderer falls back to the reference's own label, so a dead target
// still leaves a readable sentence.
export type RefResolver = (ref: Ref) => string | null;

// noResolver treats every reference as unresolved, so rendering always shows
// the written text and never a live lookup. It is the default for callers
// that need a stable mapping from source to rendered text independent of
// what the store currently holds — caret.ts's caret-position arithmetic.
const noResolver: RefResolver = () => null;

// refAt finds a [[Topic]] or ((ULID|label)) reference starting at pos, plus
// the fallback text to show when it does not resolve. The search is bounded
// to the current line: state.src can join several source lines with "\n" (a
// soft break inside one paragraph), and, like the Go parser, a reference
// never crosses that boundary.
function refAt(src: string, pos: number): { ref: Ref; fallback: string; end: number } | null {
	const nl = src.indexOf("\n", pos);
	const line = nl < 0 ? src : src.slice(0, nl);

	const topic = topicRefAt(line, pos);
	if (topic !== null) {
		return {
			ref: { kind: "topic", targetKey: normalizeTitle(topic.body) },
			fallback: topic.body.trim(),
			end: topic.end,
		};
	}

	const block = blockRefAt(line, pos);
	if (block !== null) {
		const body = line.slice(pos + 2, block.end - 2);
		const pipe = body.indexOf("|");
		const label = pipe >= 0 ? body.slice(pipe + 1).trim() : "";
		return {
			ref: { kind: "block", targetKey: block.target },
			fallback: label !== "" ? label : block.target,
			end: block.end,
		};
	}

	return null;
}

// markdown-it's built-in "text" rule consumes a run of characters up to the
// next "terminator" — a fixed set of punctuation "reserved for extensions"
// (see rules_inline/text.mjs) that a plugin's own rule can then claim first.
// "[" is in that set, which is the only reason the topic form works without
// this override: "text" already stops right before it. "(" is not, so a run
// like "See ((...))" would otherwise be swallowed whole by "text" — reached
// before the ref rule ever saw the "(" — with no per-character fallback to
// hand control back. This is the same rule with "(" added to the set;
// everything else is unchanged from markdown-it's own implementation.
function isRefTerminator(code: number): boolean {
	switch (code) {
		case 0x0a: // \n
		case 0x21: // !
		case 0x23: // #
		case 0x24: // $
		case 0x25: // %
		case 0x26: // &
		case 0x28: // (
		case 0x2a: // *
		case 0x2b: // +
		case 0x2d: // -
		case 0x3a: // :
		case 0x3c: // <
		case 0x3d: // =
		case 0x3e: // >
		case 0x40: // @
		case 0x5b: // [
		case 0x5c: // \
		case 0x5d: // ]
		case 0x5e: // ^
		case 0x5f: // _
		case 0x60: // `
		case 0x7b: // {
		case 0x7d: // }
		case 0x7e: // ~
			return true;
		default:
			return false;
	}
}
md.inline.ruler.at("text", (state, silent) => {
	let pos = state.pos;
	while (pos < state.posMax && !isRefTerminator(state.src.charCodeAt(pos))) {
		pos++;
	}
	if (pos === state.pos) {
		return false;
	}
	if (!silent) {
		state.pending += state.src.slice(state.pos, pos);
	}
	state.pos = pos;
	return true;
});

// The inline rule for both reference forms. It emits a plain span_open/text/
// span_close token triple — no renderer.rules override needed, since the
// default renderToken already escapes attributes and the default text rule
// already escapes content. Registered before "text" so it claims a "(" or
// "[" position first, per the terminator set above.
md.inline.ruler.before("text", "ref", (state, silent) => {
	const match = refAt(state.src, state.pos);
	if (match === null) {
		return false;
	}
	if (!silent) {
		const { resolve } = state.env as { resolve: RefResolver };
		const resolved = resolve(match.ref);
		const classes = ["ref", `ref-${match.ref.kind}`];
		if (resolved === null) {
			classes.push("ref-missing");
		}
		const open = state.push("ref_open", "span", 1);
		open.attrSet("class", classes.join(" "));
		open.attrSet("data-target", match.ref.targetKey);
		const text = state.push("text", "", 0);
		text.content = resolved ?? match.fallback;
		state.push("ref_close", "span", -1);
	}
	state.pos = match.end;
	return true;
});

// renderMarkdown turns one block's markdown source into sanitized HTML ready for
// {@html}. The raw source is never mutated — it stays the edit round-trip value.
// resolve defaults to treating every reference as unresolved, for callers that
// only need the written text back rather than a live lookup.
export function renderMarkdown(src: string, resolve: RefResolver = noResolver): string {
	return md.render(src, { resolve });
}
