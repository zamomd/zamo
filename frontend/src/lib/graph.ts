import { forceLink, forceManyBody, forceSimulation, forceX, forceY, type SimulationNodeDatum } from "d3-force";

export type Point = { x: number; y: number };

// Structural inputs keep the layout independent of generated bindings.
export type LayoutTopic = { ID: string };
export type LayoutEdge = { From: string; To: string };

// Repulsion separates topics, links cluster them, and the pull toward the
// origin bounds disconnected components.
const REPULSION = -220;
const LINK_DISTANCE = 90;
const CENTERING = 0.04;
const TICKS = 300;

type SimNode = SimulationNodeDatum & { id: string };

// layout is deterministic, so a map stays put across renders: d3-force seeds
// positions from the input order and draws its jitter from a fixed-seed
// generator.
export function layout(topics: LayoutTopic[], edges: LayoutEdge[]): Map<string, Point> {
	const nodes: SimNode[] = topics.map((t) => ({ id: t.ID }));
	const known = new Set(nodes.map((n) => n.id));
	// forceLink throws on an endpoint it cannot find.
	const links = edges
		.filter((e) => e.From !== e.To && known.has(e.From) && known.has(e.To))
		.map((e) => ({ source: e.From, target: e.To }));

	forceSimulation(nodes)
		.force("charge", forceManyBody().strength(REPULSION))
		.force(
			"link",
			forceLink<SimNode, { source: string; target: string }>(links)
				.id((n) => n.id)
				.distance(LINK_DISTANCE),
		)
		.force("x", forceX().strength(CENTERING))
		.force("y", forceY().strength(CENTERING))
		.stop()
		.tick(TICKS);

	return new Map(nodes.map((n) => [n.id, { x: n.x ?? 0, y: n.y ?? 0 }]));
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
