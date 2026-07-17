import type { Proposal } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";

// The pending-proposal queue shared by the whole window: the nav badge and the
// review screen read the same list, so there is exactly one polling source.
//
// The window has no push channel from the MCP server, so it re-reads the
// pending queue itself on a timer (and on focus) — see #15: no daemon, no bus.
const POLL_MS = 5000;

class QueueStore {
	pending: Proposal[] = $state([]);
	count = $derived(this.pending.length);

	async refresh(): Promise<void> {
		this.pending = (await App.PendingProposals()) ?? [];
	}

	// start reads the queue once and begins polling. An MCP-submitted proposal
	// arrives out of band while the window sits idle; re-read it without a
	// restart. Poll only while the window is visible (no wakeups when
	// minimized) and re-read the moment it regains focus or visibility, so a
	// return from the background is never left stale. Returns the cleanup.
	start(): () => void {
		const refresh = (): void => {
			if (!document.hidden) {
				this.refresh().catch(() => {});
			}
		};
		refresh();
		const timer = setInterval(refresh, POLL_MS);
		window.addEventListener("focus", refresh);
		document.addEventListener("visibilitychange", refresh);
		return () => {
			clearInterval(timer);
			window.removeEventListener("focus", refresh);
			document.removeEventListener("visibilitychange", refresh);
		};
	}
}

// queue is the single window-wide instance; module state outlives the views
// that render it.
export const queue = new QueueStore();
