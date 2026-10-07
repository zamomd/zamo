<script lang="ts">
	import type { Block } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
	import { base } from "../stores/base.svelte"
	import { jumpToBlock } from "./navigate"

	let { block }: { block: Block } = $props()

	const backlinks = $derived(base.backlinksById.get(block.ID) ?? [])
</script>

{#if backlinks.length > 0}
	<div class="backlinks">
		<p class="header">Linked references · {backlinks.length}</p>
		<ul>
			{#each backlinks as link (link.Block.ID)}
				<li>
					<button
						type="button"
						class="source"
						class:unreviewed={link.Block.Unreviewed}
						title="jump to this block"
						disabled={base.busy}
						onclick={() => jumpToBlock(link.Block.ID)}
					>
						<code>{link.Excerpt}</code>
						<span class="topic">{link.TopicTitle}</span>
					</button>
				</li>
			{/each}
		</ul>
	</div>
{/if}

<style>
	/* Same indent and quiet styling as HistoryPanel's .history: a small panel
	   hanging off this row, not a competing block. */
	.backlinks {
		margin: 0.1rem 0 0.25rem 1.25rem;
		padding: 0.2rem 0.4rem;
		font-size: var(--size-small);
		border-left: 2px solid var(--hairline);
	}
	.header {
		margin: 0 0 0.2rem;
		opacity: 0.6;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.source {
		display: flex;
		align-items: baseline;
		gap: 0.5rem;
		width: 100%;
		text-align: left;
		border: none;
		background: none;
		padding: 0.1rem 0.3rem;
		border-radius: var(--radius);
		cursor: pointer;
		color: inherit;
	}
	.source:hover:not(:disabled) {
		background: var(--hover);
	}
	/* One elided line per source; the full content lives where the jump goes. */
	.source code {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.source.unreviewed {
		border-left: 2px solid var(--text);
	}
	.topic {
		flex: none;
		opacity: 0.55;
		font-style: italic;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		max-width: 8rem;
	}
</style>
