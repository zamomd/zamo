import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";

// AIStore holds the application-wide AI policy for the whole window. It is one
// store rather than a prop threaded through the views because the switch has
// to reach surfaces that share no parent — the command box, the block gutter,
// the settings screen.
class AIStore {
	// enabled starts true so a window that has not loaded settings yet shows
	// the AI surfaces; a load that says otherwise takes them away, which reads
	// as the switch working. The other way round the surfaces would flicker in
	// on every start.
	enabled = $state(true);

	// load asks the backend for the effective policy. A failure leaves the last
	// known value: hiding every AI control because one call failed would read
	// as the feature disappearing rather than as an error.
	async load(): Promise<void> {
		try {
			const v = await App.Settings();
			this.enabled = v.aiEnabled;
		} catch {
			// Kept deliberately silent: the settings screen reports its own
			// failures, and no other view has a place to show this one.
		}
	}

	// set records a policy the caller already knows — the settings screen after
	// a save — so the window never disagrees with itself while a reload is in
	// flight.
	set(enabled: boolean): void {
		this.enabled = enabled;
	}
}

// One instance for the window's lifetime; module state outlives the views.
export const ai = new AIStore();
