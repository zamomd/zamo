<script lang="ts">
	import type { ID, UnreviewedBlock } from "../bindings/github.com/zamomd/zamo/internal/knowledge/models.js";
	import { App } from "../bindings/github.com/zamomd/zamo/internal/wailsbind";
	import { messageFor } from "./lib/callerror";
	import ScheduledTab from "./lib/ScheduledTab.svelte";
	import StatusLine from "./lib/StatusLine.svelte";
	import { findBlock } from "./lib/tree";
	import { base } from "./stores/base.svelte";
	import { changes } from "./stores/changes.svelte";
	import { schedules } from "./stores/schedules.svelte";

	let { onjump }: { onjump: (id: ID) => void } = $props();

	let tab = $state<"review" | "scheduled">("review");
	let onlyTopic = $state<ID | null>(null);

	function reviewTopic(topicID: ID): void {
		onlyTopic = topicID;
		tab = "review";
	}

	const groups = $derived.by(() => {
		const map = new Map<ID, UnreviewedBlock[]>();
		for (const b of changes.blocks) {
			if (onlyTopic !== null && b.TopicID !== onlyTopic) continue;
			const list = map.get(b.TopicID) ?? [];
			list.push(b);
			map.set(b.TopicID, list);
		}
		return [...map.entries()];
	});

	const batches = $derived.by(() => {
		const map = new Map<ID, { title: string; count: number; atomic: boolean }>();
		for (const b of changes.blocks) {
			const groupID = b.GroupID;
			if (!groupID) continue;
			const entry = map.get(groupID);
			if (entry) {
				entry.count++;
			} else {
				map.set(groupID, { title: b.GroupTitle, count: 1, atomic: b.Atomic });
			}
		}
		return [...map.entries()];
	});

	// Accepting and rejecting a whole batch share everything but the call: both
	// touch every block in it, so the screen and the tree are re-read after.
	// Rejecting is UndoGroup — dropping an unreviewed batch's revisions is
	// exactly what rolling the group back does.
	async function reviewBatch(op: () => Promise<void>, done: string): Promise<void> {
		if (base.busy) return;
		base.busy = true;
		base.error = "";
		try {
			await op();
			await base.reload();
			await changes.refresh();
			base.status = done;
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
</script>

<section>
	<h1>Changes</h1>
	<StatusLine status={base.status} error={base.error} />

	<div class="tabs" role="tablist">
		<button type="button" role="tab" aria-selected={tab === "review"} class:active={tab === "review"} onclick={() => (tab = "review")}>
			awaiting review {changes.count}
		</button>
		<button
			type="button"
			role="tab"
			aria-selected={tab === "scheduled"}
			class:active={tab === "scheduled"}
			class:attention={schedules.failing > 0}
			onclick={() => (tab = "scheduled")}
		>
			scheduled {schedules.count}
		</button>
	</div>

	{#if tab === "scheduled"}
		<ScheduledTab {onjump} onreview={reviewTopic} />
	{:else if changes.count === 0}
		<p>(nothing awaiting review)</p>
	{:else}
		{#if onlyTopic !== null}
			<p class="filter">
				showing {topicLabel(onlyTopic)} only
				<button type="button" class="link" onclick={() => (onlyTopic = null)}>show all</button>
			</p>
		{/if}
		{#if batches.length > 0}
			<section class="group">
				<h2>Batches</h2>
				<ul>
					{#each batches as [groupID, info] (groupID)}
						<li class="row">
							<code class="snippet">{info.title || groupID}</code>
							<span class="meta">
								{info.count} block{info.count === 1 ? "" : "s"}
								{#if info.atomic}<mark class="atomic-tag">one change</mark>{/if}
							</span>
							<button
								type="button"
								class="accept"
								title="accept every change in this batch"
								disabled={base.busy}
								onclick={() => reviewBatch(() => App.AcceptGroup(groupID), "accepted batch")}
							>
								Accept batch
							</button>
							{#if changes.writtenOverGroups.has(groupID)}
								<!-- A later change was saved over this batch, so undoing it
								     would have to discard that change; the backend refuses
								     (ErrGroupWrittenOver) and the button could only fail. -->
								<span class="meta" title="a later edit was saved on top of this batch, so it can only be accepted now"
									>accept only</span
								>
							{:else}
								<button
									type="button"
									class="reject"
									title="reject every change in this batch"
									disabled={base.busy}
									onclick={() => reviewBatch(() => App.UndoGroup(groupID), "rejected batch")}
								>
									Reject batch
								</button>
							{/if}
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
									{#if b.Atomic}
										<!-- Deciding on this block alone is refused below, so the
										     buttons that would do it are not offered. -->
										<span class="meta">use the batch above</span>
									{:else}
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
									{/if}
								</li>
							{/each}
						</ul>
					</section>
				{/each}
			</div>
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
	.atomic-tag,
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
	.tabs {
		display: flex;
		gap: 0.25rem;
		margin-bottom: 0.75rem;
		border-bottom: 1px solid var(--hairline);
	}
	.tabs button {
		border: none;
		border-bottom: 2px solid transparent;
		background: none;
		padding: 0.2rem 0.6rem;
		cursor: pointer;
		color: var(--text-dim);
		font: inherit;
	}
	.tabs button.active {
		color: var(--text);
		border-bottom-color: var(--accent);
	}
	.tabs button.attention {
		color: var(--accent);
	}
	.filter {
		margin: 0 0 0.5rem;
		font-size: var(--size-small);
		color: var(--text-dim);
	}
	.link {
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: var(--accent);
		font: inherit;
	}
</style>
