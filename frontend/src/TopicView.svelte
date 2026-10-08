<script lang="ts">
	import { untrack } from 'svelte'
	import Breadcrumbs from './lib/Breadcrumbs.svelte'
	import DocView from './lib/DocView.svelte'
	import StatusLine from './lib/StatusLine.svelte'
	import TabStrip from './lib/TabStrip.svelte'
	import TopicProps from './lib/TopicProps.svelte'
	import TreeNode from './lib/TreeNode.svelte'
	import { ai } from './stores/ai.svelte'
	import { base } from './stores/base.svelte'
	import { tabs } from './stores/tabs.svelte'

	const tree = $derived(base.viewRoot)

	let scroller: HTMLElement | undefined = $state()

	$effect(() => {
		const tab = tabs.active
		if (scroller === undefined || tab === null || tab.topicID !== base.selectedTopicID) {
			return
		}
		void tab.id
		const el = scroller
		untrack(() => {
			if (base.highlightID === null) {
				el.scrollTop = tab.scrollTop
			}
		})
	})
</script>

<section>
	{#if tree === null}
		<p class="empty">
			{#if ai.enabled}
				No topic selected. Pick one in the sidebar, or ask the assistant (⌘K) to start one.
			{:else}
				No topic selected. Pick one in the sidebar, or add a topic there to start writing.
			{/if}
		</p>
	{:else}
		<TabStrip />
		<Breadcrumbs />
		{#if base.focusID === null}
			<TopicProps id={tree.block.ID} />
		{/if}
		<div class="tree" bind:this={scroller} onscroll={() => tabs.setScroll(scroller?.scrollTop ?? 0)}>
			{#if base.topicMode === "doc"}
				<DocView node={tree} />
			{:else}
				<ul>
					<TreeNode node={tree} title={true} />
				</ul>
			{/if}
		</div>
	{/if}

	<footer>
		<StatusLine error={base.error} />
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
