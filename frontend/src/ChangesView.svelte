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

	// Batches: one row per distinct ApplyOps group across every unreviewed
	// block, regardless of which page it lands on — the compact "accept the
	// whole AI turn at once" affordance AcceptGroup exposes but nothing in
	// the UI called yet. Keyed by b.GroupID, NOT b.Head.GroupID: the two
	// diverge whenever a reviewed owner edit sits on top of a still-
	// unreviewed AI edit (Head is then the owner's revision, but GroupID/
	// GroupTitle name the AI batch actually awaiting review — see
	// UnreviewedBlock's doc). Keying by Head.GroupID would accept the wrong,
	// already-reviewed group: a no-op that leaves the row sitting there.
	// A block with no GroupID (should not happen for an unreviewed change,
	// but the field is technically optional) is skipped defensively rather
	// than grouped under an empty-string batch.
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

	// acceptBatch mirrors base's accept/reject discipline (busy flag, error
	// reset, status on success) even though the call itself is edge-shaped
	// like acceptEdge/rejectEdge above: it touches both blocks and edges of
	// the batch, so both the tree (base.reload) and the flat list
	// (changes.refresh) need to catch up.
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
			base.error = callError(err).message;
			base.status = "";
		} finally {
			base.busy = false;
		}
	}

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
									<button type="button" title="jump to this block in the base" onclick={() => onjump(b.Head.BlockID)}>show</button>
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
