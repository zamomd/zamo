import { App } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind";
import { base } from "./base.svelte";

// AIStore holds the effective AI policy for the window.
class AIStore {
	enabled = $state(false);

	async load(): Promise<void> {
		try {
			const v = await App.Settings();
			this.enabled = v.aiEnabled.value;
		} catch {
			// Keep AI controls hidden when the policy is unavailable.
		}
	}

	set(enabled: boolean): void {
		this.enabled = enabled;
		if (!enabled) {
			base.clearScope();
		}
	}
}

export const ai = new AIStore();
