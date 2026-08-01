import { beforeEach, describe, expect, it, vi } from "vitest";

const unreviewedFixture = vi.hoisted(() => ({
	respond: (): Promise<unknown> => Promise.resolve({ Blocks: [] }),
}));
vi.mock("../../bindings/github.com/cominddev/comind/internal/wailsbind", () => ({
	App: { Unreviewed: () => unreviewedFixture.respond() },
}));

import { changes } from "./changes.svelte";

function entry(blockID: string, groupID: string, atomic: boolean): unknown {
	return { Head: { BlockID: blockID }, TopicID: "topic", GroupID: groupID, GroupTitle: "batch", Atomic: atomic };
}

beforeEach(() => {
	unreviewedFixture.respond = () => Promise.resolve({ Blocks: [] });
	changes.blocks = [];
});

describe("refresh", () => {
	it("survives a null block list", async () => {
		unreviewedFixture.respond = () => Promise.resolve({ Blocks: null });
		await changes.refresh();
		expect(changes.blocks).toEqual([]);
		expect(changes.count).toBe(0);
	});

	// The views offering per-block accept/reject read this set: a block in it
	// gets the batch controls instead, because deciding on it alone is refused.
	it("collects the blocks whose batch has to be reviewed whole", async () => {
		unreviewedFixture.respond = () =>
			Promise.resolve({
				Blocks: [entry("root", "rename", true), entry("note", "rename", true), entry("other", "plain", false)],
			});

		await changes.refresh();

		expect([...changes.atomic]).toEqual(["root", "note"]);
		expect(changes.count).toBe(3);
	});
});
