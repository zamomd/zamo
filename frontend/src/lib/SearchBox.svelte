<script lang="ts">
	import { search } from "../stores/search.svelte";

	// The search field and its result list; layout only, all state in the store.
	// Typing runs a live query, clicking a result jumps to that block in the tree.
</script>

<div class="search">
	<div class="field">
		<input
			type="search"
			aria-label="search the base"
			placeholder="search…"
			value={search.query}
			oninput={(e) => search.setQuery(e.currentTarget.value)}
		/>
		{#if search.query !== ""}
			<button type="button" class="clear" aria-label="clear search" onclick={() => search.clear()}>
				✕
			</button>
		{/if}
	</div>

	{#if search.error !== ""}
		<p class="error" role="alert">{search.error}</p>
	{:else if search.status !== ""}
		<p class="status">{search.status}</p>
	{/if}

	{#if search.results.length > 0}
		<ul class="results">
			{#each search.results as block (block.ID)}
				<li>
					<button type="button" class="result" title="jump to this block" onclick={() => search.jumpTo(block)}>
						<code>{block.Content}</code>
					</button>
				</li>
			{/each}
		</ul>
	{/if}
</div>

<style>
	.search {
		margin-bottom: 1rem;
	}
	.field {
		display: flex;
		gap: 0.5rem;
		align-items: center;
	}
	.field input {
		flex: 1;
	}
	.clear {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0.6;
	}
	.clear:hover {
		opacity: 1;
	}
	.status,
	.error {
		margin: 0.35rem 0 0;
		font-size: 0.85rem;
	}
	.status {
		opacity: 0.75;
	}
	.error {
		color: var(--danger);
	}
	.results {
		list-style: none;
		margin: 0.35rem 0 0;
		padding: 0;
		/* The list is ranked and can be long; cap it and let it scroll so it
		   never pushes the tree off-screen. */
		max-height: 14rem;
		overflow-y: auto;
		border: 1px solid var(--hairline);
		border-radius: var(--radius);
	}
	.result {
		display: block;
		width: 100%;
		text-align: left;
		border: none;
		border-radius: 0;
		background: none;
		padding: 0 var(--unit);
		line-height: var(--row-height);
		cursor: pointer;
		color: inherit;
	}
	.result:hover {
		background: var(--hover);
	}
	/* Keep each hit to a single elided line so the ranked order stays scannable;
	   the full content lives in the tree the click jumps to. */
	.result code {
		display: block;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
</style>
