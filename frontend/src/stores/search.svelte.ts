import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";
import { messageFor } from "../lib/callerror";
import { base } from "./base.svelte";

// How long typing settles before a query is sent, so search-as-you-type is not
// a backend call per keystroke.
const debounceMs = 200;

// SearchStore is the search field's view-model: a live, debounced full-text
// query over the base, and the jump that lands a result back in the tree. It is
// read-only, so it stays independent of the AI and inline-edit flows the base
// store guards with `busy`. Like the other stores it lives at module level, so
// the field, its text and its results survive a tab switch.
class SearchStore {
	query = $state("");
	results = $state<Block[]>([]);
	status = $state("");
	error = $state("");

	private timer: ReturnType<typeof setTimeout> | null = null;
	// seq stamps each request so a slow earlier response cannot overwrite a
	// later query's results; only the newest seq is allowed to land.
	private seq = 0;

	setQuery(q: string): void {
		this.query = q;
		// Any edit invalidates what is showing and in flight: bump seq so a late
		// response for the superseded text is dropped, and clear the list so a hit
		// for an older query is never shown — or clicked — under the newer text.
		this.seq++;
		this.results = [];
		this.status = "";
		this.error = "";
		if (this.timer !== null) {
			clearTimeout(this.timer);
			this.timer = null;
		}
		if (q.trim() === "") {
			return;
		}
		this.timer = setTimeout(() => {
			this.timer = null;
			void this.run();
		}, debounceMs);
	}

	private async run(): Promise<void> {
		const mine = ++this.seq;
		const q = this.query.trim();
		try {
			const hits = (await App.Search(q)) ?? [];
			if (mine !== this.seq) {
				return;
			}
			this.results = hits;
			this.status = hits.length === 0 ? "no matches" : `${hits.length} result${hits.length === 1 ? "" : "s"}`;
			this.error = "";
		} catch (err: unknown) {
			if (mine !== this.seq) {
				return;
			}
			this.results = [];
			this.status = "";
			this.error = messageFor(err);
		}
	}

	// jumpTo leaves the results in place so the reader can walk the list without
	// searching again.
	jumpTo(block: Block): void {
		base.reveal(block.ID);
	}

	clear(): void {
		this.setQuery("");
	}
}

export const search = new SearchStore();
