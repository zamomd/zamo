import { SvelteSet } from "svelte/reactivity";
import type { ProposalReview } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";
import { callError } from "../lib/callerror";
import { queue } from "./queue.svelte";

// ReviewStore is the Review tab's view-model. The pending list itself lives in
// the shared queue store; this holds which proposal is open and how far the
// reviewer got, so a tab switch away and back lands on the same proposal,
// cursor and open diffs.
class ReviewStore {
	// detail is the open proposal annotated for review (stale flags, current
	// content); null while the queue is empty.
	detail = $state<ProposalReview | null>(null);
	selected = $state<number[]>([]); // the user's manual selection
	effective = $state<number[]>([]); // selection after the domain cascade
	cursor = $state(0); // focused op row
	openDiffs = new SvelteSet<number>();
	notice = $state("");
	error = $state("");
	busy = $state(false);

	// index is the open proposal's position in the shared queue; -1 while
	// nothing is open. Derived from the ID rather than stored: the poller can
	// insert proposals and shift positions under us.
	index = $derived.by(() => {
		const id = this.detail?.Proposal.ID;
		return id === undefined ? -1 : queue.pending.findIndex((p) => p.ID === id);
	});

	// Monotonic generation counter guarding against out-of-order async
	// resolution: open()/toggle()/switchProposal()/loadQueue() each await a
	// Wails call, and overlapping calls (key-repeat on n/p, fast Space) could
	// otherwise let the last-resolving call win over the last-issued one.
	// Plain field (not $state) — it's a coordination token, not render state.
	private seq = 0;

	// activate is the mount entry: re-read the queue (the MCP side may have
	// moved) but land back on the proposal, cursor and open diffs the reviewer
	// left, when that proposal is still pending. A proposal's ops are
	// immutable, so the saved positions still point at the same rows; the
	// selection is redone from fresh stale flags like any open().
	async activate(): Promise<void> {
		const prevID = this.detail?.Proposal.ID;
		const prevCursor = this.cursor;
		const prevDiffs = [...this.openDiffs];
		const my = ++this.seq;
		await this.loadQueue(prevID, my);
		if (my !== this.seq) return;
		if (prevID !== undefined && this.detail?.Proposal.ID === prevID) {
			const n = this.detail.Ops?.length ?? 0;
			this.cursor = Math.min(Math.max(n - 1, 0), Math.max(prevCursor, 0));
			for (const d of prevDiffs) {
				if (d < n) this.openDiffs.add(d);
			}
		}
	}

	// gen lets a caller that already claimed a generation (applySelected,
	// rejectCurrent, activate) hand it down instead of minting a new one, so
	// the whole chain of awaits is invalidated together.
	async loadQueue(preferID?: string, gen?: number): Promise<void> {
		const my = gen ?? ++this.seq;
		const prev = this.index;
		await queue.refresh();
		if (my !== this.seq) return;
		if (queue.pending.length === 0) {
			this.detail = null;
			return;
		}
		const i = preferID ? queue.pending.findIndex((p) => p.ID === preferID) : -1;
		const next = i >= 0 ? i : Math.min(Math.max(prev, 0), queue.pending.length - 1);
		await this.open(queue.pending[next].ID, my);
	}

	// open loads one proposal's annotated view. Stale ops start deselected.
	async open(id: string, gen?: number): Promise<void> {
		const my = gen ?? ++this.seq;
		const r = await App.ReviewProposal(id);
		if (my !== this.seq) return;
		this.detail = r;
		const ops = r.Ops ?? [];
		this.selected = ops.map((_, i) => i).filter((i) => !ops[i].Stale);
		const eff = (await App.EffectiveSelection(r.Proposal.Ops ?? [], this.selected)) ?? [];
		if (my !== this.seq) return;
		this.effective = eff;
		this.cursor = 0;
		this.openDiffs.clear();
		this.notice = "";
	}

	async toggle(i: number): Promise<void> {
		if (!this.detail) return;
		const my = ++this.seq;
		this.selected = this.selected.includes(i) ? this.selected.filter((x) => x !== i) : [...this.selected, i];
		const eff = (await App.EffectiveSelection(this.detail.Proposal.Ops ?? [], this.selected)) ?? [];
		if (my !== this.seq) return;
		this.effective = eff;
	}

	async applySelected(): Promise<void> {
		if (!this.detail || this.busy || this.effective.length === 0 || this.selected.length === 0) return;
		this.busy = true;
		const my = ++this.seq;
		this.error = "";
		// Pick the neighbor to land on before the apply mutates the queue:
		// the next proposal, else the previous, else none (loadQueue falls
		// back to a clamped index). Guard against selected being emptied while
		// EffectiveSelection is still in flight.
		const nextID = queue.pending[this.index + 1]?.ID ?? queue.pending[this.index - 1]?.ID;
		try {
			await App.ApplyProposal(this.detail.Proposal.ID, this.selected);
			if (my !== this.seq) return;
			await this.loadQueue(nextID, my);
			this.notice = "applied";
		} catch (err: unknown) {
			if (my !== this.seq) return;
			// Codes are assigned in internal/wailsbind MarshalError — the one
			// place errors are mapped; the message text is free to change and
			// is never matched on.
			const ce = callError(err);
			switch (ce.code) {
				case "stale":
					// The base moved under the proposal: re-read, fresh flags
					// deselect what vanished.
					await this.open(this.detail.Proposal.ID, my);
					this.notice = "base changed — check again";
					break;
				case "not_pending":
					await this.loadQueue(nextID, my);
					break;
				default:
					this.error = ce.message;
			}
		} finally {
			this.busy = false;
		}
	}

	async rejectCurrent(): Promise<void> {
		if (!this.detail || this.busy) return;
		this.busy = true;
		const my = ++this.seq;
		this.error = "";
		// Same neighbor-selection logic as applySelected: compute before the
		// reject call mutates the queue.
		const nextID = queue.pending[this.index + 1]?.ID ?? queue.pending[this.index - 1]?.ID;
		try {
			await App.RejectProposal(this.detail.Proposal.ID);
			if (my !== this.seq) return;
			await this.loadQueue(nextID, my);
			this.notice = "rejected";
		} catch (err: unknown) {
			if (my !== this.seq) return;
			this.error = callError(err).message;
		} finally {
			this.busy = false;
		}
	}

	move(delta: number): void {
		const n = this.detail?.Ops?.length ?? 0;
		if (n > 0) this.cursor = Math.min(n - 1, Math.max(0, this.cursor + delta));
	}

	async switchProposal(delta: number): Promise<void> {
		if (queue.pending.length === 0) return;
		const my = ++this.seq;
		const next = Math.min(queue.pending.length - 1, Math.max(0, this.index + delta));
		await this.open(queue.pending[next].ID, my);
	}

	toggleDiff(i: number): void {
		if (this.openDiffs.has(i)) {
			this.openDiffs.delete(i);
		} else {
			this.openDiffs.add(i);
		}
	}
}

// review is the single window-wide instance; module state outlives the views
// that render it.
export const review = new ReviewStore();
