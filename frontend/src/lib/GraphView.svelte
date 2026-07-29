<script lang="ts">
	import { onMount } from "svelte";
	import { graph } from "../stores/graph.svelte";
	import { circleBox, clampPad, radiusOf, reducedLayout, scaleFor, truncateLabel, viewBox } from "./graph";
	import { openTopic } from "./navigate";

	// The fixed screen-space allowance around the fitted circles, before
	// clampPad below has a chance to cap it. Real screen pixels, like
	// LABEL_GAP, so the margin reads the same regardless of zoom — see box.
	const PAD_PX = 24;
	// Vertical gap between a node's circle and its label's baseline, in real
	// screen pixels — see textScale below for why this is pixels and not
	// viewBox units.
	const LABEL_GAP = 12;

	onMount(() => {
		void graph.load();
	});

	// The SVG's own rendered size. viewBox fits the content, so the map's
	// on-screen scale (viewBox units per pixel) changes with the topic count —
	// a five-node map and a five-hundred-node map do not occupy the same
	// number of viewBox units per pixel. A <text> element's font-size is in
	// viewBox units like everything else in the SVG, so left alone it would
	// scale with that ratio: unreadably small on a large map, oversized on a
	// small one. svgPxWidth/svgPxHeight let scaleFor below undo exactly that
	// scaling, on whichever axis actually constrains the fit.
	let svgPxWidth = $state(0);
	let svgPxHeight = $state(0);

	const nodes = $derived(
		graph.topics.map((t) => {
			const p = graph.positions.get(t.ID) ?? { x: 0, y: 0 };
			return { id: t.ID, title: t.Title, label: truncateLabel(t.Title), x: p.x, y: p.y, r: radiusOf(t.Blocks) };
		}),
	);

	const lines = $derived(
		graph.edges
			.map((e) => ({ a: graph.positions.get(e.From), b: graph.positions.get(e.To) }))
			.filter((l): l is { a: { x: number; y: number }; b: { x: number; y: number } } => l.a !== undefined && l.b !== undefined),
	);

	// Only circles are fitted — never labels. A label has a fixed screen
	// size (see textScale/the label <g> below), so its extent in viewBox
	// units is inversely proportional to the scale; folding it into the box
	// that determines that same scale is positive feedback, and it diverged
	// on windows this app does not prevent (see task-5 report). A label
	// simply gets clipped by the SVG when a pane is too narrow to hold it —
	// bounded clipping of an annotation beats an unreadable map, and the
	// circles, which are the map, are never affected by a label's length.
	const circleBoxes = $derived(nodes.map((n) => circleBox(n.x, n.y, n.r)));

	// One-shot, not iterative: the circles-only scale converts PAD_PX to user
	// units exactly once, and that result feeds nothing back into itself.
	// clampPad then hard-caps it so padding can never dominate the fit
	// regardless of window shape — the guard the old iterative version
	// lacked.
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
		<!-- Independent facts about the base, not alternatives: a base can be both
		     link-free and above the reduced-layout threshold at once, and each
		     needs its own line rather than one hiding the other. Neither <p>
		     renders when its condition is false, rather than one empty <p> always
		     present — .graph's gap only applies between existing children, so an
		     absent note reserves no space of its own. -->
		{#if graph.edges.length === 0 && graph.topics.length > 1}
			<p class="note">No links between topics yet — links come from “+ link” or from MCP, never from an expansion.</p>
		{/if}
		{#if reducedLayout(graph.topics.length)}
			<p class="note">Large base: the layout ran fewer passes, so the arrangement is rougher.</p>
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
