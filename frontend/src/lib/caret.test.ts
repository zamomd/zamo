import { describe, expect, it } from "vitest";
import { caretColumn, lineCount, lineIndexOf, offsetAtLine, renderedText, sourceOffset } from "./caret";

// None of this file's cases involve a reference, so every call gets the same
// always-unresolved resolver — the point under test is the markdown↔text
// mapping, not reference resolution (see markdown.test.ts for that).
const noResolver = () => null;

describe("renderedText", () => {
	it("keeps the newline markdown-it emits after a paragraph", () => {
		expect(renderedText("burst", noResolver)).toBe("burst\n");
	});

	it("drops emphasis punctuation", () => {
		expect(renderedText("**burst** is the bucket size", noResolver)).toBe("burst is the bucket size\n");
	});

	it("drops a link's target entirely", () => {
		expect(renderedText("see [the RFC](https://example.com/rfc)", noResolver)).toBe("see the RFC\n");
	});

	it("decodes the entities markdown-it escapes", () => {
		expect(renderedText("a & b < c", noResolver)).toBe("a & b < c\n");
	});

	it("decodes named and numeric entities like the rendered DOM", () => {
		expect(renderedText("&copy; &#169;", noResolver)).toBe("© ©\n");
	});

	it("carries markdown-it's own newlines around a list", () => {
		expect(renderedText("- token bucket", noResolver)).toBe("\ntoken bucket\n\n");
		expect(renderedText("- alpha\n- beta", noResolver)).toBe("\nalpha\nbeta\n\n");
	});

	it("uses the given resolver's live text for a resolved reference, not the written form", () => {
		expect(renderedText("See [[Topic]].", () => "Resolved Title")).toBe("See Resolved Title.\n");
	});
});

describe("sourceOffset with a resolved reference", () => {
	// A resolver is threaded through rather than defaulted (see renderMarkdown)
	// precisely so this text matches whatever the caller actually put on
	// screen — sourceOffset must be handed the same resolver the DOM was
	// rendered with, or the offset it measures and the text it diffs against
	// disagree. These two cases pin that down: an offset landing before the
	// substitution still maps exactly, one landing inside or after it falls
	// back to the end of the source (the same documented, safe fallback the
	// plain-text cases above rely on) rather than a wrong interior offset.
	const raw = "before [[Topic]] after";
	const resolve = () => "X"; // "before X after\n" once rendered

	it("maps exactly up to where the resolved text begins", () => {
		expect(sourceOffset(raw, "before".length, resolve)).toBe("before".length);
	});

	it("falls back to the end of the source once past the substitution", () => {
		expect(sourceOffset(raw, "before X".length, resolve)).toBe(raw.length);
	});
});

describe("sourceOffset", () => {
	const cases: [string, string, number, number][] = [
		["plain text maps one to one", "burst = 40", 6, 6],
		["a caret before bold text skips the opening stars", "**burst** is a size", 0, 2],
		["a caret inside bold text keeps its place in the word", "**burst** is a size", 3, 5],
		["a caret after bold text is past the closing stars", "**burst** is a size", 5, 9],
		["inline code drops its backticks", "`burst` is a size", 2, 3],
		["a link's text is walked, its target is not", "see [the RFC](https://x.y) now", 8, 9],
		["a caret after a link lands after the whole link", "see [the RFC](https://x.y) now", 11, 26],
		["a heading's hashes are not in the rendered text", "## Rate limiting", 5, 8],
		["a list marker is not in the rendered text", "- token bucket", 6, 7],
		["a caret at the start of a list item is past the marker", "- token bucket", 1, 2],
		["a caret in a list's second item is past that item's marker", "- alpha\n- beta", 7, 10],
		["a caret after bold text ending the block clears the stars", "**burst**", 5, 9],
		["a caret after inline code ending the block clears the backtick", "`burst`", 5, 7],
		["a caret after a link ending the block clears the target", "see [the RFC](https://x.y)", 11, 26],
		["a caret before escaped punctuation stays before its backslash", String.raw`\*literal\*`, 0, 0],
		["a caret after escaped punctuation clears its backslash pair", String.raw`\*literal\*`, 1, 2],
		["a caret before a closing escaped punctuation stays before its backslash", String.raw`\*literal\*`, 8, 9],
		["a caret after a literal backslash clears the escaped pair", String.raw`\\path`, 1, 2],
		["a hard break maps to the newline it came from", "first\nsecond", 6, 6],
		["a caret after a named entity clears its source", "&copy; x", 1, 6],
		["a caret after a numeric entity clears its source", "&#169; x", 1, 6],
		["offset zero is offset zero for plain text", "burst", 0, 0],
	];
	for (const [name, raw, rendered, want] of cases) {
		it(name, () => {
			expect(sourceOffset(raw, rendered, noResolver)).toBe(want);
		});
	}

	it("falls back to the end of the block when the offset is past the rendered text", () => {
		expect(sourceOffset("**burst**", 99, noResolver)).toBe(9);
	});

	it("returns zero for empty source", () => {
		expect(sourceOffset("", 0, noResolver)).toBe(0);
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

	it("clamps a column past the end of its line", () => {
		expect(offsetAtLine(text, 2, 40)).toBe(18); // line 2 is empty
		expect(offsetAtLine(text, 1, 40)).toBe(17); // "second" ends at 17
	});

	it("clamps a line index outside the text", () => {
		expect(offsetAtLine(text, 99, 0)).toBe(19);
		expect(offsetAtLine(text, -1, 0)).toBe(0);
	});
});
