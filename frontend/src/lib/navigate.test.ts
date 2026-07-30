import { beforeEach, describe, expect, it, vi } from "vitest";

// navigate.ts pairs a mode switch with a store action; both stores are faked
// here so the test observes exactly that pairing rather than exercising the
// stores' own behaviour (already covered by their own test files).
const calls = vi.hoisted(() => ({
	mode: "",
	revealedID: "",
	selectedID: "",
	// Mirrors base.editingID: the fake setMode below leaves it set when a test
	// wants to simulate a failed commit, the way the real commitEdit does.
	editingID: null as string | null,
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
		get editingID() {
			return calls.editingID;
		},
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
	calls.editingID = null;
});

describe("jumpToBlock", () => {
	it("switches to tree mode and reveals the block", async () => {
		await jumpToBlock("b1");

		expect(calls.mode).toBe("tree");
		expect(calls.revealedID).toBe("b1");
	});

	// setMode awaits base.commitEdit() itself but its Promise<void> does not
	// report whether the commit succeeded. A failed commit leaves the session
	// open (spec §8) — editingID is what survives that — so jumpToBlock has to
	// read it back rather than reveal() unconditionally, or a failed write
	// would still get its editor's row switched out from under it.
	it("does not reveal when the mode switch's own commit left the session open", async () => {
		calls.editingID = "still-open";

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
