import { describe, expect, it } from "vitest";
import { layout, type Point, radiusOf, reducedLayout, viewBox } from "./graph";

// topics builds n topics with ids t0..t(n-1), the ULID order the read path
// guarantees.
function topics(n: number): { ID: string }[] {
	return Array.from({ length: n }, (_, i) => ({ ID: `t${i}` }));
}

function dist(p: Map<string, Point>, a: string, b: string): number {
	const pa = p.get(a);
	const pb = p.get(b);
	if (!pa || !pb) {
		throw new Error(`missing position for ${a} or ${b}`);
	}
	return Math.hypot(pa.x - pb.x, pa.y - pb.y);
}

describe("layout", () => {
	it("returns the same coordinates for the same input", () => {
		const t = topics(12);
		const e = [
			{ From: "t0", To: "t5" },
			{ From: "t3", To: "t9" },
		];
		const first = layout(t, e);
		const second = layout(t, e);
		for (const [id, p] of first) {
			expect(second.get(id)).toEqual(p);
		}
	});

	it("places every topic and only finite coordinates", () => {
		const result = layout(topics(30), [{ From: "t1", To: "t2" }]);
		expect(result.size).toBe(30);
		for (const p of result.values()) {
			expect(Number.isFinite(p.x)).toBe(true);
			expect(Number.isFinite(p.y)).toBe(true);
		}
	});

	it("pulls linked topics closer than any unlinked pair", () => {
		// t0 and t7 are far apart on the seeding spiral; the spring is the only
		// thing that can bring them together.
		const t = topics(8);
		const result = layout(t, [{ From: "t0", To: "t7" }]);
		const linked = dist(result, "t0", "t7");
		let closestUnlinked = Number.POSITIVE_INFINITY;
		for (let a = 0; a < 8; a++) {
			for (let b = a + 1; b < 8; b++) {
				if (a === 0 && b === 7) {
					continue;
				}
				closestUnlinked = Math.min(closestUnlinked, dist(result, `t${a}`, `t${b}`));
			}
		}
		expect(linked).toBeLessThan(closestUnlinked);
	});

	it("survives degenerate input", () => {
		expect(layout([], []).size).toBe(0);
		expect(layout(topics(1), []).size).toBe(1);
		// A self-edge and an edge to an unknown id must not crash or produce NaN:
		// the read path excludes both, but the layout is not entitled to assume it.
		const result = layout(topics(3), [
			{ From: "t0", To: "t0" },
			{ From: "t0", To: "nope" },
			{ From: "t1", To: "t2" },
			{ From: "t1", To: "t2" },
		]);
		expect(result.size).toBe(3);
		for (const p of result.values()) {
			expect(Number.isFinite(p.x)).toBe(true);
			expect(Number.isFinite(p.y)).toBe(true);
		}
	});

	it("completes on a base larger than the full-iteration threshold", () => {
		const result = layout(topics(250), []);
		expect(result.size).toBe(250);
		for (const p of result.values()) {
			expect(Number.isFinite(p.x)).toBe(true);
		}
		expect(reducedLayout(250)).toBe(true);
		expect(reducedLayout(10)).toBe(false);
	});
});

describe("radiusOf", () => {
	it("grows with the block count and never vanishes or runs away", () => {
		expect(radiusOf(4)).toBeGreaterThan(radiusOf(1));
		expect(radiusOf(1)).toBeGreaterThan(0);
		// A topic with no blocks cannot happen — its root is a block — but a
		// zero must still draw something rather than a point.
		expect(radiusOf(0)).toBe(radiusOf(1));
		expect(radiusOf(100_000)).toBe(radiusOf(1_000_000));
	});
});

describe("viewBox", () => {
	it("contains every node's whole circle plus the padding", () => {
		const box = viewBox(
			[
				{ x: 0, y: 0, r: 10 },
				{ x: 100, y: 40, r: 5 },
			],
			8,
		);
		const [minX, minY, w, h] = box.split(" ").map(Number);
		expect(minX).toBeLessThanOrEqual(-18);
		expect(minY).toBeLessThanOrEqual(-18);
		expect(minX + w).toBeGreaterThanOrEqual(113);
		expect(minY + h).toBeGreaterThanOrEqual(53);
	});

	it("gives an empty graph a box with area", () => {
		const [, , w, h] = viewBox([], 8).split(" ").map(Number);
		expect(w).toBeGreaterThan(0);
		expect(h).toBeGreaterThan(0);
	});
});
