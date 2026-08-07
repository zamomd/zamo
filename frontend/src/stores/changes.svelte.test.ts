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
	};
}

// buried is the same entry after a later change was saved on top of it: the
// head is that newer revision's, while the pending batch is still the old one.
function buried(blockID: string, groupID: string, atomic: boolean, topicID = "topic"): unknown {
	return {
		Head: { BlockID: blockID, GroupID: "later" },
		TopicID: topicID,
		GroupID: groupID,
		GroupTitle: "batch",
		Atomic: atomic,
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

	// One keystroke into a freshly converted document lands a reviewed owner
	// edit on top of the conversion's pending revision. Undoing the batch would
	// have to discard that edit, which the backend refuses, so the views must
	// stop offering the reject controls rather than offering ones that fail.
	it("collects the batches a later change was saved on top of", async () => {
		unreviewedFixture.respond = () =>
			Promise.resolve({
				Blocks: [buried("root", "convert", true, "zig"), entry("child", "convert", true, "zig")],
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

	// Undo is refused for any buried batch, so the Changes screen's control
	// goes either way; a page-wide rollback only takes an *atomic* batch apart,
	// so the page control is gated on that.
	it("reports a buried plain batch to the batch screen but not to the page", async () => {
		unreviewedFixture.respond = () => Promise.resolve({ Blocks: [buried("note", "plain", false, "zig")] });

		await changes.refresh();

		expect([...changes.writtenOverGroups]).toEqual(["plain"]);
		expect([...changes.writtenOverPages]).toEqual([]);
	});
});
