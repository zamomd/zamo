import { describe, expect, it } from "vitest";
import { blockRefAt, normalizeTitle, topicName, topicRefAt } from "./refs";
import { refLabel, topicReferenceable, triggerAt } from "./reftrigger";

describe("triggerAt", () => {
	it.each([
		["open topic trigger", "See [[loc", 9, { kind: "topic", query: "loc", start: 4 }],
		["empty topic query", "See [[", 6, { kind: "topic", query: "", start: 4 }],
		["open block trigger", "See ((claim", 11, { kind: "block", query: "claim", start: 4 }],
		["closed topic reference is not a trigger", "See [[Zig]] here", 16, null],
		["closed block reference is not a trigger", "See ((claim)) here", 18, null],
		["caret before the trigger", "See [[Zig", 3, null],
		["no trigger", "Just prose", 10, null],
		["newline ends a trigger", "See [[Zig\nmore", 14, null],
		// Editing inside a reference that is already closed: the popup must
		// stay away, or accepting would leave the old tail behind.
		["caret inside a closed topic reference", "[[Local-first]]", 7, null],
		["caret inside a closed block reference", "((claim|a label))", 12, null],
		["caret between the two closing brackets", "[[Local]]", 8, null],
		["caret between the two closing parens", "((claim))", 8, null],
		// The closer ahead belongs to a later reference, so this one is open.
		["a closed reference after an open one", "[[Loc[[Other]]", 5, { kind: "topic", query: "Loc", start: 0 }],
		["a closed reference on the next line", "See [[Zig\nand [[Rust]]", 9, { kind: "topic", query: "Zig", start: 4 }],
	])("%s", (_name, text, caret, want) => {
		expect(triggerAt(text, caret)).toEqual(want);
	});
});

describe("refLabel", () => {
	it("takes the first line", () => {
		expect(refLabel("A claim.\nMore body.")).toBe("A claim.");
	});
	it("strips markers", () => {
		expect(refLabel("## A heading")).toBe("A heading");
	});
	it("strips characters that would break the reference", () => {
		expect(refLabel("a|b )) c")).toBe("a b  c");
	});
	it("truncates on a word boundary", () => {
		const long = "word ".repeat(40).trim();
		const got = refLabel(long);
		expect(got.length).toBeLessThanOrEqual(80);
		expect(got.endsWith("word")).toBe(true);
	});
});

// RefAutocomplete.svelte writes ((id|refLabel(content))) and [[topicName(content)]]
// without a second look — these prove the composed text actually parses back
// out through the same grammar the reader uses, with the adversarial inputs
// that motivate refLabel and topicReferenceable in the first place.
describe("composed references survive the parser", () => {
	it("a block label with '|' and '))' in its source round-trips through blockRefAt", () => {
		const id = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
		const content = "Cite | sources )) carefully.\nMore body.";
		const label = refLabel(content);
		const written = `((${id}|${label}))`;
		const parsed = blockRefAt(written, 0);
		expect(parsed).not.toBeNull();
		expect(parsed?.target).toBe(id);
		expect(parsed?.label).toBe(label);
	});

	it("a bracket-free topic name round-trips through topicRefAt", () => {
		const content = "# Locality of reference";
		const name = topicName(content);
		expect(topicReferenceable(name)).toBe(true);
		const written = `[[${name}]]`;
		const parsed = topicRefAt(written, 0);
		expect(parsed).not.toBeNull();
		expect(normalizeTitle(parsed?.body ?? "")).toBe(normalizeTitle(content));
	});

	it("a bracketed topic name does not round-trip — topicReferenceable is why it must be excluded", () => {
		const content = "Array[T]";
		const name = topicName(content);
		expect(topicReferenceable(name)).toBe(false);
		const written = `[[${name}]]`;
		expect(topicRefAt(written, 0)).toBeNull();
	});
});
