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
