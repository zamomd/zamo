import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createRefSearch } from "./refsearch"

type Hit = { id: string }

const debounceMs = 200

// harness drives createRefSearch over a backend whose every call is left
// hanging until the test resolves it by name, which is what lets a superseded
// query answer after a newer one has already been asked.
function harness() {
	const calls: string[] = []
	const settlers = new Map<string, { resolve: (hits: Hit[]) => void; reject: (err: unknown) => void }>()
	const results: Hit[][] = []
	const errors: unknown[] = []
	const search = createRefSearch<Hit>({
		search: (query) => {
			calls.push(query)
			return new Promise<Hit[]>((resolve, reject) => settlers.set(query, { resolve, reject }))
		},
		onResults: (hits) => results.push(hits),
		onError: (err) => errors.push(err),
		debounceMs,
	})
	// flush lets the awaiting search continuation run: the promise is settled
	// by hand, so nothing else yields the microtask queue.
	const flush = async () => {
		await Promise.resolve()
		await Promise.resolve()
	}
	return {
		search,
		calls,
		results,
		errors,
		flush,
		type(query: string) {
			search.update(query)
			vi.advanceTimersByTime(debounceMs)
		},
		resolve(query: string, hits: Hit[]) {
			const settler = settlers.get(query)
			if (settler === undefined) {
				throw new Error(`nothing searched for ${query}`)
			}
			settler.resolve(hits)
			return flush()
		},
		reject(query: string, err: unknown) {
			const settler = settlers.get(query)
			if (settler === undefined) {
				throw new Error(`nothing searched for ${query}`)
			}
			settler.reject(err)
			return flush()
		},
	}
}

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe("createRefSearch", () => {
	it("searches once typing pauses and delivers the hits", async () => {
		const h = harness()
		h.type("foo")
		expect(h.calls).toEqual(["foo"])
		await h.resolve("foo", [{ id: "01" }])
		expect(h.results).toEqual([[{ id: "01" }]])
	})

	it("makes no call for a query still being typed", () => {
		const h = harness()
		h.search.update("f")
		h.search.update("fo")
		h.type("foo")
		expect(h.calls).toEqual(["foo"])
	})

	// The newer query is still inside its debounce when the older one answers,
	// so nothing but the query change itself can have superseded it.
	it("drops the hits of a query a newer one superseded", async () => {
		const h = harness()
		h.type("foo")
		h.search.update("bar")
		await h.resolve("foo", [{ id: "stale" }])
		expect(h.results).toEqual([])
		vi.advanceTimersByTime(debounceMs)
		await h.resolve("bar", [{ id: "fresh" }])
		expect(h.results).toEqual([[{ id: "fresh" }]])
	})

	it("drops the hits of a query cleared while it was in flight", async () => {
		const h = harness()
		h.type("foo")
		h.search.update("")
		expect(h.results).toEqual([[]])
		await h.resolve("foo", [{ id: "stale" }])
		expect(h.results).toEqual([[]])
	})

	it("drops the hits of a cancelled query", async () => {
		const h = harness()
		h.type("foo")
		h.search.cancel()
		await h.resolve("foo", [{ id: "stale" }])
		expect(h.results).toEqual([])
	})

	it("reports a current query's failure and stays quiet about a superseded one's", async () => {
		const h = harness()
		h.type("foo")
		h.search.update("bar")
		await h.reject("foo", new Error("stale failure"))
		expect(h.errors).toEqual([])
		vi.advanceTimersByTime(debounceMs)
		await h.reject("bar", new Error("current failure"))
		expect(h.errors).toEqual([new Error("current failure")])
	})
})
