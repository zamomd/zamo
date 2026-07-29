<script lang="ts">
	import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { base } from "../stores/base.svelte";
	import Diff from "./Diff.svelte";

	// The inline revision-history panel under a tree row: base.revisions is
	// newest first (BlockHistory's own order), so index 0 is always the head.
	// Deleting a revision IS the rollback mechanism (internal/sqlite's
	// DeleteRevision + projectHead), so each row's delete button doubles as
	// undo — the head's is labelled "revert" since that is what deleting it does.
	let { block }: { block: Block } = $props();

	const revisions = $derived(base.revisions);

	// Each revision diffs against the content of the revision right before it
	// in time — the next-older one, i.e. the next entry in this newest-first
	// list. The oldest revision (creation) has no older content to diff
	// against, so it diffs from empty: the whole block reads as added.
	function olderContent(i: number): string {
		return i + 1 < revisions.length ? revisions[i + 1].Content : "";
	}
</script>

<div class="history">
	<p class="hint">deleting the newest version reverts the block to the previous one</p>
	{#if revisions.length === 0}
		<p class="empty">(no history)</p>
	{:else}
		<ul>
			{#each revisions as rev, i (rev.ID)}
				<li class="entry" class:deleted={rev.Deleted}>
					<div class="meta">
						<span class="seq">#{rev.Seq}</span>
						<span class="author">{rev.Author} · {rev.Source}</span>
						{#if rev.Origin || rev.Rationale}
							<span class="rationale">{rev.Origin}{rev.Origin && rev.Rationale ? " · " : ""}{rev.Rationale}</span>
						{/if}
						<span class="created">{rev.CreatedAt}</span>
						{#if rev.Deleted}<mark class="deleted-tag">deleted</mark>{/if}
						<button
							type="button"
							class="delete"
							aria-label={i === 0 ? "revert to the previous version" : "delete this version"}
							title={i === 0 ? "revert to the previous version" : "delete this version"}
							disabled={base.busy}
							onclick={() => base.deleteRevision(block.ID, rev.ID)}
						>
							{i === 0 ? "revert" : "delete"}
						</button>
					</div>
					<Diff before={olderContent(i)} after={rev.Content} />
				</li>
			{/each}
		</ul>
	{/if}
</div>

<style>
	/* Indents under its row and stays quiet, matching BlockLinks: small text,
	   grey-alpha accents, theme-neutral. */
	.history {
		margin: 0.1rem 0 0.25rem 1.25rem;
		padding: 0.2rem 0.4rem;
		font-size: var(--size-small);
		border-left: 2px solid var(--hairline);
	}
	.hint {
		margin: 0 0 0.35rem;
		opacity: 0.55;
		font-style: italic;
	}
	.empty {
		margin: 0;
		opacity: 0.5;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.entry {
		padding: 0.2rem 0;
		border-bottom: 1px solid var(--hairline);
	}
	.entry:last-child {
		border-bottom: none;
	}
	.entry.deleted {
		opacity: 0.7;
	}
	.meta {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 0.5rem;
	}
	.seq {
		flex: none;
		opacity: 0.6;
		font-variant-numeric: tabular-nums;
	}
	.author {
		flex: none;
		opacity: 0.75;
	}
	.rationale {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		opacity: 0.6;
		font-style: italic;
	}
	.created {
		flex: none;
		opacity: 0.5;
		font-size: 0.8em;
	}
	.deleted-tag {
		flex: none;
		border: 1px solid var(--hairline);
		color: var(--text-dim);
		background: none;
		padding: 0 0.3em;
		border-radius: var(--radius);
	}
	.delete {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0.75;
	}
	.delete:hover:not(:disabled) {
		opacity: 1;
	}
</style>
