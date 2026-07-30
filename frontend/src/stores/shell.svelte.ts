import { base } from "./base.svelte";

// Mode is what the main area shows. The rail owns all four: putting a second
// switch anywhere else (a tab bar, a clickable counter) means one destination
// with two doors, which is how the tab bar and the sidebar footer ended up
// competing in the first place.
export type Mode = "tree" | "graph" | "changes" | "settings";

class ShellStore {
	mode = $state<Mode>("tree");

	// setMode keeps the current mode when an open edit cannot be committed.
	async setMode(m: Mode): Promise<void> {
		if (base.busy) {
			return;
		}
		if (!(await base.commitEdit())) {
			return;
		}
		this.mode = m;
	}
}

// One instance for the window's lifetime; module state outlives the views.
export const shell = new ShellStore();
