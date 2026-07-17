<script lang="ts">
	import { onMount } from "svelte";
	import CommandBox from "./lib/CommandBox.svelte";
	import StatusLine from "./lib/StatusLine.svelte";
	import TreeNode from "./lib/TreeNode.svelte";
	import { base } from "./stores/base.svelte";

	// The Base view is layout only: the command box, the status line and the
	// tree, all wired to the base store. Mounting re-reads the data; the UI
	// state (fold state, target, command) lives in the store and survives tab
	// switches.
	let { onqueued }: { onqueued: () => void } = $props();

	onMount(() => {
		base.activate();
	});
</script>

<section>
	<h1>Comind</h1>

	<CommandBox {onqueued} />
	<StatusLine status={base.status} error={base.error} />

	{#if base.forest.length === 0}
		<p>(no topics yet)</p>
	{:else}
		<ul>
			{#each base.forest as tree (tree.block.ID)}
				<TreeNode node={tree} />
			{/each}
		</ul>
	{/if}
</section>

<style>
	h1 {
		font-size: 1.3rem;
		margin: 0 0 0.75rem;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
</style>
