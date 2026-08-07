<script lang="ts">
	import Breadcrumbs from "./lib/Breadcrumbs.svelte";
	import CommandBox from "./lib/CommandBox.svelte";
	import DocView from "./lib/DocView.svelte";
	import StatusLine from "./lib/StatusLine.svelte";
	import TreeNode from "./lib/TreeNode.svelte";
	import { ai } from "./stores/ai.svelte";
	import { base } from "./stores/base.svelte";

	const tree = $derived(base.viewRoot);
</script>

<section>
	{#if tree === null}
		<p class="empty">
			{#if ai.enabled}
				No topic selected. Ask the AI below to expand one.
			{:else}
				No topic selected. Pick one in the sidebar, or add a topic there to start writing.
			{/if}
		</p>
	{:else}
		<Breadcrumbs />
		{#if base.topicMode === "doc"}
			<div class="tree"><DocView block={tree.block} /></div>
		{:else}
			<div class="tree">
				<ul>
					<TreeNode node={tree} title={true} />
				</ul>
			</div>
		{/if}
	{/if}

	<footer>
		<StatusLine status={base.status} error={base.error} />
		{#if ai.enabled}
			<CommandBox />
		{/if}
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
