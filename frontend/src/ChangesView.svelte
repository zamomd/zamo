<script lang="ts">
	import { SvelteSet } from "svelte/reactivity";
	import type { Edge, ID, UnreviewedBlock } from "../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";
	import { callError } from "./lib/callerror";
	import StatusLine from "./lib/StatusLine.svelte";
	import { findBlock } from "./lib/tree";
	import { base } from "./stores/base.svelte";
	import { changes } from "./stores/changes.svelte";

	// The Changes screen is a second read of the same unreviewed set the tree
	// renders inline: a flat, reviewable list for someone who wants to work
	// through everything at once instead of hunting rows across the base.
	// Block actions go through the base store (base.accept/reject) so the tree
	// stays in sync; edges have no tree row to sync with, so they call the
	// binding directly and just refresh changes.
	let { onjump }: { onjump: (id: ID) => void } = $props();

	// Blocks are already sorted by TopicID then BlockID (see ListUnreviewed);
	// grouping preserves that order, one section per page.
	const groups = $derived.by(() => {
		const map = new Map<ID, UnreviewedBlock[]>();
		for (const b of changes.blocks) {
			const list = map.get(b.TopicID) ?? [];
			list.push(b);
			map.set(b.TopicID, list);
		}
		return [...map.entries()];
	});

	// The topic's own content is not part of UnreviewedChanges (only the
	// touched blocks are); the tree already has it, including a ghost topic
	// root's — reload merges those in too (see base.svelte.ts).
	function topicLabel(topicID: ID): string {
		return findBlock(base.forest, topicID)?.Content ?? topicID;
	}

	// Edges are not covered by the base store: they never render as tree
	// rows, so their busy/error state lives here instead.
	let edgeBusy = new SvelteSet<string>();
	let edgeError = $state("");

	function edgeKey(e: Edge): string {
		return `${e.From} ${e.To} ${e.Relation}`;
	}

	async function acceptEdge(e: Edge): Promise<void> {
		const key = edgeKey(e);
		if (edgeBusy.has(key)) return;
		edgeBusy.add(key);
		edgeError = "";
		try {
			await App.AcceptEdge(e.From, e.To, e.Relation);
			await changes.refresh();
		} catch (err: unknown) {
			edgeError = callError(err).message;
		} finally {
			edgeBusy.delete(key);
		}
	}

	async function rejectEdge(e: Edge): Promise<void> {
		const key = edgeKey(e);
		if (edgeBusy.has(key)) return;
		edgeBusy.add(key);
		edgeError = "";
		try {
			await App.RejectEdge(e.From, e.To, e.Relation);
			await changes.refresh();
		} catch (err: unknown) {
			edgeError = callError(err).message;
		} finally {
			edgeBusy.delete(key);
		}
	}
</script>

<section>
	<h1>Changes</h1>
	<StatusLine status={base.status} error={base.error || edgeError} />

	{#if changes.count === 0}
		<p>(nothing awaiting review)</p>
	{:else}
		{#if groups.length > 0}
			<div class="blocks">
				{#each groups as [topicID, blocks] (topicID)}
					<section class="group">
						<h2>{topicLabel(topicID)}</h2>
						<ul>
							{#each blocks as b (b.Head.BlockID)}
								<li class="row" class:deleted={b.Head.Deleted}>
									<code class="snippet">{b.Head.Content}</code>
									<span class="meta">
										{b.GroupTitle}
										{#if b.Head.Deleted}<mark class="deleted-tag">deleted</mark>{/if}
									</span>
									<button type="button" onclick={() => onjump(b.Head.BlockID)}>show</button>
									<button
										type="button"
										class="accept"
										disabled={base.busy}
										onclick={() => base.accept(b.Head.BlockID)}
									>
										Accept
									</button>
									<button
										type="button"
										class="reject"
										disabled={base.busy}
										onclick={() => base.reject(b.Head.BlockID)}
									>
										Reject
									</button>
								</li>
							{/each}
						</ul>
					</section>
				{/each}
			</div>
		{/if}

		{#if changes.edges.length > 0}
			<section class="group">
				<h2>Links</h2>
				<ul>
					{#each changes.edges as e (edgeKey(e))}
						<li class="row">
							<code class="snippet">{e.From} → {e.To} ({e.Relation})</code>
							<button
								type="button"
								class="accept"
								disabled={edgeBusy.has(edgeKey(e))}
								onclick={() => acceptEdge(e)}
							>
								Accept
							</button>
							<button
								type="button"
								class="reject"
								disabled={edgeBusy.has(edgeKey(e))}
								onclick={() => rejectEdge(e)}
							>
								Reject
							</button>
						</li>
					{/each}
				</ul>
			</section>
		{/if}
	{/if}
</section>

<style>
	h1 {
		font-size: 1.3rem;
		margin: 0 0 0.75rem;
	}
	.group {
		margin-bottom: 1.25rem;
	}
	.group h2 {
		font-size: 1rem;
		margin: 0 0 0.35rem;
		opacity: 0.85;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.25rem 0;
		border-bottom: 1px solid rgba(127, 127, 127, 0.15);
	}
	.row.deleted {
		opacity: 0.6;
	}
	.snippet {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.meta {
		flex: none;
		font-size: 0.8rem;
		opacity: 0.7;
	}
	.deleted-tag {
		background: rgba(255, 165, 0, 0.4);
		padding: 0 0.3em;
		margin-left: 0.4em;
		border-radius: 3px;
	}
	.row button {
		flex: none;
		border: none;
		background: none;
		padding: 0.1rem 0.4rem;
		cursor: pointer;
		color: inherit;
		opacity: 0.75;
	}
	.row button:hover:not(:disabled) {
		opacity: 1;
	}
</style>
