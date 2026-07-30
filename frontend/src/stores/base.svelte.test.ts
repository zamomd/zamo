import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";

// The store subscribes to a Wails event in its constructor and calls bindings
// on load; neither exists outside the webview, so both are faked here. The
// tests below drive the forest directly and never touch the backend.
vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}));

// reloadFixture backs the two bindings reload() actually calls. It is
// declared via vi.hoisted so the mock factory below (itself hoisted above
// this file's imports) can close over it, and each reload test rewrites it
// to control the base that reload() reads back.
const reloadFixture = vi.hoisted(() => ({
	topics: [] as Block[],
	subtrees: {} as Record<string, Block[]>,
	// topicMapError makes the read fail; gate holds it open, so a test can act
	// while a reload is still in flight and see which act wins.
	topicMapError: null as Error | null,
	gate: null as Promise<void> | null,
	// onSubtree observes the store mid-re-read: everything a gesture does after
	// its GetSubtree is invisible to a test that can only look at the end state,
	// and the order of those steps is the whole point of the caret's placement.
	onSubtree: null as ((id: string) => void) | null,
}));
vi.mock("../../bindings/github.com/cominddev/comind/internal/wailsbind", () => ({
	App: {
		TopicMap: async () => {
			if (reloadFixture.gate !== null) {
				await reloadFixture.gate;
			}
			if (reloadFixture.topicMapError !== null) {
				throw reloadFixture.topicMapError;
			}
			return reloadFixture.topics;
		},
		GetSubtree: async (id: string) => {
			reloadFixture.onSubtree?.(id);
			return reloadFixture.subtrees[id] ?? [];
		},
		ExpandTopic: async () => {},
		EditWithAI: async () => {},
		EditBlockContent: vi.fn(async () => {}),
		DeleteBlock: vi.fn(async () => {}),
		SplitBlock: vi.fn(async () => "new"),
		InsertBlockBefore: vi.fn(async () => "above"),
		IndentBlock: vi.fn(async () => {}),
		OutdentBlock: vi.fn(async () => {}),
		MergeBlocks: vi.fn(async () => {}),
		AcceptBlock: vi.fn(async () => {}),
		RejectBlock: vi.fn(async () => {}),
		AcceptPage: vi.fn(async () => {}),
		RejectPage: vi.fn(async () => {}),
		DeleteRevision: vi.fn(async () => {}),
		BlockHistory: vi.fn(async () => []),
	},
}));
vi.mock("./changes.svelte", () => ({
	changes: { blocks: [], edges: [], count: 0, refresh: vi.fn(async () => {}) },
}));

import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";
import { findNode, type TreeNodeData } from "../lib/tree";
import { base } from "./base.svelte";
import { changes } from "./changes.svelte";
import { shell } from "./shell.svelte";

function block(id: string, parent: string, content = id): Block {
	return {
		ID: id,
		ParentID: parent,
		Position: 0,
		Content: content,
		Meta: {},
		CreatedAt: "",
		UpdatedAt: "",
		Unreviewed: false,
	};
}

function node(id: string, parent: string, children: TreeNodeData[] = [], content = id): TreeNodeData {
	return { block: block(id, parent, content), children };
}

beforeEach(() => {
	base.forest = [
		node("go", "", [node("ctx", "go", [node("done", "ctx")])]),
		node("sqlite", "", [node("wal", "sqlite")]),
	];
	base.selectedTopicID = "go";
	base.collapsed.clear();
	base.highlightID = null;
	base.focusID = null;
	base.error = "";
	// toggle, focus and the rest of the tree's actions now commit an open edit
	// rather than refuse, so a leaked editingID would silently write a stale
	// editText into an unrelated test's block instead of merely disarming it.
	base.editingID = null;
	base.editText = "";
	base.editCaret = 0;
	// Neither is reset by the store between acts, and both change what a gesture
	// is allowed to do, so a test that seeds either would otherwise decide the
	// outcome of whatever test the shuffle runs next.
	base.ghosts.clear();
	base.busy = false;
	reloadFixture.topicMapError = null;
	reloadFixture.gate = null;
	reloadFixture.onSubtree = null;
	// Seed the read fixture to match the seeded forest, so a re-read that a test
	// did not set up is a round trip rather than a topic silently vanishing.
	reloadFixture.topics = [block("go", ""), block("sqlite", "")];
	reloadFixture.subtrees = {
		go: [block("go", ""), block("ctx", "go"), block("done", "ctx")],
		sqlite: [block("sqlite", ""), block("wal", "sqlite")],
	};
	vi.clearAllMocks();
	changes.blocks = [];
});

describe("selectTopic", () => {
	it("switches the main area to another topic", async () => {
		await base.selectTopic("sqlite");
		expect(base.selectedTopicID).toBe("sqlite");
	});

	it("exposes that topic's tree", async () => {
		await base.selectTopic("sqlite");
		expect(base.selectedTree?.block.ID).toBe("sqlite");
	});

	it("clears a stale jump flash from the previous topic", async () => {
		base.highlightID = "done";
		await base.selectTopic("sqlite");
		expect(base.highlightID).toBe(null);
	});

	// The graph view is the one caller whose ids come from an
	// independently-refreshed read (see selectTopic's own comment): the topic
	// map re-reads on every entry into graph mode, while the forest here still
	// reflects whatever was loaded at startup or the last GUI mutation. A
	// topic added out-of-band — comind mcp, while the reader was in graph
	// mode — is real but absent from this.forest until a fresh read.
	it("reaches a topic the forest predates, once the base is re-read", async () => {
		base.error = "";
		reloadFixture.topics = [block("go", ""), block("sqlite", ""), block("rust", "")];
		reloadFixture.subtrees = {
			go: [block("go", "")],
			sqlite: [block("sqlite", "")],
			rust: [block("rust", "")],
		};

		await base.selectTopic("rust");

		expect(base.selectedTopicID).toBe("rust");
		expect(base.selectedTree?.block.ID).toBe("rust");
		expect(base.error).toBe("");
	});

	// A topic that is genuinely gone (deleted out-of-band, or a stale graph
	// node from a map drawn before the deletion) must not leave the pane
	// looking like nothing is selected — see TopicView's "No topic selected"
	// message, which is only honest when nothing really is.
	it("reports rather than blanks the pane when the topic is gone even after a re-read", async () => {
		base.error = "";
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", "")], sqlite: [block("sqlite", "")] };

		await base.selectTopic("nonexistent");

		expect(base.error).not.toBe("");
		// The previous, still-valid selection is left in place rather than
		// pointed at the missing id, so selectedTree stays non-null and the
		// pane keeps showing a real topic instead of the empty-state message.
		expect(base.selectedTopicID).toBe("go");
		expect(base.selectedTree).not.toBe(null);
	});

	// The re-read above is fired from markup, which cannot await it, so a second
	// act can land while it is still in flight. Whatever the reader did last has
	// to win, or the older click drags them back off the topic they moved to.
	it("does not let a re-read land on top of a later selection", async () => {
		let release: () => void = () => {};
		reloadFixture.gate = new Promise((resolve) => {
			release = () => resolve();
		});
		reloadFixture.topics = [block("go", ""), block("sqlite", ""), block("rust", "")];
		reloadFixture.subtrees = {
			go: [block("go", "")],
			sqlite: [block("sqlite", "")],
			rust: [block("rust", "")],
		};

		const stale = base.selectTopic("rust");
		// "sqlite" is already in the forest, so this resolves without a re-read
		// and is the reader's latest act.
		await base.selectTopic("sqlite");
		release();
		await stale;

		expect(base.selectedTopicID).toBe("sqlite");
	});

	// The "no longer exists" message belongs to the click that produced it. A
	// later click that lands has answered it, and leaving it on screen would
	// describe a topic the reader is no longer looking at.
	it("clears a previous selection's error once a later one lands", async () => {
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", "")], sqlite: [block("sqlite", "")] };

		await base.selectTopic("nonexistent");
		expect(base.error).not.toBe("");

		await base.selectTopic("sqlite");

		expect(base.selectedTopicID).toBe("sqlite");
		expect(base.error).toBe("");
	});

	// Nobody awaits selectTopic, so a rejected re-read has no other route to the
	// reader: without this they click a node, stay where they were, and are told
	// nothing.
	it("reports a failed re-read instead of failing silently", async () => {
		reloadFixture.topicMapError = new Error("database is locked");

		await base.selectTopic("rust");

		expect(base.error).not.toBe("");
		expect(base.selectedTopicID).toBe("go");
	});
});

describe("reveal", () => {
	it("switches topics when the block lives in another one", () => {
		base.reveal("wal");
		expect(base.selectedTopicID).toBe("sqlite");
		expect(base.highlightID).toBe("wal");
	});

	it("stays put when the block is already in the selected topic", () => {
		base.reveal("done");
		expect(base.selectedTopicID).toBe("go");
		expect(base.highlightID).toBe("done");
	});

	it("un-collapses the ancestors of the revealed block", () => {
		base.collapsed.add("go");
		base.collapsed.add("ctx");
		base.reveal("done");
		expect(base.collapsed.has("go")).toBe(false);
		expect(base.collapsed.has("ctx")).toBe(false);
	});

	it("leaves the selection alone when the block is absent", () => {
		base.reveal("missing");
		expect(base.selectedTopicID).toBe("go");
		expect(base.highlightID).toBe(null);
	});
});

describe("reload's selection fallback", () => {
	beforeEach(() => {
		reloadFixture.topics = [];
		reloadFixture.subtrees = {};
	});

	it("picks the first topic on first load, when nothing is selected", async () => {
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", "")], sqlite: [block("sqlite", "")] };
		base.selectedTopicID = null;

		await base.reload();

		expect(base.selectedTopicID).toBe("go");
	});

	it("falls back to the first surviving topic when the selected one disappeared", async () => {
		reloadFixture.topics = [block("sqlite", "")];
		reloadFixture.subtrees = { sqlite: [block("sqlite", "")] };
		base.selectedTopicID = "go";

		await base.reload();

		expect(base.selectedTopicID).toBe("sqlite");
	});

	it("leaves a surviving selection unchanged", async () => {
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", "")], sqlite: [block("sqlite", "")] };
		base.selectedTopicID = "sqlite";

		await base.reload();

		expect(base.selectedTopicID).toBe("sqlite");
	});

	it("settles on null for an empty base, not a stale id", async () => {
		reloadFixture.topics = [];
		reloadFixture.subtrees = {};
		base.selectedTopicID = "go";

		await base.reload();

		expect(base.selectedTopicID).toBe(null);
	});
});

describe("toggle", () => {
	it("collapses an expanded node", async () => {
		await base.toggle("ctx");
		expect(base.collapsed.has("ctx")).toBe(true);
	});

	it("expands a collapsed node", async () => {
		base.collapsed.add("ctx");
		await base.toggle("ctx");
		expect(base.collapsed.has("ctx")).toBe(false);
	});
});

describe("zoom-in", () => {
	it("makes the focused block the view's root", async () => {
		await base.focus("ctx");
		expect(base.viewRoot?.block.ID).toBe("ctx");
	});

	it("keeps the focused block's own children", async () => {
		await base.focus("ctx");
		expect(base.viewRoot?.children.map((c) => c.block.ID)).toEqual(["done"]);
	});

	it("falls back to the whole topic when nothing is focused", () => {
		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("go");
	});

	it("ignores a block the forest does not have", async () => {
		await base.focus("nope");
		expect(base.focusID).toBe(null);
	});

	// The view's root renders as the page's title row, which carries no collapse
	// triangle. A block collapsed before the zoom would therefore arrive as a
	// lone title on a blank page, with its own dot's halo advertising the
	// children nothing on screen could unfold.
	it("unfolds the block it zooms into", async () => {
		base.collapsed.add("ctx");
		await base.focus("ctx");
		expect(base.collapsed.has("ctx")).toBe(false);
		// TreeNode gates the children <ul> on exactly these two facts.
		expect(base.viewRoot?.children.map((c) => c.block.ID)).toEqual(["done"]);
	});

	it("leaves other rows' fold state alone while zooming", async () => {
		base.collapsed.add("done");
		await base.focus("ctx");
		expect(base.collapsed.has("done")).toBe(true);
	});

	it("zooms back out", async () => {
		await base.focus("ctx");
		base.clearFocus();
		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("go");
	});

	// A focus is a position inside one topic. Leaving that topic must not leave
	// the view pointed at a subtree of a topic nobody is looking at any more.
	it("drops the focus when the topic changes", async () => {
		await base.focus("ctx");
		await base.selectTopic("sqlite");
		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("sqlite");
	});

	it("drops the focus when a jump reveals a block elsewhere", async () => {
		await base.focus("ctx");
		base.reveal("wal");
		expect(base.focusID).toBe(null);
	});

	// reload's own fallback (see "reload's selection fallback" below) moves
	// selectedTopicID off a topic that vanished; that move has to clear the
	// focus too, or viewRoot would keep resolving against the topic the
	// reader is no longer looking at.
	it("drops the focus when reload's fallback moves off a topic that vanished", async () => {
		await base.focus("ctx");
		reloadFixture.topics = [block("sqlite", "")];
		reloadFixture.subtrees = { sqlite: [block("sqlite", "")] };

		await base.reload();

		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("sqlite");
	});

	// The focused block itself can vanish (an edit, or a concurrent comind mcp
	// write) without the topic changing at all. Unlike the cases above, this
	// leaves focusID stale, but viewRoot must not go blank over it.
	it("falls back to the topic when the focused block vanishes but the topic survives", async () => {
		await base.focus("ctx");
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", "")], sqlite: [block("sqlite", "")] };

		await base.reload();

		expect(base.viewRoot?.block.ID).toBe("go");
	});
});

describe("askAI's selection", () => {
	beforeEach(() => {
		base.command = "";
		base.scopeID = null;
		base.busy = false;
		base.editingID = null;
	});

	it("selects the newcomer after an untargeted expansion", async () => {
		base.forest = [node("go", "")];
		base.selectedTopicID = "go";
		base.scopeID = null;
		base.command = "rust";
		reloadFixture.topics = [block("go", ""), block("rust", "")];
		reloadFixture.subtrees = { go: [block("go", "")], rust: [block("rust", "")] };

		await base.askAI();

		expect(base.selectedTopicID).toBe("rust");
	});

	// This move onto the newcomer bypasses reload's own fallback (the prior
	// selection was still valid, so that branch never fires) — it has to
	// clear the focus itself.
	it("drops the focus when an untargeted expansion moves onto the newcomer", async () => {
		base.forest = [node("go", "", [node("ctx", "go")])];
		base.selectedTopicID = "go";
		base.scopeID = null;
		base.command = "rust";
		await base.focus("ctx");
		reloadFixture.topics = [block("go", ""), block("rust", "")];
		reloadFixture.subtrees = { go: [block("go", "")], rust: [block("rust", "")] };

		await base.askAI();

		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("rust");
	});

	it("leaves the selection alone for a scoped edit", async () => {
		base.forest = [node("go", ""), node("sqlite", "")];
		base.selectedTopicID = "go";
		base.scopeID = "go";
		base.command = "restructure this";
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", "")], sqlite: [block("sqlite", "")] };

		await base.askAI();

		expect(base.selectedTopicID).toBe("go");
	});
});

describe("reloadTopic", () => {
	it("replaces one topic's tree and leaves the others untouched", async () => {
		// Rewrite go's subtree only; sqlite must come back identical, and by
		// identity, not merely by shape — an unrelated topic must not be rebuilt.
		const before = base.forest.find((t) => t.block.ID === "sqlite");
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("fresh", "go")];
		await base.reloadTopic("go");
		expect(findNode(base.forest, "fresh")?.block.ID).toBe("fresh");
		expect(base.forest.find((t) => t.block.ID === "sqlite")).toBe(before);
	});

	it("does not refresh the review queue, because an owner write cannot change it", async () => {
		await base.reloadTopic("go");
		expect(changes.refresh).not.toHaveBeenCalled();
	});

	// The trap: GetSubtree omits a block with a pending deletion, so without the
	// splice an owner's edit would make every ghost on the page disappear.
	it("keeps the topic's ghosts, rebuilt from the queue already in memory", async () => {
		changes.blocks = [
			{
				TopicID: "go",
				Head: {
					BlockID: "gone",
					ParentID: "go",
					Position: 1,
					Content: "deleted by the AI",
					Deleted: true,
				},
			},
		] as unknown as typeof changes.blocks;
		base.ghosts.add("gone");
		await base.reloadTopic("go");
		expect(findNode(base.forest, "gone")?.block.Content).toBe("deleted by the AI");
		expect(base.ghosts.has("gone")).toBe(true);
	});

	it("ignores a topic that is not in the forest", async () => {
		const before = base.forest.length;
		await base.reloadTopic("nope");
		expect(base.forest.length).toBe(before);
	});
});

describe("the edit session", () => {
	it("opens with the block's stored text and the caret it was given", async () => {
		base.forest = [node("go", "", [node("ctx", "go", [], "the context package")])];
		await base.startEdit(block("ctx", "go", "the context package"), 4);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("the context package");
		expect(base.editCaret).toBe(4);
	});

	// startEdit reads the forest, not the caller's copy: after a commit the topic
	// is re-read and every block object is new, so a caller holding a block from
	// before that reload is holding stale content. The fixture deliberately
	// disagrees with the argument so a regression to block.Content would fail.
	it("opens with the forest's content, not the caller's stale copy", async () => {
		base.forest = [node("go", "", [node("ctx", "go", [], "what storage holds")])];
		await base.startEdit(block("ctx", "go", "what the caller remembered"), 0);
		expect(base.editText).toBe("what storage holds");
	});

	it("writes nothing when the text did not change", async () => {
		base.forest = [node("go", "", [node("ctx", "go", [], "unchanged")])];
		await base.startEdit(block("ctx", "go", "unchanged"), 0);
		base.editText = "unchanged";
		await base.commitEdit();
		expect(App.EditBlockContent).not.toHaveBeenCalled();
		expect(base.editingID).toBe(null);
	});

	it("writes once when the text changed", async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		await base.commitEdit();
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(base.editingID).toBe(null);
	});

	// Enter, then second thoughts, then leave: nothing left behind.
	it("deletes a block left empty when it has no children", async () => {
		await base.startEdit(block("done", "ctx", "something"), 0);
		base.editText = "";
		await base.commitEdit();
		expect(App.DeleteBlock).toHaveBeenCalledWith("done");
		expect(App.EditBlockContent).not.toHaveBeenCalled();
	});

	it("keeps an empty block that has children, because it still carries structure", async () => {
		await base.startEdit(block("ctx", "go", "something"), 0);
		base.editText = "";
		await base.commitEdit();
		expect(App.DeleteBlock).not.toHaveBeenCalled();
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "");
	});

	it("never deletes a topic root for being empty", async () => {
		await base.startEdit(block("sqlite", "", "sqlite"), 0);
		base.editText = "";
		await base.commitEdit();
		expect(App.DeleteBlock).not.toHaveBeenCalled();
	});

	// Spec §8: losing a paragraph to a bridge failure is not acceptable.
	it("stays in the edit, with the text and caret intact, when the write fails", async () => {
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		base.editCaret = 3;
		await base.commitEdit();
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
		expect(base.editCaret).toBe(3);
		expect(base.error).not.toBe("");
	});

	it("commits the open block before opening another one", async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		await base.startEdit(block("done", "ctx", "done"), 2);
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(base.editingID).toBe("done");
		expect(base.editCaret).toBe(2);
	});

	// The blur handler and the next block's click handler both land here in the
	// same tick; the second must wait for the first, not be dropped by a guard.
	it("shares one write between concurrent commits", async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		await Promise.all([base.commitEdit(), base.commitEdit()]);
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
	});

	it("does nothing when no edit is open", async () => {
		await base.commitEdit();
		expect(App.EditBlockContent).not.toHaveBeenCalled();
	});
});

describe("an open edit no longer locks the interface", () => {
	beforeEach(async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
	});

	it("collapsing an ancestor commits first, then collapses", async () => {
		await base.toggle("go");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(base.collapsed.has("go")).toBe(true);
		expect(base.editingID).toBe(null);
	});

	it("zooming in commits first, then zooms", async () => {
		await base.focus("ctx");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(base.focusID).toBe("ctx");
	});

	it("switching topic commits first", async () => {
		await base.selectTopic("sqlite");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(base.selectedTopicID).toBe("sqlite");
	});

	it("targeting a block for the AI commits first", async () => {
		await base.setScope(block("done", "ctx", "done"));
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(base.scopeID).toBe("done");
	});

	it("sending an AI command commits first", async () => {
		base.command = "tighten this";
		await base.askAI();
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
	});

	it("starting a link commits first", async () => {
		await base.startLink("ctx");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
	});

	// The review controls are rendered outside the editing branch, so ✓, ✗ and
	// the page-wide pair are clickable on the very row being edited — and all
	// five rebuild the whole forest, which drops the row the editor lives in.
	// Without the commit the typing is stranded and the next commitEdit finds no
	// such node and clears it away silently.
	it("accepting a change commits first", async () => {
		await base.accept("done");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(App.AcceptBlock).toHaveBeenCalledWith("done");
	});

	// Rejecting the row being edited throws the owner's write away again, which
	// is a wasted call rather than a wrong one — the hazard this guards is
	// rejecting one row while editing another, and that is the same act.
	it("rejecting a change commits first", async () => {
		await base.reject("done");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(App.RejectBlock).toHaveBeenCalledWith("done");
	});

	it("accepting a page commits first", async () => {
		await base.acceptPage("go");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(App.AcceptPage).toHaveBeenCalledWith("go");
	});

	it("rejecting a page commits first", async () => {
		await base.rejectPage("go");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(App.RejectPage).toHaveBeenCalledWith("go");
	});

	it("deleting a revision commits first", async () => {
		await base.deleteRevision("done", "rev");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(App.DeleteRevision).toHaveBeenCalledWith("done", "rev");
	});
});

// A failed write keeps the session open (spec §8), and that is pointless if
// the row it lives in gets unmounted anyway — so every gesture that would
// unmount the editor aborts instead of proceeding when its own commit fails.
// mockRejectedValueOnce fires once, so each case sets up its own.
describe("a failed commit aborts the gesture", () => {
	beforeEach(async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
	});

	it("leaves the tree alone when the commit that a gesture triggers fails", async () => {
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.toggle("go");
		expect(base.collapsed.has("go")).toBe(false);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});

	it("leaves the focus alone when the commit that zooming in triggers fails", async () => {
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.focus("ctx");
		expect(base.focusID).toBe(null);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});

	it("leaves the selection alone when the commit that switching topic triggers fails", async () => {
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.selectTopic("sqlite");
		expect(base.selectedTopicID).toBe("go");
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});

	it("leaves the scope alone when the commit that targeting a block triggers fails", async () => {
		// scopeID and busy aren't reset by the top-level beforeEach, so both
		// baselines are set explicitly rather than assumed.
		base.scopeID = null;
		base.busy = false;
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.setScope(block("done", "ctx", "done"));
		expect(base.scopeID).toBe(null);
		expect(base.busy).toBe(false);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});

	it("leaves the command alone when the commit that sending an AI command triggers fails", async () => {
		// busy isn't reset by the top-level beforeEach, so the baseline is set
		// explicitly rather than assumed.
		base.busy = false;
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		base.command = "tighten this";
		await base.askAI();
		expect(base.command).toBe("tighten this");
		expect(base.busy).toBe(false);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});

	it("leaves the link picker alone when the commit that starting a link triggers fails", async () => {
		// linkingID and busy aren't reset by the top-level beforeEach, so both
		// baselines are set explicitly here rather than assumed — the point is
		// that they do not move, not that they happen to already be at rest.
		base.linkingID = null;
		base.busy = false;
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.startLink("ctx");
		expect(base.linkingID).toBe(null);
		expect(base.busy).toBe(false);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});

	it("leaves the change unreviewed when the commit that accepting it triggers fails", async () => {
		base.busy = false;
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.accept("done");
		expect(App.AcceptBlock).not.toHaveBeenCalled();
		expect(base.busy).toBe(false);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});

	it("leaves the change unreviewed when the commit that rejecting it triggers fails", async () => {
		base.busy = false;
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.reject("done");
		expect(App.RejectBlock).not.toHaveBeenCalled();
		expect(base.busy).toBe(false);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});

	it("keeps the previous session open when the commit that opening another block triggers fails", async () => {
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.startEdit(block("done", "ctx", "done"), 2);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
	});
});

it("leaving the tree for another mode commits the open edit", async () => {
	await base.startEdit(block("ctx", "go", "before"), 0);
	base.editText = "after";
	await shell.setMode("graph");
	expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
	expect(shell.mode).toBe("graph");
});

it("leaves the mode alone when the commit that switching modes triggers fails", async () => {
	shell.mode = "tree";
	await base.startEdit(block("ctx", "go", "before"), 0);
	base.editText = "after";
	vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
	await shell.setMode("graph");
	expect(shell.mode).toBe("tree");
	expect(base.editingID).toBe("ctx");
	expect(base.editText).toBe("after");
});

describe("Enter", () => {
	it("splits at the caret, keeping the head and moving the caret into the tail", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce("new");
		// startEdit reads the forest, not the argument (see "the edit session"
		// above), so the fixture carries the text under test rather than the
		// default seeded forest's "done" placeholder.
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx", [], "head and tail")])])];
		await base.startEdit(block("done", "ctx", "head and tail"), 4);
		await base.splitAtCaret("done", 4);
		expect(App.SplitBlock).toHaveBeenCalledWith("done", "head", " and tail");
		expect(base.editingID).toBe("new");
		expect(base.editText).toBe(" and tail");
		expect(base.editCaret).toBe(0);
	});

	// The ID carries history, review state and links; Enter at the start must not
	// move the text onto a new one.
	it("inserts an empty block above when the caret is at the start of non-empty text", async () => {
		vi.mocked(App.InsertBlockBefore).mockResolvedValueOnce("above");
		await base.startEdit(block("done", "ctx", "untouched"), 0);
		await base.insertAbove("done");
		expect(App.InsertBlockBefore).toHaveBeenCalledWith("done", "");
		expect(App.SplitBlock).not.toHaveBeenCalled();
		expect(base.editingID).toBe("done");
		expect(base.editCaret).toBe(0);
	});

	it("splits an empty block rather than inserting above it", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce("new");
		// Same reason as above: the forest, not the argument, is what startEdit
		// reads, so it has to actually hold the empty content under test.
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx", [], "")])])];
		await base.startEdit(block("done", "ctx", ""), 0);
		await base.splitAtCaret("done", 0);
		expect(App.SplitBlock).toHaveBeenCalledWith("done", "", "");
		expect(base.editingID).toBe("new");
	});

	it("splits with the text currently in the editor, not the stored text", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce("new");
		await base.startEdit(block("done", "ctx", "old"), 0);
		base.editText = "typed more";
		await base.splitAtCaret("done", 5);
		expect(App.SplitBlock).toHaveBeenCalledWith("done", "typed", " more");
	});

	// The view's root is the page: it has no siblings to split into, so Enter
	// on the title row does nothing structural, whether or not the reader is
	// zoomed into it.
	it("refuses to split the page's own title row", async () => {
		await base.startEdit(block("go", "", "go"), 1);
		await base.splitAtCaret("go", 1);
		expect(App.SplitBlock).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	it("refuses to insert above the page's own title row", async () => {
		await base.startEdit(block("go", "", "go"), 0);
		await base.insertAbove("go");
		expect(App.InsertBlockBefore).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});
});

describe("Tab and Shift+Tab", () => {
	it("indents under the previous sibling", async () => {
		// gen is ctx's next sibling, so Tab puts it under ctx.
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("gen", "go")];
		await base.reloadTopic("go");
		await base.startEdit(block("gen", "go", "gen"), 0);
		await base.indent("gen");
		expect(App.IndentBlock).toHaveBeenCalledWith("gen");
		expect(base.editingID).toBe("gen");
	});

	// Spec §8: a gesture with no target is a silent no-op, never a message — and
	// never a bridge call, which is what keeps invalid_ops meaning a real fault.
	it("is a silent no-op for a first child", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.indent("ctx");
		expect(App.IndentBlock).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	// prevSibling is already null for the root at index 0, so this passes today
	// by accident of position; it stays here so a future reordering of the
	// forest cannot silently turn the accident into a bridge call.
	it("refuses to indent the page's own title row", async () => {
		await base.startEdit(block("go", "", "go"), 0);
		await base.indent("go");
		expect(App.IndentBlock).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	it("outdents to the parent's next sibling slot", async () => {
		await base.startEdit(block("done", "ctx", "done"), 0);
		await base.outdent("done");
		expect(App.OutdentBlock).toHaveBeenCalledWith("done");
	});

	it("is a silent no-op for a child of the view's root", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.outdent("ctx");
		expect(App.OutdentBlock).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	// Without isViewRoot, the guard above (node.block.ParentID === root.block.ID)
	// does not catch the root itself: its own ParentID is "", never its own ID.
	it("refuses to outdent the page's own title row", async () => {
		await base.startEdit(block("go", "", "go"), 0);
		await base.outdent("go");
		expect(App.OutdentBlock).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	it("writes the pending text before moving the block", async () => {
		await base.startEdit(block("done", "ctx", "before"), 0);
		base.editText = "after";
		await base.outdent("done");
		expect(App.EditBlockContent).toHaveBeenCalledWith("done", "after");
	});
});

describe("Backspace at the start", () => {
	it("merges into the block above and puts the caret at the seam", async () => {
		await base.startEdit(block("done", "ctx", "tail"), 0);
		await base.mergeIntoPrevious("done");
		expect(App.MergeBlocks).toHaveBeenCalledWith("ctx", "done");
		expect(base.editingID).toBe("ctx");
		// ctx's stored content is "ctx" in the seeded forest: the seam is its length.
		expect(base.editCaret).toBe(3);
	});

	// ctx's predecessor in the visible order is "go" — the page's title row. It is
	// visible, so prevVisible returns it, and the refusal has to be by name.
	it("is a silent no-op in the first block of the page, whose predecessor is the title", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.mergeIntoPrevious("ctx");
		expect(App.MergeBlocks).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	// The target is the block visible above — which, when it is collapsed, is the
	// collapsed block itself, not the descendant it is hiding. It is expanded so
	// the reader can see where the text went, and it stays the target: spec §4.3
	// says unfold the target, not pick a different one.
	it("expands a collapsed target rather than merging into what it hides", async () => {
		base.collapsed.add("ctx");
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("done", "ctx"), block("gen", "go")];
		await base.reloadTopic("go");
		await base.startEdit(block("gen", "go", "gen"), 0);
		await base.mergeIntoPrevious("gen");
		expect(base.collapsed.has("ctx")).toBe(false);
		expect(App.MergeBlocks).toHaveBeenCalledWith("ctx", "gen");
		expect(base.editCaret).toBe(3); // the seam, at the end of "ctx"
	});

	// prevVisible is already null for the root at index 0, so this passes today
	// by accident of position, same as indent's equivalent test above.
	it("refuses to merge the page's own title row into anything", async () => {
		await base.startEdit(block("go", "", "go"), 0);
		await base.mergeIntoPrevious("go");
		expect(App.MergeBlocks).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	// The merged text comes back from the re-read, not from what the editor held
	// before the gesture — otherwise leaving the block would write the pre-merge
	// text back over the join and silently undo it. This also pins the ordering:
	// refreshed before the re-read, editText would still be "done".
	it("shows the joined text after a merge, not the text from before it", async () => {
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go", "ctxdone")];
		await base.startEdit(block("done", "ctx", "done"), 0);
		await base.mergeIntoPrevious("done");
		expect(base.editText).toBe("ctxdone");
	});
});

// The editor places the caret from the store, in an effect that runs when it
// mounts and whenever a placement is announced. Two things have to hold for the
// caret to land where the gesture meant it to: the text must already be final
// when the placement happens (otherwise bind:value assigns el.value afterwards,
// and the HTML spec moves the caret to the end of a textarea whose value is
// written), and the announcement must be an edge rather than a value (otherwise
// a placement that repeats the previous offset is not an announcement at all).
describe("placing the caret", () => {
	it("moves the editor only after the re-read, so it never mounts on the old text", async () => {
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go", "ctxdone")];
		const during: { id: string | null; text: string }[] = [];
		reloadFixture.onSubtree = () => {
			during.push({ id: base.editingID, text: base.editText });
		};

		await base.startEdit(block("done", "ctx", "done"), 0);
		await base.mergeIntoPrevious("done");

		// Mid-merge the session is still the source block's, with the source's
		// text: nothing has been placed on the target yet.
		expect(during).toEqual([{ id: "done", text: "done" }]);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("ctxdone");
		expect(base.editCaret).toBe(3);
	});

	it("bumps the nonce when a session opens and when it closes", async () => {
		const before = base.editCaretNonce;
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		expect(base.editCaretNonce).toBe(before + 1);
		base.clearEdit();
		expect(base.editCaretNonce).toBe(before + 2);
	});

	it("bumps the nonce for a gesture that lands on the offset it was already at", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		const opened = base.editCaretNonce;

		await base.moveCaret("ctx", "down", 0);

		// Same offset, different block: without the bump the editor's effect has
		// nothing to re-run on and the caret stays wherever the DOM left it.
		expect(base.editingID).toBe("done");
		expect(base.editCaret).toBe(0);
		expect(base.editCaretNonce).toBe(opened + 1);
	});

	it("bumps the nonce for a merge and for a split", async () => {
		await base.startEdit(block("done", "ctx", "done"), 0);
		const opened = base.editCaretNonce;
		await base.mergeIntoPrevious("done");
		expect(base.editCaretNonce).toBe(opened + 1);

		vi.mocked(App.SplitBlock).mockResolvedValueOnce("new");
		await base.splitAtCaret("ctx", 1);
		expect(base.editCaretNonce).toBe(opened + 2);
	});
});

describe("Delete at the end", () => {
	it("pulls the next visible block into this one", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 3);
		await base.pullNextInto("ctx", 3);
		expect(App.MergeBlocks).toHaveBeenCalledWith("ctx", "done");
		expect(base.editingID).toBe("ctx");
		// The caret stays where it was — Delete does not move it, unlike Backspace.
		expect(base.editCaret).toBe(3);
	});

	// The caret the reader is at is the editor's, not the store's: editCaret is
	// only ever written by the store, so it says where this session *opened* —
	// here a click at the start, before the reader arrowed to the end.
	it("lands the caret where the reader is, not where the editor opened", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.pullNextInto("ctx", 3);
		expect(base.editCaret).toBe(3);
	});

	it("is a silent no-op in the last block of the page", async () => {
		await base.startEdit(block("done", "ctx", "done"), 4);
		await base.pullNextInto("done", 4);
		expect(App.MergeBlocks).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	it("refuses to pull anything into the page's own title row", async () => {
		await base.startEdit(block("go", "", "go"), 0);
		await base.pullNextInto("go", 0);
		expect(App.MergeBlocks).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});
});

// A pending deletion renders — struck through, so the reader can review it in
// place — but storage dropped the block, so naming it in a call is either a
// not_found from the bridge or, worse for Tab, a silent move under a different
// sibling than the one the reader saw above the row. Every gesture walks past it.
describe("a ghost is on screen but no gesture reaches it", () => {
	// The AI proposed deleting "gone", which sits between two live siblings.
	function ghostBetween(): void {
		base.forest = [node("go", "", [node("ctx", "go"), node("gone", "go"), node("gen", "go")])];
		base.selectedTopicID = "go";
		base.ghosts.add("gone");
		changes.blocks = [
			{
				TopicID: "go",
				Head: { BlockID: "gone", ParentID: "go", Position: 1, Content: "gone", Deleted: true },
			},
		] as unknown as typeof changes.blocks;
		// What storage actually holds: no ghost row.
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("gen", "go")];
	}

	// The ghost is the only row between the page title and gen, so with it in the
	// order every gesture below has a target that does not exist.
	function ghostAlone(): void {
		base.forest = [node("go", "", [node("gone", "go"), node("gen", "go")])];
		base.selectedTopicID = "go";
		base.ghosts.add("gone");
		reloadFixture.subtrees.go = [block("go", ""), block("gen", "go")];
	}

	it("merges Backspace into the live block above, not into the ghost", async () => {
		ghostBetween();
		await base.startEdit(block("gen", "go", "gen"), 0);
		await base.mergeIntoPrevious("gen");
		expect(App.MergeBlocks).toHaveBeenCalledWith("ctx", "gen");
		expect(base.editCaret).toBe(3); // the seam, at the end of "ctx"
	});

	it("makes Backspace a silent no-op when only the ghost stands above", async () => {
		// Past the ghost, gen's predecessor is the page's own title row, which is
		// never a merge target — so this is the same silent no-op as the first
		// block of a page, not a call the bridge would refuse.
		ghostAlone();
		await base.startEdit(block("gen", "go", "gen"), 0);
		await base.mergeIntoPrevious("gen");
		expect(App.MergeBlocks).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	it("indents under the live sibling above, not under the ghost", async () => {
		ghostBetween();
		await base.startEdit(block("gen", "go", "gen"), 0);
		await base.indent("gen");
		expect(App.IndentBlock).toHaveBeenCalledWith("gen");
	});

	// The dangerous one: IndentBlock recomputes the sibling list from storage,
	// where the ghost is absent, so a call here either indents under a different
	// sibling than the reader saw or comes back invalid_ops for a gesture the
	// frontend asserted was applicable.
	it("makes Tab a silent no-op when the only sibling above is a ghost", async () => {
		ghostAlone();
		await base.startEdit(block("gen", "go", "gen"), 0);
		await base.indent("gen");
		expect(App.IndentBlock).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	it("pulls the live block below into this one, not the ghost", async () => {
		ghostBetween();
		await base.startEdit(block("ctx", "go", "ctx"), 3);
		await base.pullNextInto("ctx", 3);
		expect(App.MergeBlocks).toHaveBeenCalledWith("ctx", "gen");
	});

	it("moves the caret past the ghost rather than into it", async () => {
		ghostBetween();
		await base.startEdit(block("gen", "go", "gen"), 0);
		await base.moveCaret("gen", "up", 0);
		// The ghost row renders no editor at all, so landing on it would lose the
		// caret and the keyboard with it.
		expect(base.editingID).toBe("ctx");
	});

	it("moves down past the ghost as well", async () => {
		ghostBetween();
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.moveCaret("ctx", "down", 0);
		expect(base.editingID).toBe("gen");
	});
});

describe("the arrows", () => {
	it("moves up into the block above, keeping the column", async () => {
		await base.startEdit(block("done", "ctx", "done"), 2);
		await base.moveCaret("done", "up", 2);
		expect(base.editingID).toBe("ctx");
		expect(base.editCaret).toBe(2);
	});

	it("clamps the column to the target's shorter line", async () => {
		await base.startEdit(block("done", "ctx", "done"), 4);
		await base.moveCaret("done", "up", 40);
		expect(base.editingID).toBe("ctx");
		expect(base.editCaret).toBe(3); // "ctx"
	});

	it("moves down into the block below", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.moveCaret("ctx", "down", 0);
		expect(base.editingID).toBe("done");
		expect(base.editCaret).toBe(0);
	});

	it("stays put at the ends of the visible order", async () => {
		await base.startEdit(block("done", "ctx", "done"), 0);
		await base.moveCaret("done", "down", 0);
		expect(base.editingID).toBe("done");
	});

	it("writes the pending text before leaving the block", async () => {
		await base.startEdit(block("done", "ctx", "before"), 0);
		base.editText = "after";
		await base.moveCaret("done", "up", 0);
		expect(App.EditBlockContent).toHaveBeenCalledWith("done", "after");
	});
});
