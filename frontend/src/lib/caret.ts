import { renderMarkdown } from "./markdown";

// Entities produced by markdown-it with HTML disabled.
const entities: [RegExp, string][] = [
	[/&lt;/g, "<"],
	[/&gt;/g, ">"],
	[/&quot;/g, '"'],
	[/&#39;/g, "'"],
	// Ampersand last: decoding it first would let "&amp;lt;" become "<".
	[/&amp;/g, "&"],
];

// renderedText mirrors the rendered container's textContent. Tag stripping is
// safe because Markdown input HTML is disabled.
export function renderedText(raw: string): string {
	let text = renderMarkdown(raw).replace(/<[^>]*>/g, "");
	for (const [pattern, char] of entities) {
		text = text.replace(pattern, char);
	}
	return text;
}

// sourceOffset maps a rendered-text offset to its Markdown source offset.
// Unmappable positions fall back to the end of the source.
export function sourceOffset(raw: string, renderedOffset: number): number {
	const rendered = renderedText(raw);
	const target = Math.max(renderedOffset, 0);
	let r = 0;
	let s = 0;
	// Continue past the target through adjacent source-only delimiters.
	while (s < raw.length && (r < target || (r < rendered.length && raw[s] !== rendered[r]))) {
		// An escape and its visible character form one rendered unit.
		if (raw[s] === "\\" && raw[s + 1] === rendered[r]) {
			if (r === target) {
				break;
			}
			r++;
			s += 2;
			continue;
		}
		if (r < rendered.length && raw[s] === rendered[r]) {
			r++;
			s++;
			continue;
		}
		// markdown-it inserts text-node newlines around list markup.
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

// renderedOffsetFromPoint returns an offset into the container's textContent.
export function renderedOffsetFromPoint(container: HTMLElement, x: number, y: number): number | null {
	// Wails' WebKit webview exposes the non-standard caretRangeFromPoint API.
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
