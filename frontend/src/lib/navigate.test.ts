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
	committed: false,
	busy: false,
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
		// A getter, not a copy: the mock object is built once, and busy is what
		// each test sets on the way in.
		get busy() {
			return calls.busy;
		},
		commitEdit: async () => {
			calls.committed = true;
			return calls.commitResult;
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
	calls.commitResult = true;
	calls.committed = false;
	calls.busy = false;
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

	// The commit above is this function's own, so the store's busy guards do not
	// cover it: setMode turns back while a write is in flight and cannot say so
	// through Promise<void>, and committing behind it would write the open
	// session a second time (see base's commitEdit).
	it("does not commit or jump while a write is in flight", async () => {
		calls.busy = true;

		await jumpToBlock("b1");

		expect(calls.committed).toBe(false);
		expect(calls.revealedID).toBe("");
		expect(calls.mode).toBe("");
	});
});

describe("openTopic", () => {
	it("switches to tree mode and selects the topic", () => {
		openTopic("t1");

		expect(calls.mode).toBe("tree");
		expect(calls.selectedID).toBe("t1");
	});
});
