import { beforeEach, describe, expect, it, vi } from "vitest";

const calls = vi.hoisted(() => ({
	mode: "",
	revealedID: "",
	selectedID: "",
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

	it("does not reveal when the commit it re-checks after the mode switch failed", async () => {
		calls.commitResult = false;

		await jumpToBlock("b1");

		expect(calls.mode).toBe("tree");
		expect(calls.revealedID).toBe("");
	});

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
