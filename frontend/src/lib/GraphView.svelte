<script lang="ts">
	import { onMount } from "svelte";
	import { graph } from "../stores/graph.svelte";
	import { radiusOf, reducedLayout, viewBox } from "./graph";
	import { openTopic } from "./navigate";

	// Padding around the fitted bounding box, in the SVG's own units.
	const PAD = 24;
	// Vertical gap between a node's circle and its label, in real screen
	// pixels — see textScale below for why this is pixels and not viewBox units.
	const LABEL_GAP = 12;

	onMount(() => {
		void graph.load();
	});

	// The SVG's own rendered width. viewBox fits the content, so the map's
	// on-screen scale (viewBox units per pixel) changes with the topic count —
	// a five-node map and a five-hundred-node map do not occupy the same
	// number of viewBox units per pixel. A <text> element's font-size is in
	// viewBox units like everything else in the SVG, so left alone it would
	// scale with that ratio: unreadably small on a large map, oversized on a
	// small one. svgPxWidth lets textScale below undo exactly that scaling.
	let svgPxWidth = $state(0);

	const nodes = $derived(
		graph.topics.map((t) => {
			const p = graph.positions.get(t.ID) ?? { x: 0, y: 0 };
			return { id: t.ID, title: t.Title, x: p.x, y: p.y, r: radiusOf(t.Blocks) };
		}),
	);

	const lines = $derived(
		graph.edges
			.map((e) => ({ a: graph.positions.get(e.From), b: graph.positions.get(e.To) }))
			.filter((l): l is { a: { x: number; y: number }; b: { x: number; y: number } } => l.a !== undefined && l.b !== undefined),
	);

	const box = $derived(viewBox(nodes, PAD));

	// viewBox is "minX minY width height"; the third field is the box's width
	// in viewBox units. Dividing it by the SVG's rendered pixel width gives
	// viewBox-units-per-pixel — the factor a <g> around a label must be scaled
	// by to cancel the SVG's own zoom and keep the label a constant size on
	// screen. Before the first layout pass svgPxWidth is 0, so this falls back
	// to 1 (no correction) rather than dividing by zero.
	const textScale = $derived.by(() => {
		const boxWidth = Number(box.split(" ")[2]);
		return svgPxWidth > 0 && boxWidth > 0 ? boxWidth / svgPxWidth : 1;
	});
</script>

<div class="graph">
	{#if graph.error !== ""}
		<p class="error">{graph.error}</p>
	{:else if graph.loading && graph.topics.length === 0}
		<p class="note">reading the base…</p>
	{:else if graph.topics.length === 0}
		<p class="note">No topics yet.</p>
	{:else}
		<!-- biome-ignore lint/a11y/useSemanticElements: an <svg> cannot be a <fieldset>; role="group" is the ARIA equivalent for a collection of graphical controls -->
		<svg viewBox={box} role="group" aria-label="Topic map" bind:clientWidth={svgPxWidth}>
			{#each lines as l (`${l.a.x},${l.a.y},${l.b.x},${l.b.y}`)}
				<line class="edge" x1={l.a.x} y1={l.a.y} x2={l.b.x} y2={l.b.y} />
			{/each}
			{#each nodes as n (n.id)}
				<!-- A node is a control, not decoration: SVG cannot hold a <button>,
				     so the role, the tab stop and the keyboard handler are explicit. -->
				<!-- biome-ignore lint/a11y/useSemanticElements: <g> is an SVG grouping element and cannot be a <button>; role="button" plus tabindex and the keydown handler below reproduce button semantics -->
				<g
					class="node"
					role="button"
					tabindex="0"
					aria-label={n.title}
					onclick={() => openTopic(n.id)}
					onkeydown={(e) => {
						if (e.key === "Enter" || e.key === " ") {
							e.preventDefault();
							openTopic(n.id);
						}
					}}
				>
					<circle cx={n.x} cy={n.y} r={n.r} />
					<!-- The label lives in its own group so it can be counter-scaled by
					     textScale independently of the circle: the circle's radius is
					     meaningful map data (block count) and must stay in viewBox
					     units, but the label is UI chrome and must not. -->
					<g transform="translate({n.x} {n.y + n.r}) scale({textScale})">
						<text y={LABEL_GAP} text-anchor="middle">{n.title}</text>
					</g>
				</g>
			{/each}
		</svg>
		<p class="note">
			{#if graph.edges.length === 0 && graph.topics.length > 1}
				No links between topics yet — links come from “+ link” or from MCP, never from an expansion.
			{:else if reducedLayout(graph.topics.length)}
				Large base: the layout ran fewer passes, so the arrangement is rougher.
			{/if}
		</p>
	{/if}
</div>

<style>
	.graph {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		gap: var(--unit);
	}
	svg {
		flex: 1;
		min-height: 0;
		width: 100%;
	}
	.edge {
		stroke: var(--rail);
		stroke-width: 1;
	}
	.node circle {
		fill: none;
		/* A fill of none only paints the stroke, and SVG hit-tests fill and
		   stroke separately: without this, the circle's interior — most of a
		   node's visible area — would not receive pointer events at all,
		   leaving only a hairline-wide ring around the edge clickable. */
		pointer-events: all;
		stroke: var(--hairline-strong);
		stroke-width: 1;
	}
	.node text {
		fill: var(--text-dim);
		font-size: var(--size-small);
	}
	.node:hover circle,
	.node:focus-visible circle {
		stroke: var(--accent);
	}
	.node:hover text,
	.node:focus-visible text {
		fill: var(--text);
	}
	.node {
		cursor: pointer;
		outline: none;
	}
	.note {
		margin: 0;
		color: var(--text-faint);
		font-size: var(--size-small);
	}
	.error {
		margin: 0;
		color: var(--danger);
	}
</style>
