<script lang="ts">
	import { SvelteSet } from "svelte/reactivity";
	import type { Edge, ID, UnreviewedBlock } from "../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";
	import { messageFor } from "./lib/callerror";
	import StatusLine from "./lib/StatusLine.svelte";
	import { findBlock } from "./lib/tree";
	import { base } from "./stores/base.svelte";
	import { changes } from "./stores/changes.svelte";

	let { onjump }: { onjump: (id: ID) => void } = $props();

	const groups = $derived.by(() => {
		const map = new Map<ID, UnreviewedBlock[]>();
		for (const b of changes.blocks) {
			const list = map.get(b.TopicID) ?? [];
			list.push(b);
			map.set(b.TopicID, list);
		}
		return [...map.entries()];
	});

	const batches = $derived.by(() => {
		const map = new Map<ID, { title: string; count: number }>();
		for (const b of changes.blocks) {
			const groupID = b.GroupID;
			if (!groupID) continue;
			const entry = map.get(groupID);
			if (entry) {
				entry.count++;
			} else {
				map.set(groupID, { title: b.GroupTitle, count: 1 });
			}
		}
		return [...map.entries()];
	});

	async function acceptBatch(groupID: ID): Promise<void> {
		if (base.busy) return;
		base.busy = true;
		base.error = "";
		try {
			await App.AcceptGroup(groupID);
			await base.reload();
			await changes.refresh();
			base.status = "accepted batch";
		} catch (err: unknown) {
			base.error = messageFor(err);
			base.status = "";
		} finally {
			base.busy = false;
		}
	}

	function topicLabel(topicID: ID): string {
		return findBlock(base.forest, topicID)?.Content ?? topicID;
	}

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
			edgeError = messageFor(err);
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
			edgeError = messageFor(err);
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
		{#if batches.length > 0}
			<section class="group">
				<h2>Batches</h2>
				<ul>
					{#each batches as [groupID, info] (groupID)}
						<li class="row">
							<code class="snippet">{info.title || groupID}</code>
							<span class="meta">{info.count} block{info.count === 1 ? "" : "s"}</span>
							<button
								type="button"
								class="accept"
								title="accept every change in this batch, blocks and links alike"
								disabled={base.busy}
								onclick={() => acceptBatch(groupID)}
							>
								Accept batch
							</button>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

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
								<button
									type="button"
									title="jump to this block in the base"
									disabled={base.busy}
									onclick={() => onjump(b.Head.BlockID)}>show</button>
									<button
										type="button"
										class="accept"
										title="accept this change"
										disabled={base.busy}
										onclick={() => base.accept(b.Head.BlockID)}
									>
										Accept
									</button>
									<button
										type="button"
										class="reject"
										title={b.Head.Deleted ? "restore this block" : "reject: revert to the last reviewed version"}
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
								title="accept this link"
								disabled={edgeBusy.has(edgeKey(e))}
								onclick={() => acceptEdge(e)}
							>
								Accept
							</button>
							<button
								type="button"
								class="reject"
								title="reject and remove this link"
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
		font-size: var(--size-title);
		line-height: calc(var(--row-height) * 1.5);
		margin: 0 0 0.75rem;
	}
	.group {
		margin-bottom: 1.25rem;
	}
	.group h2 {
		font-size: var(--size-heading);
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
		border-bottom: 1px solid var(--hairline);
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
		font-size: var(--size-small);
		opacity: 0.7;
	}
	.deleted-tag {
		border: 1px solid var(--hairline);
		color: var(--text-dim);
		background: none;
		padding: 0 0.3em;
		margin-left: 0.4em;
		border-radius: var(--radius);
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
