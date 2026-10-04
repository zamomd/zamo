import type { TopicEdge, TopicNode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { App } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind"
import { messageFor } from "../lib/callerror"
import { layout, type Point } from "../lib/graph"

// GraphStore is the topic map's view-model. Like the other stores it lives at
// module level, so a mode switch away and back does not lose what was read —
// but unlike the tree it re-reads on every entry into graph mode: links and
// topics change under it while the reader is elsewhere, and a stale map is
// worse than a moment's wait.
class GraphStore {
	topics = $state<TopicNode[]>([])
	edges = $state<TopicEdge[]>([])
	loading = $state(false)
	error = $state("")

	positions = $derived<Map<string, Point>>(layout(this.topics, this.edges))

	async load(): Promise<void> {
		if (this.loading) {
			return
		}
		this.loading = true
		this.error = ""
		try {
			const g = await App.TopicGraph()
			this.topics = g.Topics ?? []
			this.edges = g.Edges ?? []
		} catch (err: unknown) {
			this.topics = []
			this.edges = []
			this.error = messageFor(err)
		} finally {
			this.loading = false
		}
	}
}

// graph is the single window-wide instance; module state outlives the views
// that render it.
export const graph = new GraphStore()
