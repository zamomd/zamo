// Debounced, latest-only block search for the reference popup: one backend
// call per pause in typing, and results delivered only while the query that
// asked for them is still the one the caret carries. It lives here rather than
// in RefAutocomplete.svelte so the sequencing is testable without a DOM.

export interface RefSearchOptions<T> {
	// search runs the query against the backend. A null answer is no hits.
	search: (query: string) => Promise<T[] | null>;
	// onResults delivers the current query's hits, empty when it has none.
	onResults: (results: T[]) => void;
	// onError reports a failed search that was still current when it failed.
	onError: (err: unknown) => void;
	debounceMs?: number;
}

export interface RefSearch {
	// update announces the query the caret now carries. An empty one has no
	// call to make and delivers no hits at once.
	update(query: string): void;
	// cancel drops what is pending or in flight without delivering it.
	cancel(): void;
}

const defaultDebounceMs = 200;

export function createRefSearch<T>({
	search,
	onResults,
	onError,
	debounceMs = defaultDebounceMs,
}: RefSearchOptions<T>): RefSearch {
	let timer: ReturnType<typeof setTimeout> | null = null;
	// Every change of query starts a generation, and a call answers only for
	// the generation it was made in. Counting where the query changes rather
	// than where a call starts is what keeps a slow answer to a query since
	// retyped — or cleared — out of the list the caret is completing, which is
	// also the list Enter inserts from.
	let generation = 0;

	function invalidate(): number {
		if (timer !== null) {
			clearTimeout(timer);
			timer = null;
		}
		generation += 1;
		return generation;
	}

	async function run(query: string, mine: number): Promise<void> {
		try {
			const hits = (await search(query)) ?? [];
			if (mine === generation) {
				onResults(hits);
			}
		} catch (err: unknown) {
			if (mine === generation) {
				onError(err);
			}
		}
	}

	return {
		update(query: string): void {
			const mine = invalidate();
			if (query === "") {
				onResults([]);
				return;
			}
			timer = setTimeout(() => {
				timer = null;
				void run(query, mine);
			}, debounceMs);
		},
		cancel(): void {
			invalidate();
		},
	};
}
