import { base } from "./base.svelte";

// Mode is what the main area shows. The rail owns all four: putting a second
// switch anywhere else (a tab bar, a clickable counter) means one destination
// with two doors, which is how the tab bar and the sidebar footer ended up
// competing in the first place.
export type Mode = "tree" | "graph" | "changes" | "settings";

class ShellStore {
	mode = $state<Mode>("tree");

	// Every mode change leaves the tree, which can unmount an open editor's
	// row, so it commits first — the rail owning all four switches (see above)
	// is what makes this the one place that has to.
	async setMode(m: Mode): Promise<void> {
		await base.commitEdit();
		this.mode = m;
	}
}

// One instance for the window's lifetime; module state outlives the views.
export const shell = new ShellStore();
