import { beforeEach, describe, expect, it, vi } from "vitest";

const settingsFixture = vi.hoisted(() => ({
	aiEnabled: true,
	aiEnabledOrigin: "default",
	error: null as Error | null,
	calls: 0,
}));

vi.mock("../../bindings/github.com/cominddev/comind/internal/wailsbind", () => ({
	App: {
		Settings: async () => {
			settingsFixture.calls++;
			if (settingsFixture.error !== null) {
				throw settingsFixture.error;
			}
			return {
				aiEnabled: settingsFixture.aiEnabled,
				aiEnabledOrigin: settingsFixture.aiEnabledOrigin,
				provider: { value: "anthropic", origin: "config" },
				model: { value: "", origin: "default" },
				baseUrl: { value: "", origin: "default" },
				apiKeySet: false,
				apiKeyOrigin: "default",
				declarative: false,
			};
		},
	},
}));

const baseFixture = vi.hoisted(() => ({ cleared: 0 }));
vi.mock("./base.svelte", () => ({
	base: {
		clearScope: () => {
			baseFixture.cleared++;
		},
	},
}));

import { ai } from "./ai.svelte";

describe("ai policy store", () => {
	beforeEach(() => {
		settingsFixture.aiEnabled = true;
		settingsFixture.aiEnabledOrigin = "default";
		settingsFixture.error = null;
		settingsFixture.calls = 0;
		baseFixture.cleared = 0;
		ai.set(true);
	});

	it("starts enabled so a fresh window shows the AI surfaces", () => {
		expect(ai.enabled).toBe(true);
	});

	it("takes the policy from the backend on load", async () => {
		settingsFixture.aiEnabled = false;
		await ai.load();
		expect(ai.enabled).toBe(false);
	});

	// The settings screen already knows the new policy after a save; making it
	// tell the store avoids a second round trip and a window that disagrees
	// with itself in between.
	it("accepts the policy from a settings save", () => {
		ai.set(false);
		expect(ai.enabled).toBe(false);
		ai.set(true);
		expect(ai.enabled).toBe(true);
	});

	// A backend that cannot answer must not silently hide the AI surfaces:
	// the user would read that as the feature disappearing.
	it("keeps the last known policy when loading fails", async () => {
		settingsFixture.error = new Error("boom");
		await ai.load();
		expect(ai.enabled).toBe(true);
	});

	// The AI command's target is AI-specific state, and the only control that
	// clears it lives in the command box — which is gone once AI is off. Left
	// alone it paints a row that nothing can unpaint.
	it("clears the AI command target when AI goes off", () => {
		baseFixture.cleared = 0;
		ai.set(false);
		expect(baseFixture.cleared).toBe(1);
	});

	it("does not clear the target when AI stays on", () => {
		baseFixture.cleared = 0;
		ai.set(true);
		expect(baseFixture.cleared).toBe(0);
	});
});
