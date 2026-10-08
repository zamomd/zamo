import { base } from "./base.svelte"

// Mode is what the main area shows. The rail owns all four: putting a second
// switch anywhere else (a tab bar, a clickable counter) means one destination
// with two doors, which is how the tab bar and the sidebar footer ended up
// competing in the first place.
export type Mode = "tree" | "graph" | "changes" | "settings"

const SIDEBAR_KEY = "zamo.sidebar"

interface SidebarPrefs {
	width: number | null
	hidden: boolean
}

// Storage can be unavailable or hold anything; the layout must come up anyway.
function loadSidebar(): SidebarPrefs {
	try {
		const raw: unknown = JSON.parse(localStorage.getItem(SIDEBAR_KEY) ?? "null")
		if (typeof raw === "object" && raw !== null) {
			const { width, hidden } = raw as Record<string, unknown>
			return {
				width: typeof width === "number" && Number.isFinite(width) ? width : null,
				hidden: hidden === true,
			}
		}
	} catch {}
	return { width: null, hidden: false }
}

const ASSISTANT_KEY = "zamo.assistant"

interface AssistantPrefs {
	width: number | null
	open: boolean
}

// Reads the stored panel preferences; anything unreadable is a closed panel
// at its default width.
export function readAssistantPrefs(stored: string | null): AssistantPrefs {
	try {
		const raw: unknown = JSON.parse(stored ?? "null")
		if (typeof raw === "object" && raw !== null) {
			const { width, open } = raw as Record<string, unknown>
			return {
				width: typeof width === "number" && Number.isFinite(width) ? width : null,
				open: open === true,
			}
		}
	} catch {}
	return { width: null, open: false }
}

function loadAssistant(): AssistantPrefs {
	try {
		return readAssistantPrefs(localStorage.getItem(ASSISTANT_KEY))
	} catch {
		return readAssistantPrefs(null)
	}
}

class ShellStore {
	mode = $state<Mode>("tree")
	// null until the user drags the edge: the stylesheet's proportional width.
	sidebarWidth = $state<number | null>(null)
	sidebarHidden = $state(false)
	assistantOpen = $state(false)
	// null until the user drags the edge: the stylesheet's own width.
	assistantWidth = $state<number | null>(null)
	// searchFocusNonce changes each time the search field is asked to take the focus.
	searchFocusNonce = $state(0)

	constructor() {
		const prefs = loadSidebar()
		this.sidebarWidth = prefs.width
		this.sidebarHidden = prefs.hidden
		const panel = loadAssistant()
		this.assistantOpen = panel.open
		this.assistantWidth = panel.width
	}

	saveAssistant(): void {
		const prefs: AssistantPrefs = { width: this.assistantWidth, open: this.assistantOpen }
		try {
			localStorage.setItem(ASSISTANT_KEY, JSON.stringify(prefs))
		} catch {}
	}

	toggleAssistant(): void {
		this.assistantOpen = !this.assistantOpen
		this.saveAssistant()
	}

	saveSidebar(): void {
		const prefs: SidebarPrefs = { width: this.sidebarWidth, hidden: this.sidebarHidden }
		try {
			localStorage.setItem(SIDEBAR_KEY, JSON.stringify(prefs))
		} catch {}
	}

	focusSearch(): void {
		this.sidebarHidden = false
		this.searchFocusNonce++
	}

	async setMode(m: Mode): Promise<void> {
		if (base.busy) {
			return
		}
		if (!(await base.commitEdit())) {
			return
		}
		this.mode = m
	}
}

// One instance for the window's lifetime; module state outlives the views.
export const shell = new ShellStore()
