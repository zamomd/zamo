// Topic-map geometry is deterministic so layouts remain stable across renders.

export type Point = { x: number; y: number };

// Structural inputs keep the layout independent of generated bindings.
export type LayoutTopic = { ID: string };
export type LayoutEdge = { From: string; To: string };

// Golden-angle seeding spreads nodes evenly while preserving their input order.
const GOLDEN_ANGLE = Math.PI * (3 - Math.sqrt(5));
const SPIRAL_STEP = 40;

// Repulsion separates topics, springs cluster links, and centering bounds
// disconnected components.
const ITERATIONS = 300;
const REPULSION = 9000;
const SPRING = 0.1;
const REST_LENGTH = 90;
const CENTERING = 0.006;
const COOLING = 0.985;
const INITIAL_STEP = 1;

// Avoid singular repulsion while retaining deterministic output.
const MIN_DISTANCE = 1;
const MAX_MOVE = 20;

// Repulsion is quadratic, so large maps use fewer iterations.
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

// clampMagnitude bounds a vector without changing its direction.
function clampMagnitude(dx: number, dy: number, limit: number): [number, number] {
	const magnitude = Math.hypot(dx, dy);
	if (magnitude <= limit) {
		return [dx, dy];
	}
	const scale = limit / magnitude;
	return [dx * scale, dy * scale];
}

// Radius scales area with block count and stays within visible bounds.
const BASE_RADIUS = 5;
const MAX_RADIUS = 26;

export function radiusOf(blocks: number): number {
	return Math.min(BASE_RADIUS * Math.sqrt(Math.max(blocks, 1)), MAX_RADIUS);
}

export type Box = { minX: number; minY: number; maxX: number; maxY: number };

// circleBox returns a circle's axis-aligned bounding box.
export function circleBox(x: number, y: number, r: number): Box {
	return { minX: x - r, minY: y - r, maxX: x + r, maxY: y + r };
}

// A minimum fitted extent prevents sparse maps from magnifying small topics.
const MIN_EXTENT = 300;

// floorAxis expands an interval to MIN_EXTENT without moving its midpoint.
function floorAxis(min: number, max: number): [number, number] {
	const extent = max - min;
	if (extent >= MIN_EXTENT) {
		return [min, max];
	}
	const mid = (min + max) / 2;
	return [mid - MIN_EXTENT / 2, mid + MIN_EXTENT / 2];
}

// viewBox fits boxes and padding into an SVG view box. Labels are excluded
// because their screen-relative size would create a circular fit calculation.
export function viewBox(boxes: Box[], pad: number): string {
	if (boxes.length === 0) {
		return `0 0 ${2 * pad} ${2 * pad}`;
	}
	let minX = Number.POSITIVE_INFINITY;
	let minY = Number.POSITIVE_INFINITY;
	let maxX = Number.NEGATIVE_INFINITY;
	let maxY = Number.NEGATIVE_INFINITY;
	for (const b of boxes) {
		minX = Math.min(minX, b.minX);
		minY = Math.min(minY, b.minY);
		maxX = Math.max(maxX, b.maxX);
		maxY = Math.max(maxY, b.maxY);
	}
	[minX, maxX] = floorAxis(minX, maxX);
	[minY, maxY] = floorAxis(minY, maxY);
	return `${minX - pad} ${minY - pad} ${maxX - minX + 2 * pad} ${maxY - minY + 2 * pad}`;
}

// scaleFor returns view-box units per screen pixel for SVG's default meet fit.
// It returns 1 until both boxes have measurable dimensions.
export function scaleFor(boxWidth: number, boxHeight: number, pxWidth: number, pxHeight: number): number {
	if (pxWidth <= 0 || pxHeight <= 0 || boxWidth <= 0 || boxHeight <= 0) {
		return 1;
	}
	return Math.max(boxWidth / pxWidth, boxHeight / pxHeight);
}

// Padding is capped against the tighter content axis so margins cannot dwarf
// the map on narrow panes.
const PAD_CLAMP_FRACTION = 0.25;

export function clampPad(boxes: Box[], requestedPad: number): number {
	if (boxes.length === 0) {
		return requestedPad;
	}
	const [, , w, h] = viewBox(boxes, 0).split(" ").map(Number);
	return Math.min(requestedPad, PAD_CLAMP_FRACTION * Math.min(w, h));
}

const MAX_LABEL_CHARS = 28;

export function truncateLabel(title: string): string {
	const codePoints = Array.from(title);
	if (codePoints.length <= MAX_LABEL_CHARS) {
		return title;
	}
	return `${codePoints.slice(0, MAX_LABEL_CHARS - 1).join("")}…`;
}
