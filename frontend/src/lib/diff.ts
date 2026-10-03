import { diffArrays, diffWordsWithSpace } from "diff";

export type DiffPart = { text: string; changed: boolean };

// intraline marks a line whose parts single out what changed within it; on any
// other line the whole line is the change, or the lack of one.
export type DiffLine = { kind: "same" | "del" | "add"; parts: DiffPart[]; intraline: boolean };

function whole(kind: DiffLine["kind"], lines: string[]): DiffLine[] {
	return lines.map((text) => ({ kind, parts: [{ text, changed: false }], intraline: false }));
}

// splitLines regroups parts that may span line breaks into one list per line.
function splitLines(parts: DiffPart[]): DiffPart[][] {
	const lines: DiffPart[][] = [[]];
	for (const part of parts) {
		const [first, ...rest] = part.text.split("\n");
		if (first !== "") {
			lines[lines.length - 1].push({ text: first, changed: part.changed });
		}
		for (const text of rest) {
			lines.push(text === "" ? [] : [{ text, changed: part.changed }]);
		}
	}
	return lines;
}

// replaced words a run of removed lines against the lines that took their
// place. Runs with no text in common stay whole lines: marking every word as
// changed says nothing the line's own kind does not.
function replaced(removed: string[], added: string[]): DiffLine[] {
	const words = diffWordsWithSpace(removed.join("\n"), added.join("\n"));
	if (!words.some((w) => !w.added && !w.removed && w.value.trim() !== "")) {
		return [...whole("del", removed), ...whole("add", added)];
	}
	const side = (kind: "del" | "add"): DiffLine[] =>
		splitLines(
			words
				.filter((w) => (kind === "del" ? !w.added : !w.removed))
				.map((w) => ({ text: w.value, changed: w.added || w.removed })),
		).map((parts) => ({ kind, parts, intraline: true }));
	return [...side("del"), ...side("add")];
}

// diffLines is the line diff of a block's history, with the changed words
// singled out where a line was rewritten rather than added or removed.
export function diffLines(before: string, after: string): DiffLine[] {
	const chunks = diffArrays(before.split("\n"), after.split("\n"));
	const out: DiffLine[] = [];
	for (let i = 0; i < chunks.length; i++) {
		const chunk = chunks[i];
		const next = chunks[i + 1];
		if (chunk.removed && next?.added) {
			out.push(...replaced(chunk.value, next.value));
			i++;
		} else {
			out.push(...whole(chunk.removed ? "del" : chunk.added ? "add" : "same", chunk.value));
		}
	}
	return out;
}
