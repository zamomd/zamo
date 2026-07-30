<script lang="ts">
	import Breadcrumbs from "./lib/Breadcrumbs.svelte";
	import CommandBox from "./lib/CommandBox.svelte";
	import StatusLine from "./lib/StatusLine.svelte";
	import TreeNode from "./lib/TreeNode.svelte";
	import { base } from "./stores/base.svelte";

	// One topic, not the forest: the sidebar is the index, so repeating every
	// topic here would make the same list twice. The tree scrolls; the command
	// line stays pinned to the bottom, because it is the entry point for the
	// next thing the reader writes and hunting for it would break that.
	//
	// The view's root is whatever the reader is looking at — the topic, or a
	// block they zoomed into — and it renders as the page's title row rather
	// than as a heading above a duplicate of itself.
	const tree = $derived(base.viewRoot);
</script>

<section>
	{#if tree === null}
		<p class="empty">No topic selected. Ask the AI below to expand one.</p>
	{:else}
		<Breadcrumbs />
		<div class="tree">
			<ul>
				<TreeNode node={tree} title={true} />
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
