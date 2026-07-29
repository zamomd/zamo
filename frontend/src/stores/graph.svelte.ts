import type { TopicEdge, TopicNode } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";
import { messageFor } from "../lib/callerror";
import { layout, type Point } from "../lib/graph";

// GraphStore is the topic map's view-model. Like the other stores it lives at
// module level, so a mode switch away and back does not lose what was read —
// but unlike the tree it re-reads on every entry into graph mode: links and
// topics change under it while the reader is elsewhere, and a stale map is
// worse than a moment's wait.
class GraphStore {
	topics = $state<TopicNode[]>([]);
	edges = $state<TopicEdge[]>([]);
	loading = $state(false);
	error = $state("");

	// The geometry is derived, never stored: a stored copy could disagree with
	// the topics it was computed from, and the simulation is deterministic, so
	// deriving it costs nothing but the arithmetic.
	positions = $derived<Map<string, Point>>(layout(this.topics, this.edges));

	async load(): Promise<void> {
		// Entering graph mode twice in quick succession — or a re-entry landing
		// on a slow read — must not run two simulations over two answers.
		if (this.loading) {
			return;
		}
		this.loading = true;
		this.error = "";
		try {
			// The graph object is never null — it crosses every layer by value.
			// Its two slices are, because a Go nil slice marshals to JSON null.
			const g = await App.TopicGraph();
			this.topics = g.Topics ?? [];
			this.edges = g.Edges ?? [];
		} catch (err: unknown) {
			// Drop what was on screen: a map drawn from the previous read, shown
			// beside an error about this one, is a lie about what the base holds.
			this.topics = [];
			this.edges = [];
			this.error = messageFor(err);
		} finally {
			this.loading = false;
		}
	}
}

// graph is the single window-wide instance; module state outlives the views
// that render it.
export const graph = new GraphStore();
