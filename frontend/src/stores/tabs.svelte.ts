import { untrack } from "svelte"
import { base } from "./base.svelte"

// Tab is one open topic and the view of it a switch would otherwise lose.
export interface Tab {
	id: string
	topicID: string
	focusID: string | null
	scrollTop: number
}

const TABS_KEY = "zamo.tabs"

interface StoredTabs {
	tabs: Omit<Tab, "id">[]
	active: number
}

// Reads the stored tab list; anything unreadable is no tabs, and an entry
// that is not a tab is skipped rather than failing the rest.
export function readTabs(stored: string | null): StoredTabs {
	const none: StoredTabs = { tabs: [], active: 0 }
	try {
		const raw: unknown = JSON.parse(stored ?? "null")
		if (typeof raw !== "object" || raw === null) {
			return none
		}
		const { tabs, active } = raw as Record<string, unknown>
		if (!Array.isArray(tabs)) {
			return none
		}
		const out: Omit<Tab, "id">[] = []
		let activeAt = 0
		for (const [i, entry] of tabs.entries()) {
			if (typeof entry !== "object" || entry === null) {
				continue
			}
			const { topicID, focusID, scrollTop } = entry as Record<string, unknown>
			if (typeof topicID !== "string" || topicID === "" || out.some((t) => t.topicID === topicID)) {
				continue
			}
			if (i === active) {
				activeAt = out.length
			}
			out.push({
				topicID,
				focusID: typeof focusID === "string" && focusID !== "" ? focusID : null,
				scrollTop: typeof scrollTop === "number" && Number.isFinite(scrollTop) && scrollTop > 0 ? scrollTop : 0,
			})
		}
		return { tabs: out, active: activeAt }
	} catch {
		return none
	}
}

// TabsStore is the list of open topics beside base, which goes on showing
// one topic and knows nothing of tabs. Gestures come in through open; a
// selection base makes on its own is picked up by reconcile.
class TabsStore {
	tabs = $state<Tab[]>([])
	activeID = $state<string | null>(null)
	// nextOpensNew makes the next opened topic land in a new tab.
	nextOpensNew = $state(false)

	active = $derived(this.tabs.find((t) => t.id === this.activeID) ?? null)

	private seq = 0
	// settling is above zero while a switch this store started is still on its
	// way through base: until it lands the two disagree on purpose, and
	// reconcile would undo the switch.
	private settling = 0
	private restored = false

	constructor() {
		this.load()
	}

	// load replaces the list with the stored one.
	load(): void {
		let stored: string | null = null
		try {
			stored = localStorage.getItem(TABS_KEY)
		} catch {}
		const { tabs, active } = readTabs(stored)
		this.tabs = tabs.map((t) => ({ id: this.nextID(), ...t }))
		this.activeID = this.tabs[active]?.id ?? null
		this.nextOpensNew = false
		this.restored = false
	}

	save(): void {
		const stored: StoredTabs = {
			tabs: this.tabs.map(({ topicID, focusID, scrollTop }) => ({ topicID, focusID, scrollTop })),
			active: Math.max(
				0,
				this.tabs.findIndex((t) => t.id === this.activeID),
			),
		}
		try {
			localStorage.setItem(TABS_KEY, JSON.stringify(stored))
		} catch {}
	}

	private nextID(): string {
		this.seq++
		return `tab-${this.seq}`
	}

	// open shows a topic: in its own tab if it has one, in a new tab when asked
	// to, and otherwise in place of the active tab's topic.
	async open(topicID: string, opts: { newTab?: boolean } = {}): Promise<void> {
		if (base.busy || !(await base.commitEdit())) {
			return
		}
		const wantNew = opts.newTab === true || this.nextOpensNew
		this.nextOpensNew = false
		const existing = this.tabs.find((t) => t.topicID === topicID)
		if (existing !== undefined && existing.id !== this.activeID) {
			await this.activate(existing.id)
			return
		}
		if (existing !== undefined || !wantNew || this.active === null) {
			await base.selectTopic(topicID)
			return
		}
		this.settling++
		try {
			const tab: Tab = { id: this.nextID(), topicID, focusID: null, scrollTop: 0 }
			this.tabs.splice(this.tabs.indexOf(this.active) + 1, 0, tab)
			this.activeID = tab.id
			await base.selectTopic(topicID)
		} finally {
			this.settling--
			this.reconcile()
		}
	}

	async activate(id: string): Promise<void> {
		const tab = this.tabs.find((t) => t.id === id)
		if (tab === undefined || tab.id === this.activeID || base.busy || !(await base.commitEdit())) {
			return
		}
		this.settling++
		try {
			const focusID = tab.focusID
			this.activeID = tab.id
			await base.selectTopic(tab.topicID)
			if (focusID !== null && base.selectedTopicID === tab.topicID) {
				await base.focus(focusID)
			}
		} finally {
			this.settling--
			this.reconcile()
		}
	}

	// close removes a tab, never the only one.
	async close(id: string): Promise<void> {
		const i = this.tabs.findIndex((t) => t.id === id)
		if (i < 0 || this.tabs.length < 2) {
			return
		}
		if (id !== this.activeID) {
			this.tabs.splice(i, 1)
			return
		}
		if (base.busy || !(await base.commitEdit())) {
			return
		}
		const neighbour = this.tabs[i + 1] ?? this.tabs[i - 1]
		this.settling++
		try {
			this.tabs.splice(i, 1)
			await this.activate(neighbour.id)
		} finally {
			this.settling--
			this.reconcile()
		}
	}

	async next(): Promise<void> {
		await this.step(1)
	}

	async previous(): Promise<void> {
		await this.step(-1)
	}

	private async step(by: number): Promise<void> {
		const i = this.tabs.findIndex((t) => t.id === this.activeID)
		if (i < 0 || this.tabs.length < 2) {
			return
		}
		const n = this.tabs.length
		await this.activate(this.tabs[(i + by + n) % n].id)
	}

	// at activates a tab by position; the ninth position is the last tab,
	// however many there are.
	async at(index: number): Promise<void> {
		const tab = index === 8 ? this.tabs.at(-1) : this.tabs[index]
		if (tab !== undefined) {
			await this.activate(tab.id)
		}
	}

	setScroll(scrollTop: number): void {
		if (this.active !== null && this.settling === 0) {
			this.active.scrollTop = scrollTop
		}
	}

	// reconcile brings the list in line with base: tabs of topics that are
	// gone are dropped, and a topic base selected on its own is shown by the
	// tab that holds it or, failing that, taken over by the active tab.
	reconcile(): void {
		if (this.settling > 0 || !base.loaded) {
			return
		}
		const known = new Set(base.forest.map((t) => t.block.ID))
		if (this.tabs.some((t) => !known.has(t.topicID))) {
			const at = this.tabs.findIndex((t) => t.id === this.activeID)
			const before = this.tabs.slice(0, Math.max(at, 0)).filter((t) => known.has(t.topicID)).length
			this.tabs = this.tabs.filter((t) => known.has(t.topicID))
			if (this.active === null) {
				this.activeID = (this.tabs[before] ?? this.tabs.at(-1))?.id ?? null
			}
		}
		if (!this.restored) {
			this.restored = true
			const tab = this.active
			if (tab !== null && tab.topicID !== base.selectedTopicID) {
				this.activeID = null
				void this.activate(tab.id)
				return
			}
		}
		const selected = base.selectedTopicID
		if (selected === null) {
			this.tabs = []
			this.activeID = null
			return
		}
		if (this.active?.topicID !== selected) {
			const holder = this.tabs.find((t) => t.topicID === selected)
			if (holder !== undefined) {
				this.activeID = holder.id
			} else if (this.active !== null) {
				this.active.topicID = selected
				this.active.scrollTop = 0
			} else {
				const tab: Tab = { id: this.nextID(), topicID: selected, focusID: null, scrollTop: 0 }
				this.tabs.push(tab)
				this.activeID = tab.id
			}
		}
		if (this.active !== null) {
			this.active.focusID = base.focusID
		}
	}

	// start keeps the list reconciled and stored for as long as the window
	// lives. Scroll positions change too often to store one by one; they go
	// out with the next change to the list and when the page is hidden.
	start(): () => void {
		const save = (): void => this.save()
		const stop = $effect.root(() => {
			$effect(() => {
				void base.forest
				void base.selectedTopicID
				void base.focusID
				void base.loaded
				untrack(() => this.reconcile())
			})
			$effect(() => {
				for (const t of this.tabs) {
					void t.topicID
					void t.focusID
				}
				void this.activeID
				if (base.loaded) {
					untrack(save)
				}
			})
		})
		window.addEventListener("pagehide", save)
		return () => {
			stop()
			window.removeEventListener("pagehide", save)
		}
	}
}

// tabs is the single window-wide instance, like the other stores.
export const tabs = new TabsStore()
