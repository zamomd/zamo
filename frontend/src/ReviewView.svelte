<script lang="ts">
	import { onMount } from "svelte";
	import type {
		Proposal,
		ProposalReview,
	} from "../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";
	import OpRow from "./OpRow.svelte";

	let { onchanged }: { onchanged: () => void } = $props();

	let queue: Proposal[] = $state([]);
	let current = $state(0); // index into queue
	let review: ProposalReview | null = $state(null);
	let selected: number[] = $state([]); // the user's manual selection
	let effective: number[] = $state([]); // selection after the domain cascade
	let cursor = $state(0); // focused op row
	let openDiffs: Set<number> = $state(new Set());
	let notice = $state("");
	let error = $state("");
	let busy = $state(false);

	// Monotonic generation counter guarding against out-of-order async
	// resolution: open()/toggle()/switchProposal()/loadQueue() each await a
	// Wails call, and overlapping calls (key-repeat on n/p, fast Space) could
	// otherwise let the last-resolving call win over the last-issued one.
	// Plain let (not $state) — it's a coordination token, not render state.
	let seq = 0;

	// gen lets a caller that already claimed a generation (applySelected,
	// rejectCurrent, switchProposal) hand it down instead of minting a new
	// one, so the whole chain of awaits is invalidated together.
	async function loadQueue(preferID?: string, gen?: number): Promise<void> {
		const my = gen ?? ++seq;
		const q = (await App.PendingProposals()) ?? [];
		if (my !== seq) return;
		queue = q;
		if (queue.length === 0) {
			review = null;
			return;
		}
		const i = preferID ? queue.findIndex((p) => p.ID === preferID) : -1;
		current = i >= 0 ? i : Math.min(current, queue.length - 1);
		await open(queue[current].ID, my);
	}

	// open loads one proposal's annotated view. Stale ops start deselected.
	async function open(id: string, gen?: number): Promise<void> {
		const my = gen ?? ++seq;
		const r = await App.ReviewProposal(id);
		if (my !== seq) return;
		review = r;
		const ops = r.Ops ?? [];
		selected = ops.map((_, i) => i).filter((i) => !ops[i].Stale);
		const eff = (await App.EffectiveSelection(r.Proposal.Ops ?? [], selected)) ?? [];
		if (my !== seq) return;
		effective = eff;
		cursor = 0;
		openDiffs = new Set();
		notice = "";
	}

	async function toggle(i: number): Promise<void> {
		if (!review) return;
		const my = ++seq;
		selected = selected.includes(i) ? selected.filter((x) => x !== i) : [...selected, i];
		const eff = (await App.EffectiveSelection(review.Proposal.Ops ?? [], selected)) ?? [];
		if (my !== seq) return;
		effective = eff;
	}

	async function applySelected(): Promise<void> {
		if (!review || busy || effective.length === 0 || selected.length === 0) return;
		busy = true;
		const my = ++seq;
		error = "";
		// Pick the neighbor to land on before the apply mutates the queue:
		// the next proposal, else the previous, else none (loadQueue falls
		// back to a clamped index). Guard against selected being emptied while
		// EffectiveSelection is still in flight.
		const nextID = queue[current + 1]?.ID ?? queue[current - 1]?.ID;
		try {
			await App.ApplyProposal(review.Proposal.ID, selected);
			if (my !== seq) return;
			await loadQueue(nextID, my);
			onchanged();
			notice = "applied";
		} catch (err: unknown) {
			if (my !== seq) return;
			const msg = String(err);
			// The Wails JS runtime surfaces a rejected Go call by throwing the
			// raw error-body text as the message (undocumented behavior), so
			// these substrings must keep matching the Go domain error text in
			// internal/knowledge/errors.go: "proposal is stale" and "proposal
			// already decided".
			if (msg.includes("stale")) {
				// The base moved under the proposal: re-read, fresh flags deselect
				// what vanished.
				await open(review.Proposal.ID, my);
				notice = "base changed — check again";
			} else if (msg.includes("already decided")) {
				await loadQueue(nextID, my);
				onchanged();
			} else {
				error = msg;
			}
		} finally {
			busy = false;
		}
	}

	async function rejectCurrent(): Promise<void> {
		if (!review || busy) return;
		busy = true;
		const my = ++seq;
		error = "";
		// Same neighbor-selection logic as applySelected: compute before the
		// reject call mutates the queue.
		const nextID = queue[current + 1]?.ID ?? queue[current - 1]?.ID;
		try {
			await App.RejectProposal(review.Proposal.ID);
			if (my !== seq) return;
			await loadQueue(nextID, my);
			onchanged();
			notice = "rejected";
		} catch (err: unknown) {
			if (my !== seq) return;
			error = String(err);
		} finally {
			busy = false;
		}
	}

	function move(delta: number): void {
		const n = review?.Ops?.length ?? 0;
		if (n > 0) cursor = Math.min(n - 1, Math.max(0, cursor + delta));
	}

	async function switchProposal(delta: number): Promise<void> {
		if (queue.length === 0) return;
		const my = ++seq;
		current = Math.min(queue.length - 1, Math.max(0, current + delta));
		await open(queue[current].ID, my);
	}

	function toggleDiff(i: number): void {
		const next = new Set(openDiffs);
		if (next.has(i)) next.delete(i);
		else next.add(i);
		openDiffs = next;
	}

	// Keyboard: the issue's speed requirement. Inactive while typing in a field.
	function onKey(e: KeyboardEvent): void {
		const t = e.target as HTMLElement;
		// Let native activation win for anything focusable/interactive (e.g. Tab
		// onto the Reject button, then Enter must click it, not applySelected).
		if (t.closest("input, textarea, button, select, [contenteditable]")) return;
		switch (e.key) {
			case "j":
			case "ArrowDown":
				move(1);
				break;
			case "k":
			case "ArrowUp":
				move(-1);
				break;
			case " ":
				toggle(cursor);
				break;
			case "a":
			case "Enter":
				applySelected();
				break;
			case "r":
				rejectCurrent();
				break;
			case "n":
				switchProposal(1);
				break;
			case "p":
				switchProposal(-1);
				break;
			case "d":
				toggleDiff(cursor);
				break;
			default:
				return;
		}
		e.preventDefault();
	}

	onMount(() => {
		loadQueue().catch((err: unknown) => {
			error = String(err);
		});
	});
</script>

<svelte:window onkeydown={onKey} />

<section>
	{#if notice !== ""}<p><code>{notice}</code></p>{/if}
	{#if error !== ""}<p><code>error: {error}</code></p>{/if}

	{#if queue.length === 0}
		<p>(queue empty)</p>
	{:else}
		<div class="split">
			<ul class="queue">
				{#each queue as p, i (p.ID)}
					<li class:active={i === current}>
						<button type="button" onclick={() => switchProposal(i - current)}>
							{p.Title} <small>{p.Source} · {(p.Ops ?? []).length} ops</small>
						</button>
					</li>
				{/each}
			</ul>

			{#if review}
				<div class="proposal">
					<h2>{review.Proposal.Title}</h2>
					<ul>
						{#each review.Ops ?? [] as r, i (i)}
							<OpRow
								review={r}
								checked={effective.includes(i)}
								cascaded={selected.includes(i) && !effective.includes(i)}
								focused={i === cursor}
								showDiff={openDiffs.has(i)}
								ontoggle={() => toggle(i)}
							/>
						{/each}
					</ul>
					<div class="actions">
						<button
							type="button"
							class="apply"
							onclick={applySelected}
							disabled={busy || effective.length === 0}
						>
							Apply ({effective.length})
						</button>
						<button type="button" onclick={rejectCurrent} disabled={busy}>Reject</button>
						<small class="hint">j/k · Space · a/Enter apply · r reject · n/p proposal · d diff</small>
					</div>
				</div>
			{/if}
		</div>
	{/if}
</section>

<style>
	.split {
		display: flex;
		gap: 1.75rem;
		align-items: flex-start;
	}
	.queue {
		min-width: 15rem;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
	}
	.queue li {
		list-style: none;
	}
	.queue li button {
		display: block;
		width: 100%;
		text-align: left;
		padding: 0.5rem 0.7rem;
	}
	.queue li button small {
		color: var(--muted);
		font-weight: 400;
	}
	.queue li.active button {
		color: var(--sea);
		border-color: var(--sea);
		font-weight: 600;
	}
	.queue li.active button small {
		color: var(--sea);
		opacity: 0.85;
	}
	.proposal {
		flex: 1;
	}
	.proposal h2 {
		font-size: 1.1rem;
		font-weight: 600;
		margin: 0 0 0.9rem;
	}
	.proposal ul {
		margin: 0 0 1.1rem;
		padding: 0;
	}
	/* Apply and Reject stand apart, with the primary action carrying the sea. */
	.actions {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		flex-wrap: wrap;
	}
	.actions .apply {
		background: var(--sea);
		border-color: var(--sea);
		color: #08101c;
		font-weight: 600;
	}
	.actions .apply:hover:not(:disabled) {
		background: var(--sea-bright);
		border-color: var(--sea-bright);
	}
	.hint {
		margin-left: 0.35rem;
		color: var(--muted);
	}
</style>
