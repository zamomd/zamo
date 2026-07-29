<script lang="ts">
	import {
		type Block,
		type LinkedBlock,
		Relation,
	} from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { base } from "../stores/base.svelte";

	// The inline links panel under a tree row: the block's typed links (with
	// jump and unlink) plus the +link picker. All state and actions live in
	// the base store; the block is the only input.
	let { block }: { block: Block } = $props();

	const links = $derived(base.linksById.get(block.ID) ?? []);
	const picking = $derived(base.linkingID === block.ID);

	// An edge is identified by (endpoints, relation); direction disambiguates
	// a mutual pair of links between the same two blocks.
	const linkKey = (l: LinkedBlock) => `${l.Relation}:${l.Outgoing ? "out" : "in"}:${l.Block.ID}`;
</script>

<div class="links">
	{#if links.length === 0}
		<p class="empty">(no links)</p>
	{:else}
		<ul>
			{#each links as link (linkKey(link))}
				<li>
					<span class="arrow" title={link.Outgoing ? "outgoing" : "incoming"}>
						{link.Outgoing ? "↗" : "↙"}
					</span>
					<span class="relation">{link.Relation}</span>
					<button
						type="button"
						class="neighbour"
						title="jump to this block"
						onclick={() => base.reveal(link.Block.ID)}
					>
						<code>{link.Block.Content}</code>
					</button>
					<button
						type="button"
						class="unlink"
						aria-label="remove this link"
						disabled={base.busy}
						onclick={() => base.removeLink(block.ID, link)}
					>
						✕
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	{#if picking}
		<div class="picker">
			<input
				type="search"
				placeholder="link to…"
				aria-label="search for a block to link to"
				value={base.linkQuery}
				oninput={(e) => base.setLinkQuery(e.currentTarget.value)}
			/>
			<select aria-label="link relation" bind:value={base.linkRelation}>
				<option value={Relation.RelationRelated}>related</option>
				<option value={Relation.RelationExample}>example</option>
			</select>
			<button type="button" class="cancel" onclick={() => base.cancelLink()}>cancel</button>
		</div>
		{#if base.linkResults.length > 0}
			<ul class="results">
				{#each base.linkResults as hit (hit.ID)}
					<li>
						<button
							type="button"
							class="result"
							title="link to this block"
							disabled={base.busy}
							onclick={() => base.createLink(hit)}
						>
							<code>{hit.Content}</code>
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	{:else}
		<button
			type="button"
			class="add"
			disabled={base.busy || base.editingID !== null}
			onclick={() => base.startLink(block.ID)}
		>
			+ link
		</button>
	{/if}
</div>

<style>
	/* The panel indents under its row and stays quiet: small text, grey-alpha
	   accents, theme-neutral like the rest of the tree. */
	.links {
		margin: 0.1rem 0 0.25rem 1.25rem;
		padding: 0.2rem 0.4rem;
		font-size: var(--size-small);
		border-left: 2px solid var(--hairline);
	}
	.links ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.links li {
		display: flex;
		align-items: baseline;
		gap: 0.35rem;
	}
	.empty {
		margin: 0;
		opacity: 0.5;
	}
	.arrow {
		flex: none;
		opacity: 0.6;
	}
	.relation {
		flex: none;
		opacity: 0.6;
		font-style: italic;
	}
	.neighbour {
		flex: 1;
		min-width: 0;
		text-align: left;
		border: none;
		background: none;
		padding: 0.1rem 0.3rem;
		border-radius: var(--radius);
		cursor: pointer;
		color: inherit;
	}
	.neighbour:hover {
		background: var(--hover);
	}
	/* One elided line per neighbour; the full content lives where the jump goes. */
	.neighbour code {
		display: block;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.unlink {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0;
	}
	.links li:hover .unlink,
	.links li:focus-within .unlink {
		opacity: 0.6;
	}
	.unlink:hover:not(:disabled) {
		opacity: 1;
	}
	.add,
	.cancel {
		border: none;
		background: none;
		padding: 0.1rem 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0.6;
	}
	.add:hover:not(:disabled),
	.cancel:hover {
		opacity: 1;
	}
	.picker {
		display: flex;
		gap: 0.35rem;
		align-items: center;
		margin-top: 0.2rem;
	}
	.picker input {
		flex: 1;
		min-width: 0;
	}
	.results {
		max-height: 10rem;
		overflow-y: auto;
		margin-top: 0.25rem;
		border: 1px solid var(--hairline);
		border-radius: var(--radius);
	}
	.result {
		display: block;
		width: 100%;
		text-align: left;
		border: none;
		background: none;
		padding: 0.25rem 0.5rem;
		cursor: pointer;
		color: inherit;
	}
	.result:hover:not(:disabled) {
		background: var(--hover);
	}
	.result code {
		display: block;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
</style>
