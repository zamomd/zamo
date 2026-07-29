<script lang="ts">
	import { search } from "../stores/search.svelte";

	// The field only. Results render in the sidebar, replacing the topic list,
	// so searching never opens a second panel over the tree.
</script>

<div class="field">
	<input
		type="search"
		aria-label="search the base"
		placeholder="search"
		value={search.query}
		oninput={(e) => search.setQuery(e.currentTarget.value)}
	/>
	{#if search.query !== ""}
		<button type="button" class="clear" aria-label="clear search" onclick={() => search.clear()}>✕</button>
	{/if}
</div>

<style>
	.field {
		display: flex;
		gap: var(--unit);
		align-items: center;
	}
	.field input {
		flex: 1;
		min-width: 0;
		line-height: calc(var(--row-height) * 1.2);
	}
	.clear {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: var(--text-dim);
	}
	.clear:hover {
		color: var(--text);
		background: none;
	}
</style>
