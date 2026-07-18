import type { Edge, UnreviewedBlock } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";

// The unreviewed-change set shared by the whole window: the nav badge, the
// Changes screen and the tree's ghost rows read the same snapshot, so there
// is exactly one polling source (no push channel from the MCP server — #15).
const POLL_MS = 5000;

class ChangesStore {
	blocks: UnreviewedBlock[] = $state([]);
	edges: Edge[] = $state([]);
	count = $derived(this.blocks.length + this.edges.length);

	async refresh(): Promise<void> {
		const c = await App.Unreviewed();
		this.blocks = c?.Blocks ?? [];
		this.edges = c?.Edges ?? [];
	}

	// start reads the changes once and begins polling. An MCP-submitted write
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

// changes is the single window-wide instance; module state outlives the views
// that render it.
export const changes = new ChangesStore();
