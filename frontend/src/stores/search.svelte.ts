import type { SearchHit } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { App } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind"
import { messageFor } from "../lib/callerror"

// How long typing settles before a query is sent, so search-as-you-type is not
// a backend call per keystroke.
const debounceMs = 200

// SearchStore is the search field's view-model: a live, debounced full-text
// query over the base. It is read-only, so it stays independent of the AI and
// inline-edit flows the base store guards with `busy`. Like the other stores
// it lives at module level, so the field, its text and its results survive a
// mode switch.
class SearchStore {
	query = $state("")
	results = $state<SearchHit[]>([])
	status = $state("")
	error = $state("")

	private timer: ReturnType<typeof setTimeout> | null = null
	private seq = 0

	setQuery(q: string): void {
		this.query = q
		this.seq++
		this.results = []
		this.status = ""
		this.error = ""
		if (this.timer !== null) {
			clearTimeout(this.timer)
			this.timer = null
		}
		if (q.trim() === "") {
			return
		}
		this.timer = setTimeout(() => {
			this.timer = null
			void this.run()
		}, debounceMs)
	}

	private async run(): Promise<void> {
		const mine = ++this.seq
		const q = this.query.trim()
		try {
			const hits = (await App.Search(q)) ?? []
			if (mine !== this.seq) {
				return
			}
			this.results = hits
			this.status = hits.length === 0 ? "no matches" : `${hits.length} result${hits.length === 1 ? "" : "s"}`
			this.error = ""
		} catch (err: unknown) {
			if (mine !== this.seq) {
				return
			}
			this.results = []
			this.status = ""
			this.error = messageFor(err)
		}
	}

	clear(): void {
		this.setQuery("")
	}
}

export const search = new SearchStore()
