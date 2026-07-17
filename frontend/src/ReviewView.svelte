<script lang="ts">
	import { onMount } from "svelte";
	import OpRow from "./lib/OpRow.svelte";
	import StatusLine from "./lib/StatusLine.svelte";
	import { queue } from "./stores/queue.svelte";
	import { review } from "./stores/review.svelte";

	// The Review view is layout plus the keyboard map: the queue list and the
	// open proposal render straight from the shared queue store and the review
	// store. Mounting re-reads the queue; which proposal is open and where the
	// cursor sits live in the store and survive tab switches.

	// Keyboard: the issue's speed requirement. Inactive while typing in a field,
	// and bound only while this view is mounted.
	function onKey(e: KeyboardEvent): void {
		const t = e.target as HTMLElement;
		// Let native activation win for anything focusable/interactive (e.g. Tab
		// onto the Reject button, then Enter must click it, not applySelected).
		if (t.closest("input, textarea, button, select, [contenteditable]")) return;
		switch (e.key) {
			case "j":
			case "ArrowDown":
				review.move(1);
				break;
			case "k":
			case "ArrowUp":
				review.move(-1);
				break;
			case " ":
				review.toggle(review.cursor);
				break;
			case "a":
			case "Enter":
				review.applySelected();
				break;
			case "r":
				review.rejectCurrent();
				break;
			case "n":
				review.switchProposal(1);
				break;
			case "p":
				review.switchProposal(-1);
				break;
			case "d":
				review.toggleDiff(review.cursor);
				break;
			default:
				return;
		}
		e.preventDefault();
	}

	onMount(() => {
		review.activate().catch((err: unknown) => {
			review.error = String(err);
		});
	});
</script>

<svelte:window onkeydown={onKey} />

<section>
	<StatusLine status={review.notice} error={review.error} />

	{#if queue.pending.length === 0}
		<p>(queue empty)</p>
	{:else}
		<div class="split">
			<ul class="queue">
				{#each queue.pending as p, i (p.ID)}
					<li class:active={i === review.index}>
						<button type="button" onclick={() => review.switchProposal(i - review.index)}>
							{p.Title} <small>{p.Source} · {(p.Ops ?? []).length} ops</small>
						</button>
					</li>
				{/each}
			</ul>

			{#if review.detail}
				<div class="proposal">
					<h2>{review.detail.Proposal.Title}</h2>
					<ul>
						{#each review.detail.Ops ?? [] as r, i (i)}
							<OpRow
								review={r}
								checked={review.effective.includes(i)}
								cascaded={review.selected.includes(i) && !review.effective.includes(i)}
								focused={i === review.cursor}
								showDiff={review.openDiffs.has(i)}
								ontoggle={() => review.toggle(i)}
							/>
						{/each}
					</ul>
					<p>
						<button
							type="button"
							onclick={() => review.applySelected()}
							disabled={review.busy || review.effective.length === 0}
						>
							Apply ({review.effective.length})
						</button>
						<button type="button" onclick={() => review.rejectCurrent()} disabled={review.busy}>
							Reject
						</button>
						<small>j/k · Space · a/Enter apply · r reject · n/p proposal · d diff</small>
					</p>
				</div>
			{/if}
		</div>
	{/if}
</section>

<style>
	.split {
		display: flex;
		gap: 1.5rem;
		align-items: flex-start;
	}
	.queue {
		min-width: 14rem;
		padding: 0;
		margin: 0;
	}
	.queue li {
		list-style: none;
	}
	.queue li.active button {
		font-weight: bold;
	}
	.proposal {
		flex: 1;
	}
	.proposal ul {
		padding: 0;
	}
</style>
