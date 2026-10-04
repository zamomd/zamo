import type { UnreviewedBlock } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { App } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind"

// The unreviewed-change set shared by the whole window: the sidebar footer,
// the Changes screen and the tree's ghost rows read the same snapshot, so
// there is exactly one polling source (no push channel from the MCP server —
// #15).
const POLL_MS = 5000

class ChangesStore {
	blocks: UnreviewedBlock[] = $state([])
	count = $derived(this.blocks.length)
	// atomic holds the blocks whose pending change is only true alongside the
	// rest of its batch — a topic rename and the references it rewrote. The
	// backend refuses a per-block decision on them, so the views that offer
	// per-block controls read this and offer the batch instead.
	atomic = $derived(new Set(this.blocks.filter((b) => b.Atomic).map((b) => b.Head.BlockID)))
	// straddling holds the topics whose page carries only part of an atomic
	// batch — a rename lands on the renamed root's own page and on every page
	// holding a block it rewrote. A page-wide decision on one of them is
	// refused, so the views that offer Accept all / Reject all read this and
	// point at the batch instead of a control that could only fail.
	straddling = $derived.by(() => {
		const pagesOf = new Map<string, Set<string>>()
		for (const b of this.blocks) {
			if (!b.Atomic || !b.GroupID) continue
			const pages = pagesOf.get(b.GroupID) ?? new Set<string>()
			pages.add(b.TopicID)
			pagesOf.set(b.GroupID, pages)
		}
		const out = new Set<string>()
		for (const pages of pagesOf.values()) {
			if (pages.size > 1) {
				for (const page of pages) out.add(page)
			}
		}
		return out
	})

	// writtenOverGroups holds the batches a later change has been saved on top
	// of. Undoing one would have to discard that later change, so the backend
	// refuses it (ErrGroupWrittenOver) and the "Reject batch" control can only
	// fail. Typing one character into a freshly converted document is the
	// common way to reach this, since an owner edit is born reviewed and lands
	// straight on top of the conversion.
	//
	// The flag is read, never inferred: the payload's own group ids cannot
	// answer this. A burying change that is itself unreviewed becomes the very
	// group the entry reports, and a batch can equally be buried by a change to
	// a different block of it, which this entry says nothing about. Storage
	// answers with the same test UndoGroup refuses by.
	writtenOverGroups = $derived.by(() => {
		const out = new Set<string>()
		for (const b of this.blocks) {
			if (b.GroupID && b.WrittenOver) {
				out.add(b.GroupID)
			}
		}
		return out
	})

	// writtenOverPages holds the topics whose page carries such a batch, and
	// only for an atomic one: a page-wide rollback does not undo the group, it
	// rolls each block back on its own, which for a buried atomic batch either
	// takes the batch apart — the thing atomic exists to prevent — or trips the
	// document invariant and aborts. A plain batch keeps its page controls,
	// where that per-block rollback is exactly the intended meaning.
	writtenOverPages = $derived.by(() => {
		const out = new Set<string>()
		for (const b of this.blocks) {
			if (b.Atomic && b.WrittenOver) {
				out.add(b.TopicID)
			}
		}
		return out
	})

	async refresh(): Promise<void> {
		const c = await App.Unreviewed()
		this.blocks = c?.Blocks ?? []
	}

	start(): () => void {
		const refresh = (): void => {
			if (!document.hidden) {
				this.refresh().catch(() => {})
			}
		}
		refresh()
		const timer = setInterval(refresh, POLL_MS)
		window.addEventListener("focus", refresh)
		document.addEventListener("visibilitychange", refresh)
		return () => {
			clearInterval(timer)
			window.removeEventListener("focus", refresh)
			document.removeEventListener("visibilitychange", refresh)
		}
	}
}

// changes is the single window-wide instance; module state outlives the views
// that render it.
export const changes = new ChangesStore()
