import type { UnreviewedBlock } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";

// The unreviewed-change set shared by the whole window: the sidebar footer,
// the Changes screen and the tree's ghost rows read the same snapshot, so
// there is exactly one polling source (no push channel from the MCP server —
// #15).
const POLL_MS = 5000;

class ChangesStore {
	blocks: UnreviewedBlock[] = $state([]);
	count = $derived(this.blocks.length);
	// atomic holds the blocks whose pending change is only true alongside the
	// rest of its batch — a topic rename and the references it rewrote. The
	// backend refuses a per-block decision on them, so the views that offer
	// per-block controls read this and offer the batch instead.
	atomic = $derived(new Set(this.blocks.filter((b) => b.Atomic).map((b) => b.Head.BlockID)));
	// straddling holds the topics whose page carries only part of an atomic
	// batch — a rename lands on the renamed root's own page and on every page
	// holding a block it rewrote. A page-wide decision on one of them is
	// refused, so the views that offer Accept all / Reject all read this and
	// point at the batch instead of a control that could only fail.
	straddling = $derived.by(() => {
		const pagesOf = new Map<string, Set<string>>();
		for (const b of this.blocks) {
			if (!b.Atomic || !b.GroupID) continue;
			const pages = pagesOf.get(b.GroupID) ?? new Set<string>();
			pages.add(b.TopicID);
			pagesOf.set(b.GroupID, pages);
		}
		const out = new Set<string>();
		for (const pages of pagesOf.values()) {
			if (pages.size > 1) {
				for (const page of pages) out.add(page);
			}
		}
		return out;
	});

	async refresh(): Promise<void> {
		const c = await App.Unreviewed();
		this.blocks = c?.Blocks ?? [];
	}

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

// changes is the single window-wide instance; module state outlives the views
// that render it.
export const changes = new ChangesStore();
