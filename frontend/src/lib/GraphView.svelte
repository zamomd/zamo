<script lang="ts">
	import { onMount } from "svelte";
	import { graph } from "../stores/graph.svelte";
	import { circleBox, clampPad, radiusOf, scaleFor, truncateLabel, viewBox } from "./graph";
	import { openTopic } from "./navigate";

	const PAD_PX = 24;
	const LABEL_GAP = 12;

	onMount(() => {
		void graph.load();
	});

	let svgPxWidth = $state(0);
	let svgPxHeight = $state(0);

	const UNTITLED = "Untitled topic";

	const nodes = $derived(
		graph.topics.map((t) => {
			const p = graph.positions.get(t.ID) ?? { x: 0, y: 0 };
			const title = t.Title === "" ? UNTITLED : t.Title;
			return { id: t.ID, title, label: truncateLabel(title), x: p.x, y: p.y, r: radiusOf(t.Blocks) };
		}),
	);

	const lines = $derived(
		graph.edges
			.map((e) => ({ a: graph.positions.get(e.From), b: graph.positions.get(e.To) }))
			.filter((l): l is { a: { x: number; y: number }; b: { x: number; y: number } } => l.a !== undefined && l.b !== undefined),
	);

	const circleBoxes = $derived(nodes.map((n) => circleBox(n.x, n.y, n.r)));

	const box = $derived.by(() => {
		const [, , w0, h0] = viewBox(circleBoxes, 0).split(" ").map(Number);
		const scale = scaleFor(w0, h0, svgPxWidth, svgPxHeight);
		const pad = clampPad(circleBoxes, PAD_PX * scale);
		return viewBox(circleBoxes, pad);
	});

	const textScale = $derived.by(() => {
		const [, , w, h] = box.split(" ").map(Number);
		return scaleFor(w, h, svgPxWidth, svgPxHeight);
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
		<svg viewBox={box} role="group" aria-label="Topic map" bind:clientWidth={svgPxWidth} bind:clientHeight={svgPxHeight}>
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
						<text y={LABEL_GAP} text-anchor="middle">{n.label}</text>
					</g>
				</g>
			{/each}
		</svg>
		{#if graph.edges.length === 0 && graph.topics.length > 1}
			<p class="note">
				No links between topics yet — an edge appears once a block references another topic, by name in
				[[brackets]] or by block id.
			</p>
		{/if}
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
		/* A label is wider than its own node and routinely lies over a
		   neighbour's circle. Left hit-testable, it would take the click meant
		   for the circle underneath it and open its own topic instead — the
		   reader aims at one node and lands on another. Opening a topic by
		   clicking its name is worth less than never opening the wrong one. */
		pointer-events: none;
	}
	.node:hover circle,
	.node:focus-visible circle {
		stroke: var(--accent);
	}
	/* Hover has the pointer itself as a second cue; keyboard focus has nothing
	   but this ring, so colour alone — a 1px hairline swapping hue — is easy to
	   lose on a dense map. A heavier stroke gives focus a cue that does not
	   depend on colour perception, without adding a fill the house style
	   reserves for nothing on a control. */
	.node:focus-visible circle {
		stroke-width: 2.5;
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
