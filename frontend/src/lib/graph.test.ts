import { describe, expect, it } from "vitest";
import { circleBox, layout, type Point, radiusOf, reducedLayout, scaleFor, truncateLabel, viewBox } from "./graph";

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

	// The nearest-unlinked-pair comparator does not hold at every size: ambient
	// nearest-neighbour spacing shrinks under crowding as topic count grows, so
	// a linked pair (which settles near a roughly constant rest length) can end
	// up farther apart than the single closest unlinked pair once the base is
	// large enough. What the spring actually guarantees, at any size, is these
	// two weaker claims instead.
	describe.each([8, 50])("with %i topics", (n) => {
		it("ends the pair closer than it would be without the edge", () => {
			// t0 and t(n-1) are the two ends of the seeding spiral, so nothing
			// but the spring itself can be responsible for a difference here.
			const t = topics(n);
			const last = `t${n - 1}`;
			const withEdge = layout(t, [{ From: "t0", To: last }]);
			const withoutEdge = layout(t, []);
			expect(dist(withEdge, "t0", last)).toBeLessThan(dist(withoutEdge, "t0", last));
		});

		it("ends closer than the median unlinked pair", () => {
			// The product claim: a linked pair should read as clustered relative
			// to the rest of the map, not merely relative to whichever unlinked
			// pair happens to be nearest.
			const t = topics(n);
			const last = `t${n - 1}`;
			const result = layout(t, [{ From: "t0", To: last }]);
			const linked = dist(result, "t0", last);
			const unlinked: number[] = [];
			for (let a = 0; a < n; a++) {
				for (let b = a + 1; b < n; b++) {
					if (a === 0 && b === n - 1) {
						continue;
					}
					unlinked.push(dist(result, `t${a}`, `t${b}`));
				}
			}
			unlinked.sort((x, y) => x - y);
			const median = unlinked[Math.floor(unlinked.length / 2)];
			expect(linked).toBeLessThan(median);
		});
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

	it("keeps every coordinate finite under a large duplicate-edge count", () => {
		// Thousands of parallel edges between the same pair sum to a spring
		// force many times any single edge's — without a per-iteration move
		// clamp this diverges to NaN. Three topics, most of them unconnected,
		// isolate the effect to the pair actually under the load.
		const t = topics(3);
		const edges = Array.from({ length: 5000 }, () => ({ From: "t0", To: "t1" }));
		const result = layout(t, edges);
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

describe("circleBox", () => {
	it("bounds a circle by its center and radius", () => {
		expect(circleBox(10, -5, 3)).toEqual({ minX: 7, minY: -8, maxX: 13, maxY: -2 });
	});
});

describe("viewBox", () => {
	it("contains every box plus the padding", () => {
		const box = viewBox([circleBox(0, 0, 10), circleBox(100, 40, 5)], 8);
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

	it("fits boxes that are not circles, such as a label's footprint", () => {
		const box = viewBox([circleBox(0, 0, 5), { minX: 50, minY: -2, maxX: 90, maxY: 6 }], 4);
		const [minX, minY, w] = box.split(" ").map(Number);
		expect(minX + w).toBeGreaterThanOrEqual(94);
		expect(minY).toBeLessThanOrEqual(-6);
	});
});

describe("scaleFor", () => {
	it("uses the width ratio when width is the constraining axis", () => {
		// A wide box (200x100) into a narrow-ish container (100x100): fitting
		// the box's width into the container is the tighter constraint.
		expect(scaleFor(200, 100, 100, 100)).toBeCloseTo(2);
	});

	it("uses the height ratio when height is the constraining axis", () => {
		// A near-square box (100x100, like the golden-spiral bounding box) into
		// a wide, short pane (200x50): height is the tighter constraint, and a
		// formula that only looked at width would undershoot here.
		expect(scaleFor(100, 100, 200, 50)).toBeCloseTo(2);
	});

	it("falls back to 1 before the SVG has a measured pixel size", () => {
		expect(scaleFor(100, 100, 0, 0)).toBe(1);
	});
});

describe("truncateLabel", () => {
	it("leaves a short title untouched", () => {
		expect(truncateLabel("Comind roadmap")).toBe("Comind roadmap");
	});

	it("bounds a long title with an ellipsis", () => {
		const title = "A topic title long enough to be a full sentence about something";
		const result = truncateLabel(title);
		expect(result.length).toBeLessThan(title.length);
		expect(result.endsWith("…")).toBe(true);
		expect(title.startsWith(result.slice(0, -1))).toBe(true);
	});
});
