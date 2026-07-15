<script lang="ts">
	import type { OpReview } from "../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import Diff from "./Diff.svelte";

	let {
		review,
		checked,
		cascaded,
		focused,
		showDiff,
		ontoggle,
	}: {
		review: OpReview;
		checked: boolean;
		cascaded: boolean; // selected by the user but dropped by the cascade
		focused: boolean;
		showDiff: boolean;
		ontoggle: () => void;
	} = $props();

	const op = $derived(review.Op);

	// summary is the one-line rendering of the operation.
	const summary = $derived.by(() => {
		switch (op.op) {
			case "add":
				return `add under ${op.parent === "" ? "(root)" : op.parent}: ${op.content}`;
			case "edit":
				return `edit ${op.block_id}`;
			case "move":
				return `move ${op.block_id} → ${op.parent === "" ? "(root)" : op.parent}`;
			case "delete":
				return `delete ${op.block_id}`;
			case "link":
				return `link ${op.from} → ${op.to} (${op.relation || "related"})`;
			default:
				return op.op;
		}
	});

	// labelText mirrors the label's visible content for aria-label: Biome's
	// a11y check can't statically resolve dynamic children, and aria-label
	// would otherwise hide the stale/cascade markers from assistive tech.
	const labelText = $derived(
		`${summary}${review.Stale ? " (stale)" : ""}${cascaded ? " (cascade)" : ""}`,
	);
</script>

<li class:focused class:cascaded>
	<label aria-label={labelText}>
		<input type="checkbox" {checked} disabled={cascaded} onchange={ontoggle} />
		<code>{summary}</code>
		{#if review.Stale}<mark class="stale">stale</mark>{/if}
		{#if cascaded}<em>(cascade)</em>{/if}
	</label>
	{#if op.origin || op.rationale}
		<p class="provenance">{op.origin}{op.origin && op.rationale ? " · " : ""}{op.rationale}</p>
	{/if}
	{#if showDiff && op.op === "edit"}
		<Diff before={review.CurrentContent} after={op.content ?? ""} />
	{/if}
</li>

<style>
	li {
		padding: 0.25rem 0.5rem;
		list-style: none;
	}
	li.focused {
		outline: 2px solid #36c;
	}
	li.cascaded {
		opacity: 0.45;
	}
	.provenance {
		margin: 0 0 0 1.5rem;
		font-size: 0.8em;
		color: #666;
	}
	.stale {
		background: #fc6;
		padding: 0 0.3em;
		margin-left: 0.4em;
	}
</style>
