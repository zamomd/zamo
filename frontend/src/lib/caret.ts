import { renderMarkdown } from "./markdown";

// The caret's arithmetic. A click lands on rendered markdown while the editor
// edits the source, and the two do not share offsets: `**burst**` is nine
// characters written and five read. Everything here is pure and offset-based
// except renderedOffsetFromPoint, which is the one place a DOM is needed.

// The entities markdown-it's escapeHtml produces. Nothing else can appear,
// because markdown.ts runs with html: false, so content angle brackets are
// escaped rather than passed through.
const entities: [RegExp, string][] = [
	[/&lt;/g, "<"],
	[/&gt;/g, ">"],
	[/&quot;/g, '"'],
	[/&#39;/g, "'"],
	// Ampersand last: decoding it first would let "&amp;lt;" become "<".
	[/&amp;/g, "&"],
];

// renderedText is what the reader sees, as plain text. Its contract is
// byte-for-byte agreement with the rendered container's textContent, because
// sourceOffset maps between the two and a one-character disagreement is a
// one-character caret error.
//
// Stripping tags with a regex is safe on this input specifically: markdown.ts
// sets html: false, so every angle bracket in the content is already escaped and
// the only tags left are markdown-it's own. Tags come out before entities go in,
// or a decoded "<" would read as the start of a tag. Nothing is trimmed —
// markdown-it emits "<p>foo</p>\n", whose textContent is "foo\n".
export function renderedText(raw: string): string {
	let text = renderMarkdown(raw).replace(/<[^>]*>/g, "");
	for (const [pattern, char] of entities) {
		text = text.replace(pattern, char);
	}
	return text;
}

// sourceOffset maps an offset in the rendered text to the offset in the markdown
// source that produced it. Two pointers: equal characters advance both, unequal
// characters mean the source is carrying syntax the reader never saw, so only
// the source advances.
//
// A renderedOffset past the end of the rendered text — a click below the last
// line, or a mapping that did not converge — returns the end of the source. That
// is the deliberate safe default (spec §7.2): the worst outcome is a caret in
// the wrong place, and no content is at risk.
export function sourceOffset(raw: string, renderedOffset: number): number {
	const rendered = renderedText(raw);
	const target = Math.max(renderedOffset, 0);
	let r = 0;
	let s = 0;
	// Keep walking past the target once reached, as long as the next raw
	// character is still syntax (it doesn't equal the upcoming rendered
	// character). This is what carries the caret across a run of source-only
	// punctuation adjacent to the target rendered position: leading marks
	// before the first visible character of a run, or trailing marks right
	// after the last one. A raw character that matches rendered[r] belongs to
	// the *next* rendered position, so it stops the walk rather than being
	// consumed.
	while (s < raw.length && (r < target || (r < rendered.length && raw[s] !== rendered[r]))) {
		if (r < rendered.length && raw[s] === rendered[r]) {
			r++;
			s++;
			continue;
		}
		// The source can carry characters the reader never saw — that is markdown
		// syntax, and the walk skips them. The renderer does the same in the other
		// direction: markdown-it puts a real newline between <ul> and <li>, and it
		// is a text node, so it reaches textContent with nothing behind it in the
		// source. Without this the walk never converges for a list and every click
		// in one lands at the end of the block.
		//
		// Only while catching up, hence r < target. Past the target the walk has the
		// opposite job — stepping the source over the delimiters that closed the run
		// the caret is at the end of — and consuming a rendered newline there ends
		// the walk (the loop's second clause needs r < rendered.length) with the
		// caret still inside the markup: offset 5 of "**burst**" would come out as 7,
		// between "burst" and its own closing stars, so the next keystroke would
		// extend the emphasis rather than follow it.
		//
		// A hard break is untouched: there both sides hold "\n", so they match and
		// advance together before this rule is reached.
		if (r < target && rendered[r] === "\n" && raw[s] !== "\n") {
			r++;
			continue;
		}
		s++;
	}
	return r < target ? raw.length : s;
}

// lineCount counts the caret's lines, not the rendered ones: a trailing newline
// opens a real line the caret can sit on, so "a\n" is two.
export function lineCount(text: string): number {
	return text.split("\n").length;
}

// lineIndexOf is the zero-based line the offset sits on.
export function lineIndexOf(text: string, offset: number): number {
	let line = 0;
	for (let i = 0; i < offset && i < text.length; i++) {
		if (text[i] === "\n") {
			line++;
		}
	}
	return line;
}

// caretColumn counts characters from the start of the caret's own line. It is
// what survives an arrow jump between blocks.
export function caretColumn(text: string, offset: number): number {
	const before = text.slice(0, offset);
	const nl = before.lastIndexOf("\n");
	return nl === -1 ? before.length : before.length - nl - 1;
}

// offsetAtLine is caretColumn's inverse, clamped on both axes. Clamping is the
// normal case, not the error case: a column carried from a long line routinely
// lands past the end of a short one.
export function offsetAtLine(text: string, line: number, column: number): number {
	const lines = text.split("\n");
	const index = Math.min(Math.max(line, 0), lines.length - 1);
	let offset = 0;
	for (let i = 0; i < index; i++) {
		offset += lines[i].length + 1; // + the newline itself
	}
	return offset + Math.min(Math.max(column, 0), lines[index].length);
}

// renderedOffsetFromPoint reduces a click to a single offset into the container's
// textContent, so sourceOffset can map it back to the markdown behind it.
//
// The only DOM-dependent function here, and therefore the only one without unit
// tests: caretRangeFromPoint is WebKit's, has no jsdom equivalent worth mocking,
// and what it returns is exactly what a real click produces. It is kept small
// enough to read instead. A null return means "could not tell", which every
// caller answers with the end of the block.
export function renderedOffsetFromPoint(container: HTMLElement, x: number, y: number): number | null {
	// caretRangeFromPoint is WebKit's non-standard spelling; it is what the Wails
	// webview implements, and TypeScript's DOM lib does not declare it.
	const fromPoint = (
		document as Document & {
			caretRangeFromPoint?: (x: number, y: number) => Range | null;
		}
	).caretRangeFromPoint;
	const range = fromPoint?.call(document, x, y);
	if (!range || !container.contains(range.startContainer)) {
		return null;
	}
	// Sum the text nodes preceding the hit, in the same order textContent
	// concatenates them, then add the offset within the hit node.
	const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
	let offset = 0;
	let node = walker.nextNode();
	while (node !== null) {
		if (node === range.startContainer) {
			return offset + range.startOffset;
		}
		offset += node.textContent?.length ?? 0;
		node = walker.nextNode();
	}
	return null;
}
