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

	async function loadQueue(preferID?: string): Promise<void> {
		queue = (await App.PendingProposals()) ?? [];
		if (queue.length === 0) {
			review = null;
			return;
		}
		const i = preferID ? queue.findIndex((p) => p.ID === preferID) : -1;
		current = i >= 0 ? i : Math.min(current, queue.length - 1);
		await open(queue[current].ID);
	}

	// open loads one proposal's annotated view. Stale ops start deselected.
	async function open(id: string): Promise<void> {
		const r = await App.ReviewProposal(id);
		review = r;
		const ops = r.Ops ?? [];
		selected = ops.map((_, i) => i).filter((i) => !ops[i].Stale);
		effective = (await App.EffectiveSelection(r.Proposal.Ops ?? [], selected)) ?? [];
		cursor = 0;
		openDiffs = new Set();
		notice = "";
	}

	async function toggle(i: number): Promise<void> {
		if (!review) return;
		selected = selected.includes(i) ? selected.filter((x) => x !== i) : [...selected, i];
		effective = (await App.EffectiveSelection(review.Proposal.Ops ?? [], selected)) ?? [];
	}

	async function applySelected(): Promise<void> {
		if (!review || busy || effective.length === 0) return;
		busy = true;
		error = "";
		try {
			await App.ApplyProposal(review.Proposal.ID, selected);
			await loadQueue();
			onchanged();
			notice = "applied";
		} catch (err: unknown) {
			const msg = String(err);
			if (msg.includes("stale")) {
				// The base moved under the proposal: re-read, fresh flags deselect
				// what vanished.
				await open(review.Proposal.ID);
				notice = "base changed — check again";
			} else if (msg.includes("already decided")) {
				await loadQueue();
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
		error = "";
		try {
			await App.RejectProposal(review.Proposal.ID);
			await loadQueue();
			onchanged();
			notice = "rejected";
		} catch (err: unknown) {
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
		current = Math.min(queue.length - 1, Math.max(0, current + delta));
		await open(queue[current].ID);
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
		if (t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement) return;
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
					<p>
						<button type="button" onclick={applySelected} disabled={busy || effective.length === 0}>
							Apply ({effective.length})
						</button>
						<button type="button" onclick={rejectCurrent} disabled={busy}>Reject</button>
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
