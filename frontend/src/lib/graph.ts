// The topic map's geometry. Everything here is a pure function of its
// arguments: no Math.random, no clock, no animation frame. A set of topics has
// no geometry of its own — physics invents one — and an invented geometry that
// differs between runs cannot be checked by a test or by comparing screenshots,
// which is the only way this project can verify appearance at all.

export type Point = { x: number; y: number };

// The layout takes structural types rather than the generated binding types, so
// it can be tested without the Wails bindings. TopicNode and TopicEdge are
// assignable to these.
export type LayoutTopic = { ID: string };
export type LayoutEdge = { From: string; To: string };

// The golden angle. Seeding node i at angle i·GOLDEN_ANGLE with radius
// SPIRAL_STEP·√i spreads the nodes evenly with no clumping, and — because the
// read path returns topics in ULID order — a new topic takes a new slot at the
// end instead of shifting the ones already placed. The simulation still nudges
// its neighbours afterwards, so the map is perturbed by an addition, not
// reshuffled by it.
const GOLDEN_ANGLE = Math.PI * (3 - Math.sqrt(5));
const SPIRAL_STEP = 40;

// The simulation. Repulsion separates every pair, springs pull linked topics to
// a rest length, and a weak pull toward the origin keeps disconnected
// components from drifting away — which they otherwise do, since nothing else
// acts on them.
const ITERATIONS = 300;
const REPULSION = 9000;
const SPRING = 0.1;
const REST_LENGTH = 90;
const CENTERING = 0.006;
// Cooling: the step shrinks each iteration, so the system settles instead of
// oscillating around its equilibrium forever.
const COOLING = 0.985;
const INITIAL_STEP = 1;

// Two nodes at the same point would make the inverse-square repulsion
// infinite. No current input reaches this: seeding places every node at a
// distance set purely by its index, and across every fixture in
// graph.test.ts — including the stress fixture of thousands of duplicate
// edges pulling two nodes together — the observed minimum inter-node
// distance stays well clear of this threshold. The guard stays anyway,
// three lines of cheap insurance against a future change to seeding rather
// than a case any input today can trigger. If it is ever reached, nodes are
// pushed apart along a fixed axis by MIN_DISTANCE — fixed, not random, so the
// result stays reproducible.
const MIN_DISTANCE = 1;
// No node may move more than MAX_MOVE in one iteration — the displacement
// vector's magnitude is clamped, not each axis independently, so a diagonal
// step is bounded the same as an axis-aligned one. This keeps a near-collision
// (see MIN_DISTANCE above) from launching a node off the map in one step.
const MAX_MOVE = 20;

// Repulsion is over all pairs, so the cost is quadratic in the topic count and
// linear in the iterations. Above this many topics the iteration count drops,
// and the view says so rather than silently degrading. Barnes-Hut is the answer
// if a base ever really grows that far.
const FULL_ITERATION_MAX = 200;
const REDUCED_ITERATIONS = 60;

export function reducedLayout(topicCount: number): boolean {
	return topicCount > FULL_ITERATION_MAX;
}

export function layout(topics: LayoutTopic[], edges: LayoutEdge[]): Map<string, Point> {
	const n = topics.length;
	const xs = new Float64Array(n);
	const ys = new Float64Array(n);
	for (let i = 0; i < n; i++) {
		const angle = i * GOLDEN_ANGLE;
		const radius = SPIRAL_STEP * Math.sqrt(i);
		xs[i] = radius * Math.cos(angle);
		ys[i] = radius * Math.sin(angle);
	}

	const index = new Map(topics.map((t, i) => [t.ID, i]));
	// An edge to an unknown topic, or a topic to itself, contributes no force.
	// The read path emits neither, but the layout does not depend on that.
	const links: Array<[number, number]> = [];
	for (const e of edges) {
		const a = index.get(e.From);
		const b = index.get(e.To);
		if (a !== undefined && b !== undefined && a !== b) {
			links.push([a, b]);
		}
	}

	const fx = new Float64Array(n);
	const fy = new Float64Array(n);
	const iterations = reducedLayout(n) ? REDUCED_ITERATIONS : ITERATIONS;
	let step = INITIAL_STEP;

	for (let it = 0; it < iterations; it++) {
		fx.fill(0);
		fy.fill(0);

		for (let a = 0; a < n; a++) {
			for (let b = a + 1; b < n; b++) {
				let dx = xs[a] - xs[b];
				let dy = ys[a] - ys[b];
				let d = Math.hypot(dx, dy);
				if (d < MIN_DISTANCE) {
					dx = MIN_DISTANCE;
					dy = 0;
					d = MIN_DISTANCE;
				}
				const f = REPULSION / (d * d);
				const ux = (dx / d) * f;
				const uy = (dy / d) * f;
				fx[a] += ux;
				fy[a] += uy;
				fx[b] -= ux;
				fy[b] -= uy;
			}
		}

		for (const [a, b] of links) {
			const dx = xs[b] - xs[a];
			const dy = ys[b] - ys[a];
			const d = Math.max(Math.hypot(dx, dy), MIN_DISTANCE);
			const f = SPRING * (d - REST_LENGTH);
			const ux = (dx / d) * f;
			const uy = (dy / d) * f;
			fx[a] += ux;
			fy[a] += uy;
			fx[b] -= ux;
			fy[b] -= uy;
		}

		for (let i = 0; i < n; i++) {
			fx[i] -= xs[i] * CENTERING;
			fy[i] -= ys[i] * CENTERING;
			const [dx, dy] = clampMagnitude(fx[i] * step, fy[i] * step, MAX_MOVE);
			xs[i] += dx;
			ys[i] += dy;
		}
		step *= COOLING;
	}

	const out = new Map<string, Point>();
	for (let i = 0; i < n; i++) {
		out.set(topics[i].ID, { x: xs[i], y: ys[i] });
	}
	return out;
}

// clampMagnitude scales (dx, dy) down to at most limit if it exceeds it,
// preserving direction. Clamping the vector's magnitude, rather than each
// axis independently, bounds a diagonal step the same as an axis-aligned one.
function clampMagnitude(dx: number, dy: number, limit: number): [number, number] {
	const magnitude = Math.hypot(dx, dy);
	if (magnitude <= limit) {
		return [dx, dy];
	}
	const scale = limit / magnitude;
	return [dx * scale, dy * scale];
}

// Node radius from block count. The area scales with the count, not the radius,
// so a topic with four times the blocks reads as four times the ink rather than
// sixteen. Both ends are clamped: a one-block topic has to stay visible, and a
// huge one must not swallow the map.
const BASE_RADIUS = 5;
const MAX_RADIUS = 26;

export function radiusOf(blocks: number): number {
	return Math.min(BASE_RADIUS * Math.sqrt(Math.max(blocks, 1)), MAX_RADIUS);
}

// viewBox fits every node's whole circle, plus pad, into an SVG viewBox string.
// Fitting the content is what lets the first version ship without pan or zoom.
// An empty graph still needs a box with area, or the SVG collapses.
export function viewBox(nodes: Array<{ x: number; y: number; r: number }>, pad: number): string {
	if (nodes.length === 0) {
		return `0 0 ${2 * pad} ${2 * pad}`;
	}
	let minX = Number.POSITIVE_INFINITY;
	let minY = Number.POSITIVE_INFINITY;
	let maxX = Number.NEGATIVE_INFINITY;
	let maxY = Number.NEGATIVE_INFINITY;
	for (const n of nodes) {
		minX = Math.min(minX, n.x - n.r);
		minY = Math.min(minY, n.y - n.r);
		maxX = Math.max(maxX, n.x + n.r);
		maxY = Math.max(maxY, n.y + n.r);
	}
	return `${minX - pad} ${minY - pad} ${maxX - minX + 2 * pad} ${maxY - minY + 2 * pad}`;
}
