import { describe, expect, it } from "vitest";
import {
	circleBox,
	clampPad,
	layout,
	type Point,
	radiusOf,
	reducedLayout,
	scaleFor,
	truncateLabel,
	viewBox,
} from "./graph";

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

	it("fits boxes that are not circles", () => {
		// viewBox itself stays generic — clampPad below reuses it with pad 0
		// to measure a box's own content extent — even though GraphView now
		// only ever calls it with circles.
		const box = viewBox([circleBox(0, 0, 5), { minX: 50, minY: -2, maxX: 90, maxY: 6 }], 4);
		const [minX, minY, w] = box.split(" ").map(Number);
		expect(minX + w).toBeGreaterThanOrEqual(94);
		expect(minY).toBeLessThanOrEqual(-6);
	});

	// A single node's content extent is a 10-unit circle box — with nothing to
	// fit against, the tight-fitting box used to scale that up to fill the
	// whole pane, making a one-topic base draw the single largest circle the
	// view can ever produce. See MIN_EXTENT.
	it("floors a single node's extent instead of fitting tight to it", () => {
		const box = viewBox([circleBox(0, 0, 5)], 0);
		const [, , w, h] = box.split(" ").map(Number);
		expect(w).toBeGreaterThanOrEqual(300);
		expect(h).toBeGreaterThanOrEqual(300);
	});

	it("leaves a content extent already past the floor unaffected", () => {
		const boxes = [circleBox(0, 0, 5), circleBox(400, 400, 5)];
		const box = viewBox(boxes, 0);
		const [minX, minY, w, h] = box.split(" ").map(Number);
		expect(minX).toBe(-5);
		expect(minY).toBe(-5);
		expect(w).toBe(410);
		expect(h).toBe(410);
	});

	it("floors each axis independently, not the box as a whole", () => {
		// A wide, flat box (content 400x10): the wide axis already clears the
		// floor and must stay untouched while the flat one is floored — proof
		// the two axes are not both forced to the same fallback shape.
		const box = viewBox([{ minX: 0, minY: 0, maxX: 400, maxY: 10 }], 0);
		const [, , w, h] = box.split(" ").map(Number);
		expect(w).toBe(400);
		expect(h).toBe(300);
	});
});

describe("clampPad", () => {
	it("passes small requests through unchanged", () => {
		const boxes = [{ minX: 0, minY: 0, maxX: 100, maxY: 40 }];
		expect(clampPad(boxes, 5)).toBe(5);
	});

	// Content extent well past viewBox's MIN_EXTENT floor on both axes, so
	// this isolates the tighter-axis fraction from the floor's own effect
	// (covered separately below).
	it("clamps a large request to a fraction of the tighter content axis", () => {
		const boxes = [{ minX: 0, minY: 0, maxX: 1000, maxY: 400 }]; // w=1000, h=400
		expect(clampPad(boxes, 10_000)).toBe(100); // 0.25 * 400
	});

	it("returns the request unclamped when there is no content to dominate", () => {
		expect(clampPad([], 24)).toBe(24);
	});

	// clampPad measures the content axis by calling viewBox with pad 0 (see
	// its own comment), so it inherits viewBox's floor for a tiny box rather
	// than clamping against the tiny raw extent. Without this, the smallest
	// bases would get the *stingiest* pad allowance of any base size —
	// backwards, since they are exactly the ones MIN_EXTENT exists to give
	// room to breathe.
	it("clamps against the floored extent, not the raw one, for content below the floor", () => {
		const boxes = [circleBox(0, 0, 5)]; // raw extent 10, far below the floor
		expect(clampPad(boxes, 1_000_000)).toBe(75); // 0.25 * 300
	});

	// This is the property that actually matters — not the clamp's internal
	// arithmetic, but what it guarantees about the fitted box: padding alone
	// cannot make the box balloon without bound, no matter how large a
	// request a runaway scale conversion produces. The old iterative version
	// failed exactly this, unmeasured, at 520×760 with six topics. Content
	// here is chosen past the floor so this isolates the pad clamp from the
	// floor — the floor's own effect on a tiny node is covered above and in
	// viewBox's own tests.
	it("never lets padding shrink the circle box below half the fitted viewBox on either axis", () => {
		const boxes = [circleBox(0, 0, 200)];
		const pad = clampPad(boxes, 1_000_000);
		const [, , w, h] = viewBox(boxes, pad).split(" ").map(Number);
		const content = 400; // 2 * r
		expect(content / w).toBeGreaterThanOrEqual(0.5);
		expect(content / h).toBeGreaterThanOrEqual(0.5);
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

	// Pins the actual bound rather than just "shorter than the input": a
	// change to the internal MAX_LABEL_CHARS constant (28 → 60, say) leaves
	// every other truncateLabel test green, since they only assert relative
	// properties. This one fails, because it names the number.
	it("bounds a long title to exactly 28 characters, ellipsis included", () => {
		const title = "x".repeat(100);
		expect(truncateLabel(title).length).toBe(28);
	});

	it("truncates by code point, not UTF-16 code unit, so a surrogate pair is never split", () => {
		// Each 🎯 is one code point but two UTF-16 units; 30 of them is well
		// past the 28-character bound, forcing a cut. Slicing by code unit
		// (the previous implementation's title.slice) can land inside a pair
		// and emit a lone surrogate, which renders as U+FFFD.
		const title = "🎯".repeat(30);
		const result = truncateLabel(title);
		expect(result.endsWith("…")).toBe(true);
		expect(result).not.toContain("�");
		// Everything before the ellipsis must be whole target emoji — never a
		// half-pair left over from a mid-code-point cut.
		expect(Array.from(result.slice(0, -1)).every((ch) => ch === "🎯")).toBe(true);
	});
});
