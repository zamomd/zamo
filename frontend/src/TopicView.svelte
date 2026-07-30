<script lang="ts">
	import CommandBox from "./lib/CommandBox.svelte";
	import StatusLine from "./lib/StatusLine.svelte";
	import TreeNode from "./lib/TreeNode.svelte";
	import { base } from "./stores/base.svelte";

	// One topic, not the forest: the sidebar is the index, so repeating every
	// topic here would make the same list twice. The tree scrolls; the command
	// line stays pinned to the bottom, because it is the entry point for the
	// next thing the reader writes and hunting for it would break that.
	const tree = $derived(base.selectedTree);
</script>

<section>
	{#if tree === null}
		<p class="empty">No topic selected. Ask the AI below to expand one.</p>
	{:else}
		<h1>{tree.block.Content}</h1>
		<div class="tree">
			<ul>
				<TreeNode node={tree} />
			</ul>
		</div>
	{/if}

	<footer>
		<StatusLine status={base.status} error={base.error} />
		<CommandBox />
	</footer>
</section>

<style>
	section {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}
	h1 {
		flex: none;
		font-size: var(--size-title);
		line-height: calc(var(--row-height) * 1.5);
		margin: 0 0 var(--unit);
	}
	.tree {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		scrollbar-gutter: stable;
	}
	.tree ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.empty {
		flex: 1;
		margin: 0;
		color: var(--text-faint);
	}
	footer {
		flex: none;
		padding-top: var(--unit);
	}
</style>
