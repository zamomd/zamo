import { beforeEach, describe, expect, it, vi } from "vitest";

// navigate.ts pairs a mode switch with a store action; both stores are faked
// here so the test observes exactly that pairing rather than exercising the
// stores' own behaviour (already covered by their own test files).
const calls = vi.hoisted(() => ({
	mode: "",
	revealedID: "",
	selectedID: "",
	// Mirrors what base.commitEdit() resolves to: true when there was nothing
	// to commit or the write succeeded, false when the write failed and the
	// session is still open (spec §8).
	commitResult: true,
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
		commitEdit: async () => calls.commitResult,
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
	calls.commitResult = true;
});

describe("jumpToBlock", () => {
	it("switches to tree mode and reveals the block", async () => {
		await jumpToBlock("b1");

		expect(calls.mode).toBe("tree");
		expect(calls.revealedID).toBe("b1");
	});

	// setMode awaits base.commitEdit() itself but its Promise<void> does not
	// report whether the commit succeeded, so jumpToBlock reads the verdict
	// back from its own call to commitEdit rather than assuming success.
	// A failed commit leaves the session open (spec §8), and revealing
	// unconditionally would switch topics under that still-open editor's row.
	it("does not reveal when the commit it re-checks after the mode switch failed", async () => {
		calls.commitResult = false;

		await jumpToBlock("b1");

		expect(calls.mode).toBe("tree");
		expect(calls.revealedID).toBe("");
	});
});

describe("openTopic", () => {
	it("switches to tree mode and selects the topic", () => {
		openTopic("t1");

		expect(calls.mode).toBe("tree");
		expect(calls.selectedID).toBe("t1");
	});
});
