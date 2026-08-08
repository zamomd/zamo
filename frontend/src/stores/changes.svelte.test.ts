import { beforeEach, describe, expect, it, vi } from "vitest";

const unreviewedFixture = vi.hoisted(() => ({
	respond: (): Promise<unknown> => Promise.resolve({ Blocks: [] }),
}));
vi.mock("../../bindings/github.com/cominddev/comind/internal/wailsbind", () => ({
	App: { Unreviewed: () => unreviewedFixture.respond() },
}));

import { changes } from "./changes.svelte";

function entry(blockID: string, groupID: string, atomic: boolean, topicID = "topic"): unknown {
	return {
		Head: { BlockID: blockID, GroupID: groupID },
		TopicID: topicID,
		GroupID: groupID,
		GroupTitle: "batch",
		Atomic: atomic,
		WrittenOver: false,
	};
}

// buried is an entry whose batch storage reports as no longer undoable. Its own
// group ids still match — the burying change can be unreviewed, or can sit on a
// different block of the same batch, and in both cases the payload looks
// untouched. That is the whole reason the flag is computed in the backend.
function buried(blockID: string, groupID: string, atomic: boolean, topicID = "topic"): unknown {
	return {
		Head: { BlockID: blockID, GroupID: groupID },
		TopicID: topicID,
		GroupID: groupID,
		GroupTitle: "batch",
		Atomic: atomic,
		WrittenOver: true,
	};
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

	// A rename lands on the renamed root's own page and on every page holding a
	// block it rewrote, so a page-wide decision on either covers half a batch.
	it("collects the pages holding only part of an atomic batch", async () => {
		unreviewedFixture.respond = () =>
			Promise.resolve({
				Blocks: [
					entry("root", "rename", true, "root"),
					entry("note", "rename", true, "zig"),
					entry("other", "plain", false, "rust"),
				],
			});

		await changes.refresh();

		expect([...changes.straddling].sort()).toEqual(["root", "zig"]);
	});

	// A page that happens to hold the whole batch splits nothing, and the
	// backend lets it through — so the controls must stay.
	it("leaves a page holding a whole atomic batch alone", async () => {
		unreviewedFixture.respond = () =>
			Promise.resolve({
				Blocks: [entry("root", "rename", true, "zig"), entry("note", "rename", true, "zig")],
			});

		await changes.refresh();

		expect([...changes.straddling]).toEqual([]);
	});

	it("collects the batches a later change was saved on top of", async () => {
		unreviewedFixture.respond = () =>
			Promise.resolve({
				Blocks: [buried("root", "convert", true, "zig"), buried("child", "convert", true, "zig")],
			});

		await changes.refresh();

		expect([...changes.writtenOverGroups]).toEqual(["convert"]);
		expect([...changes.writtenOverPages]).toEqual(["zig"]);
	});

	it("leaves an untouched batch's reject controls alone", async () => {
		unreviewedFixture.respond = () =>
			Promise.resolve({
				Blocks: [entry("root", "convert", true, "zig"), entry("child", "convert", true, "zig")],
			});

		await changes.refresh();

		expect([...changes.writtenOverGroups]).toEqual([]);
		expect([...changes.writtenOverPages]).toEqual([]);
	});

	it("reports a buried plain batch to the batch screen but not to the page", async () => {
		unreviewedFixture.respond = () => Promise.resolve({ Blocks: [buried("note", "plain", false, "zig")] });

		await changes.refresh();

		expect([...changes.writtenOverGroups]).toEqual(["plain"]);
		expect([...changes.writtenOverPages]).toEqual([]);
	});
});
