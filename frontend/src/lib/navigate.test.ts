import { beforeEach, describe, expect, it, vi } from "vitest";

// navigate.ts pairs a mode switch with a store action; both stores are faked
// here so the test observes exactly that pairing rather than exercising the
// stores' own behaviour (already covered by their own test files).
const calls = vi.hoisted(() => ({
	mode: "",
	revealedID: "",
	selectedID: "",
}));

vi.mock("../stores/shell.svelte", () => ({
	shell: {
		setMode: async (m: string) => {
			calls.mode = m;
		},
	},
}));

vi.mock("../stores/base.svelte", () => ({
	base: {
		reveal: (id: string) => {
			calls.revealedID = id;
		},
		selectTopic: (id: string) => {
			calls.selectedID = id;
		},
	},
}));

import { jumpToBlock, openTopic } from "./navigate";

beforeEach(() => {
	calls.mode = "";
	calls.revealedID = "";
	calls.selectedID = "";
});

describe("jumpToBlock", () => {
	it("switches to tree mode and reveals the block", async () => {
		await jumpToBlock("b1");

		expect(calls.mode).toBe("tree");
		expect(calls.revealedID).toBe("b1");
	});
});

describe("openTopic", () => {
	it("switches to tree mode and selects the topic", () => {
		openTopic("t1");

		expect(calls.mode).toBe("tree");
		expect(calls.selectedID).toBe("t1");
	});
});
