export type DiffLine = { kind: "same" | "del" | "add"; text: string };

// diffLines is a minimal LCS line diff for the review screen: enough to see
// what an edit overwrites, not a general-purpose diff.
export function diffLines(before: string, after: string): DiffLine[] {
	const a = before.split("\n");
	const b = after.split("\n");
	const lcs: number[][] = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0));
	for (let i = a.length - 1; i >= 0; i--) {
		for (let j = b.length - 1; j >= 0; j--) {
			lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
		}
	}
	const out: DiffLine[] = [];
	let i = 0;
	let j = 0;
	while (i < a.length && j < b.length) {
		if (a[i] === b[j]) {
			out.push({ kind: "same", text: a[i] });
			i++;
			j++;
		} else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
			out.push({ kind: "del", text: a[i] });
			i++;
		} else {
			out.push({ kind: "add", text: b[j] });
			j++;
		}
	}
	for (; i < a.length; i++) out.push({ kind: "del", text: a[i] });
	for (; j < b.length; j++) out.push({ kind: "add", text: b[j] });
	return out;
}
