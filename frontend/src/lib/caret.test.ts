import { describe, expect, it } from "vitest";
import { caretColumn, lineCount, lineIndexOf, offsetAtLine, renderedText, sourceOffset } from "./caret";

describe("renderedText", () => {
	// The contract is byte-for-byte agreement with the rendered container's
	// textContent, which is why the trailing newline markdown-it emits after a
	// block tag is kept rather than trimmed.
	it("keeps the newline markdown-it emits after a paragraph", () => {
		expect(renderedText("burst")).toBe("burst\n");
	});

	it("drops emphasis punctuation", () => {
		expect(renderedText("**burst** is the bucket size")).toBe("burst is the bucket size\n");
	});

	it("drops a link's target entirely", () => {
		expect(renderedText("see [the RFC](https://example.com/rfc)")).toBe("see the RFC\n");
	});

	it("decodes the entities markdown-it escapes", () => {
		expect(renderedText("a & b < c")).toBe("a & b < c\n");
	});

	// The shape sourceOffset's rendered-newline rule exists for. markdown-it emits
	// "<ul>\n<li>…</li>\n<li>…</li>\n</ul>\n": every one of those newlines is a real
	// text node, so it reaches textContent with nothing behind it in the source,
	// and the rendered offsets of a list are shifted by the leading one.
	it("carries markdown-it's own newlines around a list", () => {
		expect(renderedText("- token bucket")).toBe("\ntoken bucket\n\n");
		expect(renderedText("- alpha\n- beta")).toBe("\nalpha\nbeta\n\n");
	});
});

describe("sourceOffset", () => {
	// Each case is [name, raw, renderedOffset, expected source offset]. The
	// expected values are positions in the RAW string, so they are what the
	// textarea's selectionStart has to become.
	const cases: [string, string, number, number][] = [
		["plain text maps one to one", "burst = 40", 6, 6],
		["a caret before bold text skips the opening stars", "**burst** is a size", 0, 2],
		["a caret inside bold text keeps its place in the word", "**burst** is a size", 3, 5],
		["a caret after bold text is past the closing stars", "**burst** is a size", 5, 9],
		["inline code drops its backticks", "`burst` is a size", 2, 3],
		["a link's text is walked, its target is not", "see [the RFC](https://x.y) now", 8, 9],
		["a caret after a link lands after the whole link", "see [the RFC](https://x.y) now", 11, 26],
		["a heading's hashes are not in the rendered text", "## Rate limiting", 5, 8],
		// The rendered text of a list carries a leading newline from markdown-it's
		// "<ul>\n<li>" — it is a real text node, so it is in textContent and the
		// rendered offsets are shifted by one. Offset 6 is the caret after "token";
		// in the source that is offset 7, past the "- " the reader never saw.
		["a list marker is not in the rendered text", "- token bucket", 6, 7],
		["a caret at the start of a list item is past the marker", "- token bucket", 1, 2],
		// The pin for the rule above: the walk only converges on the second item
		// because it skipped the newline markdown-it put before the first. Rendered
		// "\nalpha\nbeta\n\n" offset 7 is the caret before "beta"; in the source that
		// is offset 10, past that item's own "- ". Drop the rule and the walk never
		// reaches the target at all, so it falls back to the source's end, 14.
		["a caret in a list's second item is past that item's marker", "- alpha\n- beta", 7, 10],
		// A block whose last visible character is followed by nothing but its own
		// closing delimiter. The caret has to come out past the delimiter — landing
		// between "burst" and "**" is not merely the wrong place, it is inside the
		// emphasis, and the next character typed joins it. Each expected value is
		// the raw string's own length: there is nothing after the markup to stop on.
		["a caret after bold text ending the block clears the stars", "**burst**", 5, 9],
		["a caret after inline code ending the block clears the backtick", "`burst`", 5, 7],
		["a caret after a link ending the block clears the target", "see [the RFC](https://x.y)", 11, 26],
		["a hard break maps to the newline it came from", "first\nsecond", 6, 6],
		["offset zero is offset zero for plain text", "burst", 0, 0],
	];
	for (const [name, raw, rendered, want] of cases) {
		it(name, () => {
			expect(sourceOffset(raw, rendered)).toBe(want);
		});
	}

	it("falls back to the end of the block when the offset is past the rendered text", () => {
		// The documented safe default: a caret in the wrong place is recoverable,
		// mangled text is not.
		expect(sourceOffset("**burst**", 99)).toBe(9);
	});

	it("returns zero for empty source", () => {
		expect(sourceOffset("", 0)).toBe(0);
	});
});

describe("line arithmetic", () => {
	const text = "first line\nsecond\n\nfourth";

	it("counts lines, including a trailing empty one", () => {
		expect(lineCount(text)).toBe(4);
		expect(lineCount("")).toBe(1);
		expect(lineCount("a\n")).toBe(2);
	});

	it("reports which line an offset is on", () => {
		expect(lineIndexOf(text, 0)).toBe(0);
		expect(lineIndexOf(text, 10)).toBe(0); // end of "first line"
		expect(lineIndexOf(text, 11)).toBe(1); // start of "second"
		expect(lineIndexOf(text, text.length)).toBe(3);
	});

	it("reports the column, counted from the start of the caret's own line", () => {
		expect(caretColumn(text, 0)).toBe(0);
		expect(caretColumn(text, 6)).toBe(6);
		expect(caretColumn(text, 11)).toBe(0);
		expect(caretColumn(text, 14)).toBe(3);
	});

	it("finds the offset for a line and column", () => {
		expect(offsetAtLine(text, 0, 3)).toBe(3);
		expect(offsetAtLine(text, 1, 3)).toBe(14);
		expect(offsetAtLine(text, 3, 0)).toBe(19);
	});

	// The column is preserved across an arrow jump, so it routinely lands past
	// the end of a shorter line and has to clamp rather than overshoot.
	it("clamps a column past the end of its line", () => {
		expect(offsetAtLine(text, 2, 40)).toBe(18); // line 2 is empty
		expect(offsetAtLine(text, 1, 40)).toBe(17); // "second" ends at 17
	});

	it("clamps a line index outside the text", () => {
		expect(offsetAtLine(text, 99, 0)).toBe(19);
		expect(offsetAtLine(text, -1, 0)).toBe(0);
	});
});
