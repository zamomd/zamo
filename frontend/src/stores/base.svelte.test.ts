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
		GetSubtree: async (id: string) => reloadFixture.subtrees[id] ?? [],
		ExpandTopic: async () => {},
		EditWithAI: async () => {},
	},
}));
vi.mock("./changes.svelte", () => ({
	changes: { blocks: [], edges: [], count: 0, refresh: async () => {} },
}));

import type { TreeNodeData } from "../lib/tree";
import { base } from "./base.svelte";

function block(id: string, parent: string): Block {
	return {
		ID: id,
		ParentID: parent,
		Position: 0,
		Content: id,
		Meta: {},
		CreatedAt: "",
		UpdatedAt: "",
		Unreviewed: false,
	};
}

function node(id: string, parent: string, children: TreeNodeData[] = []): TreeNodeData {
	return { block: block(id, parent), children };
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
	reloadFixture.topicMapError = null;
	reloadFixture.gate = null;
});

describe("selectTopic", () => {
	it("switches the main area to another topic", () => {
		base.selectTopic("sqlite");
		expect(base.selectedTopicID).toBe("sqlite");
	});

	it("exposes that topic's tree", () => {
		base.selectTopic("sqlite");
		expect(base.selectedTree?.block.ID).toBe("sqlite");
	});

	it("clears a stale jump flash from the previous topic", () => {
		base.highlightID = "done";
		base.selectTopic("sqlite");
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

describe("zoom-in", () => {
	it("makes the focused block the view's root", () => {
		base.focus("ctx");
		expect(base.viewRoot?.block.ID).toBe("ctx");
	});

	it("keeps the focused block's own children", () => {
		base.focus("ctx");
		expect(base.viewRoot?.children.map((c) => c.block.ID)).toEqual(["done"]);
	});

	it("falls back to the whole topic when nothing is focused", () => {
		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("go");
	});

	it("ignores a block the forest does not have", () => {
		base.focus("nope");
		expect(base.focusID).toBe(null);
	});

	it("zooms back out", () => {
		base.focus("ctx");
		base.clearFocus();
		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("go");
	});

	// A focus is a position inside one topic. Leaving that topic must not leave
	// the view pointed at a subtree of a topic nobody is looking at any more.
	it("drops the focus when the topic changes", () => {
		base.focus("ctx");
		base.selectTopic("sqlite");
		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("sqlite");
	});

	it("drops the focus when a jump reveals a block elsewhere", () => {
		base.focus("ctx");
		base.reveal("wal");
		expect(base.focusID).toBe(null);
	});

	// reload's own fallback (see "reload's selection fallback" below) moves
	// selectedTopicID off a topic that vanished; that move has to clear the
	// focus too, or viewRoot would keep resolving against the topic the
	// reader is no longer looking at.
	it("drops the focus when reload's fallback moves off a topic that vanished", async () => {
		base.focus("ctx");
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
		base.focus("ctx");
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
		base.focus("ctx");
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
