<script lang="ts">
	import { tick } from "svelte";
	import { search } from "../stores/search.svelte";
	import { shell } from "../stores/shell.svelte";

	let field: HTMLInputElement | undefined = $state();

	$effect(() => {
		if (shell.searchFocusNonce > 0) {
			void tick().then(() => field?.focus());
		}
	});
</script>

<div class="field">
	<input
		type="search"
		bind:this={field}
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
