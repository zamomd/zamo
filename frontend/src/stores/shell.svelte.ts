// Mode is what the main area shows. The rail owns all four: putting a second
// switch anywhere else (a tab bar, a clickable counter) means one destination
// with two doors, which is how the tab bar and the sidebar footer ended up
// competing in the first place.
export type Mode = "tree" | "graph" | "changes" | "settings";

class ShellStore {
	mode = $state<Mode>("tree");

	setMode(m: Mode): void {
		this.mode = m;
	}
}

// One instance for the window's lifetime; module state outlives the views.
export const shell = new ShellStore();
