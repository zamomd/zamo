import { shell } from "../stores/shell.svelte"
import { tabs } from "../stores/tabs.svelte"

// handleTabKey runs the tab shortcut a key press names, on the tree screen
// only, and reports whether it was one. Closing is not here: that key is
// claimed by the window before the page sees it and arrives as an event.
export function handleTabKey(e: KeyboardEvent): boolean {
	if (shell.mode !== "tree" || !(e.metaKey || e.ctrlKey) || e.altKey) {
		return false
	}
	// Physical keys: the brackets and digits sit elsewhere, or behind Shift,
	// on other layouts.
	if (e.shiftKey) {
		if (e.code === "BracketRight") {
			void tabs.next()
			return true
		}
		if (e.code === "BracketLeft") {
			void tabs.previous()
			return true
		}
		return false
	}
	if (e.code === "KeyT") {
		tabs.nextOpensNew = true
		shell.focusSearch()
		return true
	}
	const digit = /^Digit([1-9])$/.exec(e.code)
	if (digit !== null) {
		void tabs.at(Number(digit[1]) - 1)
		return true
	}
	return false
}

// closeActiveTab is the close key. It never falls through to the window:
// closing here means hiding to the tray, and one press too many while
// closing tabs would make the app vanish.
export function closeActiveTab(): void {
	if (shell.mode === "tree" && tabs.activeID !== null) {
		void tabs.close(tabs.activeID)
	}
}
