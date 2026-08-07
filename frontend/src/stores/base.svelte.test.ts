import { beforeEach, describe, expect, it, vi } from "vitest";
import { type Block, Mode } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}));

const reloadFixture = vi.hoisted(() => ({
	topics: [] as Block[],
	subtrees: {} as Record<string, Block[]>,
	topicMapError: null as Error | null,
	gate: null as Promise<void> | null,
	writeGate: null as Promise<void> | null,
	onSubtree: null as ((id: string) => void) | null,
	backlinks: {} as Record<string, unknown[]>,
	backlinksError: null as Error | null,
	createdTopicID: "",
	createTopicError: null as Error | null,
	expandByIDError: null as Error | null,
}));
const change = vi.hoisted(() => (GroupID: string, BlockID = "") => ({ GroupID, BlockID }));
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
		ExpandTopic: vi.fn(async () => {}),
		ExpandTopicByID: vi.fn(async () => {
			if (reloadFixture.expandByIDError !== null) {
				throw reloadFixture.expandByIDError;
			}
		}),
		CreateTopic: vi.fn(async (_name: string, _mode: string) => {
			if (reloadFixture.createTopicError !== null) {
				throw reloadFixture.createTopicError;
			}
			return reloadFixture.createdTopicID;
		}),
		SetTopicMode: vi.fn(async () => {}),
		Backlinks: vi.fn(async (id: string) => {
			if (reloadFixture.backlinksError !== null) {
				throw reloadFixture.backlinksError;
			}
			return reloadFixture.backlinks[id] ?? [];
		}),
		EditWithAI: vi.fn(async () => {}),
		EditBlockContent: vi.fn(async () => {
			if (reloadFixture.writeGate !== null) {
				await reloadFixture.writeGate;
			}
		}),
		DeleteBlock: vi.fn(async () => change("grp")),
		SplitBlock: vi.fn(async () => change("grp", "new")),
		InsertBlockBefore: vi.fn(async () => change("grp", "above")),
		IndentBlock: vi.fn(async () => change("grp")),
		OutdentBlock: vi.fn(async () => change("grp")),
		MergeBlocks: vi.fn(async () => change("grp")),
		UndoGroup: vi.fn(async () => {}),
		AcceptBlock: vi.fn(async () => {}),
		RejectBlock: vi.fn(async () => {}),
		AcceptPage: vi.fn(async () => {}),
		RejectPage: vi.fn(async () => {}),
		DeleteRevision: vi.fn(async () => {}),
		BlockHistory: vi.fn(async () => []),
	},
}));
vi.mock("./changes.svelte", () => ({
	changes: { blocks: [], count: 0, refresh: vi.fn(async () => {}) },
}));

import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";
import { parseRefs } from "../lib/refs";
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
		Mode: Mode.ModeOutline,
		Meta: {},
		CreatedAt: "",
		UpdatedAt: "",
		Unreviewed: false,
	};
}

function node(id: string, parent: string, children: TreeNodeData[] = [], content = id): TreeNodeData {
	return { block: block(id, parent, content), children };
}

function backlink(id: string, topicID: string) {
	return { Block: block(id, topicID), TopicID: topicID, TopicTitle: topicID };
}

// A real 26-character Crockford ULID: a short one is not a block reference at
// all, and a test built on one proves nothing.
const ulid = "01ARZ3NDEKTSV4RRFFQ69G5FAV";

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
	base.clearEdit();
	base.clearUndoHistory();
	base.ghosts.clear();
	base.busy = false;
	base.historyID = null;
	base.revisions = [];
	base.openBacklinks.clear();
	base.backlinksById.clear();
	reloadFixture.backlinks = {};
	reloadFixture.backlinksError = null;
	reloadFixture.createdTopicID = "";
	reloadFixture.createTopicError = null;
	reloadFixture.expandByIDError = null;
	reloadFixture.topicMapError = null;
	reloadFixture.gate = null;
	reloadFixture.writeGate = null;
	reloadFixture.onSubtree = null;
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

	it("reports rather than blanks the pane when the topic is gone even after a re-read", async () => {
		base.error = "";
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", "")], sqlite: [block("sqlite", "")] };

		await base.selectTopic("nonexistent");

		expect(base.error).not.toBe("");
		expect(base.selectedTopicID).toBe("go");
		expect(base.selectedTree).not.toBe(null);
	});

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
		await base.selectTopic("sqlite");
		release();
		await stale;

		expect(base.selectedTopicID).toBe("sqlite");
	});

	it("clears a previous selection's error once a later one lands", async () => {
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", "")], sqlite: [block("sqlite", "")] };

		await base.selectTopic("nonexistent");
		expect(base.error).not.toBe("");

		await base.selectTopic("sqlite");

		expect(base.selectedTopicID).toBe("sqlite");
		expect(base.error).toBe("");
	});

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

	it("unfolds the block it zooms into", async () => {
		base.collapsed.add("ctx");
		await base.focus("ctx");
		expect(base.collapsed.has("ctx")).toBe(false);
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

	it("drops the focus when reload's fallback moves off a topic that vanished", async () => {
		await base.focus("ctx");
		reloadFixture.topics = [block("sqlite", "")];
		reloadFixture.subtrees = { sqlite: [block("sqlite", "")] };

		await base.reload();

		expect(base.focusID).toBe(null);
		expect(base.viewRoot?.block.ID).toBe("sqlite");
	});

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

	it("reloads the tree after a scoped edit, not just after an untargeted expansion", async () => {
		base.forest = [node("go", ""), node("sqlite", "")];
		base.selectedTopicID = "go";
		base.scopeID = "go";
		base.command = "restructure this";
		reloadFixture.topics = [block("go", ""), block("sqlite", "")];
		reloadFixture.subtrees = { go: [block("go", ""), block("fresh", "go")], sqlite: [block("sqlite", "")] };

		await base.askAI();

		expect(App.EditWithAI).toHaveBeenCalledWith("go", "restructure this");
		expect(findNode(base.forest, "fresh")?.block.ID).toBe("fresh");
	});
});

describe("reloadTopic", () => {
	it("replaces one topic's tree and leaves the others untouched", async () => {
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

	it("closes the history panel of a topic that vanished entirely", async () => {
		base.historyID = "ctx";
		base.revisions = [{ ID: "rev" }] as unknown as typeof base.revisions;
		reloadFixture.subtrees.go = [];

		await base.reloadTopic("go");

		expect(base.forest.some((t) => t.block.ID === "go")).toBe(false);
		expect(base.historyID).toBe(null);
		expect(base.revisions).toEqual([]);
	});

	it("leaves the history panel of a surviving block alone", async () => {
		base.historyID = "wal";
		reloadFixture.subtrees.go = [];

		await base.reloadTopic("go");

		expect(base.historyID).toBe("wal");
	});

	// The common flow: type a reference into a block, blur, runCommit,
	// reloadTopic. The panel that exists to list that reference must not keep
	// the count it had before the edit.
	it("refreshes an open backlink panel", async () => {
		reloadFixture.backlinks.ctx = [backlink("wal", "sqlite")];
		await base.toggleBacklinks("ctx");
		reloadFixture.backlinks.ctx = [backlink("wal", "sqlite"), backlink("done", "go")];

		await base.reloadTopic("go");

		expect(base.backlinksById.get("ctx")).toHaveLength(2);
	});

	it("closes a backlink panel whose block left the forest", async () => {
		await base.toggleBacklinks("ctx");
		reloadFixture.subtrees.go = [block("go", "")];

		await base.reloadTopic("go");

		expect(base.openBacklinks.has("ctx")).toBe(false);
		expect(base.backlinksById.has("ctx")).toBe(false);
	});
});

describe("backlink panels", () => {
	it("reads the backlinks when a panel opens", async () => {
		reloadFixture.backlinks.ctx = [backlink("wal", "sqlite")];

		await base.toggleBacklinks("ctx");

		expect(base.openBacklinks.has("ctx")).toBe(true);
		expect(base.backlinksById.get("ctx")).toEqual([backlink("wal", "sqlite")]);
		expect(App.Backlinks).toHaveBeenCalledTimes(1);
	});

	it("drops the panel and its answer when it closes", async () => {
		await base.toggleBacklinks("ctx");
		await base.toggleBacklinks("ctx");

		expect(base.openBacklinks.has("ctx")).toBe(false);
		expect(base.backlinksById.has("ctx")).toBe(false);
	});

	it("reads again when a closed panel reopens, because closing dropped the answer", async () => {
		await base.toggleBacklinks("ctx");
		await base.toggleBacklinks("ctx");
		await base.toggleBacklinks("ctx");

		expect(App.Backlinks).toHaveBeenCalledTimes(2);
	});

	it("does not read when the panel already has an answer", async () => {
		await base.refreshBacklinks("ctx");

		await base.toggleBacklinks("ctx");

		expect(App.Backlinks).toHaveBeenCalledTimes(1);
	});

	it("reports a failed read and leaves the panel without a stale answer", async () => {
		reloadFixture.backlinksError = new Error("storage is gone");

		await base.refreshBacklinks("ctx");

		expect(base.error).not.toBe("");
		expect(base.backlinksById.has("ctx")).toBe(false);
	});
});

describe("acceptRef", () => {
	it("replaces the trigger span with a topic reference and lands the caret past it", () => {
		base.editText = "See [[Loc more";
		base.acceptRef(4, 9, "[[Local-first]]");

		expect(base.editText).toBe("See [[Local-first]] more");
		expect(base.editCaret).toBe(19);
		expect(base.editText.slice(base.editCaret)).toBe(" more");
	});

	it("replaces the trigger span with a block reference", () => {
		base.editText = "Cite ((cla later";
		base.acceptRef(5, 10, `((${ulid}|A claim))`);

		expect(base.editText).toBe(`Cite ((${ulid}|A claim)) later`);
		expect(base.editText.slice(base.editCaret)).toBe(" later");
		expect(parseRefs(base.editText)).toEqual([{ kind: "block", targetKey: ulid }]);
	});

	// The offsets are the only thing standing between the popup and a corrupted
	// block, so the result has to parse back out as the reference intended.
	it("writes text the parser reads back as one reference", () => {
		base.editText = "See [[Loc more";
		base.acceptRef(4, 9, "[[Local-first]]");

		expect(parseRefs(base.editText)).toEqual([{ kind: "topic", targetKey: "local-first" }]);
	});

	it("handles a span at the very start and one running to the end", () => {
		base.editText = "[[Zi";
		base.acceptRef(0, 4, "[[Zig]]");

		expect(base.editText).toBe("[[Zig]]");
		expect(base.editCaret).toBe(7);
	});

	it("bumps the caret nonce so the editor re-places the caret", () => {
		const before = base.editCaretNonce;
		base.editText = "[[Zi";
		base.acceptRef(0, 4, "[[Zig]]");

		expect(base.editCaretNonce).toBeGreaterThan(before);
	});
});

describe("createTopic", () => {
	it("creates the topic and selects it", async () => {
		reloadFixture.createdTopicID = "zig";
		reloadFixture.topics = [block("go", ""), block("sqlite", ""), block("zig", "")];
		reloadFixture.subtrees.zig = [block("zig", "")];

		await base.createTopic("Zig");

		expect(App.CreateTopic).toHaveBeenCalledWith("Zig", "outline");
		expect(base.selectedTopicID).toBe("zig");
		expect(base.error).toBe("");
	});

	it("refuses while another operation is running", async () => {
		base.busy = true;

		await base.createTopic("Zig");

		expect(App.CreateTopic).not.toHaveBeenCalled();
	});

	it("reports a name already taken and clears busy", async () => {
		reloadFixture.createTopicError = new Error("boom", {
			cause: { code: "duplicate_topic", message: "topic name already taken" },
		});

		await base.createTopic("Go");

		expect(base.error).toBe("A topic with that name already exists.");
		expect(base.busy).toBe(false);
	});
});

describe("topicMode", () => {
	it("reads a topic root in doc mode as a document", () => {
		const doc = node("doc", "", [], "# Doc");
		doc.block.Mode = Mode.ModeDoc;
		base.forest = [doc];
		base.selectedTopicID = "doc";

		expect(base.topicMode).toBe("doc");
	});

	it("reads an ordinary topic as an outline", () => {
		expect(base.topicMode).toBe("outline");
	});
});

describe("editing a document", () => {
	it("commits through the ordinary block write path", async () => {
		const doc = node("doc", "", [], "# Doc\n\nOld.");
		doc.block.Mode = Mode.ModeDoc;
		base.forest = [doc];
		base.selectedTopicID = "doc";

		await base.startEdit(doc.block, 0);
		base.editText = "# Doc\n\nNew.";
		expect(await base.commitEdit()).toBe(true);

		expect(App.EditBlockContent).toHaveBeenCalledWith("doc", "# Doc\n\nNew.");
	});
});

describe("asking the AI to write an existing topic", () => {
	beforeEach(() => {
		base.busy = false;
		base.editingID = null;
	});

	it("expands the topic in place instead of creating a new one", async () => {
		base.forest = [node("go", "")];
		base.selectedTopicID = "go";
		reloadFixture.topics = [block("go", "")];
		reloadFixture.subtrees = { go: [block("go", ""), block("written", "go")] };

		await base.expandTopic("go");

		expect(App.ExpandTopicByID).toHaveBeenCalledWith("go", "");
		expect(App.ExpandTopic).not.toHaveBeenCalled();
		expect(findNode(base.forest, "written")?.block.ID).toBe("written");
		expect(base.status).toBe("changes applied, awaiting review");
	});

	it("does nothing while another write is in flight", async () => {
		base.busy = true;
		await base.expandTopic("go");

		expect(App.ExpandTopicByID).not.toHaveBeenCalled();
	});

	it("reports a refusal without leaving the interface busy", async () => {
		// The refusal an AI-off base answers with; the store must surface it
		// rather than the generic fallback, and must not stay busy.
		reloadFixture.expandByIDError = Object.assign(new Error("ai disabled"), {
			cause: { code: "ai_disabled", message: "AI features are switched off" },
		});
		await base.expandTopic("go");

		expect(base.error).toContain("switched off");
		expect(base.busy).toBe(false);
	});
});

describe("choosing a topic's shape", () => {
	it("creates a topic in the mode asked for", async () => {
		reloadFixture.createdTopicID = "new";
		await base.createTopic("Go Contexts", "doc");

		expect(App.CreateTopic).toHaveBeenCalledWith("Go Contexts", "doc");
	});

	it("creates a topic from an unresolved reference as an outline", async () => {
		reloadFixture.createdTopicID = "new";
		await base.createTopic("Go Contexts");

		expect(App.CreateTopic).toHaveBeenCalledWith("Go Contexts", "outline");
	});

	it("converts the selected topic and reloads it", async () => {
		const seen: string[] = [];
		reloadFixture.onSubtree = (id) => seen.push(id);

		await base.setTopicMode("doc");

		expect(App.SetTopicMode).toHaveBeenCalledWith("go", "doc");
		expect(seen).toContain("go");
	});

	// The conversion awaits review as one batch, and every screen that offers
	// to give it back reads the pending-change snapshot: the tree's ghosts for
	// the children it tombstoned, the Changes screen for the batch itself.
	// Re-reading the topic alone leaves all of that showing the state before.
	it("re-reads the pending changes so the conversion can be given back", async () => {
		await base.setTopicMode("doc");

		expect(changes.refresh).toHaveBeenCalled();
	});

	it("does nothing while another write is in flight", async () => {
		base.busy = true;
		await base.setTopicMode("doc");

		expect(App.SetTopicMode).not.toHaveBeenCalled();
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

	it("shares one write between concurrent commits", async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		await Promise.all([base.commitEdit(), base.commitEdit()]);
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
	});

	it("exposes a standalone commit as in flight until its write settles", async () => {
		let releaseWrite: (() => void) | undefined;
		reloadFixture.writeGate = new Promise<void>((resolve) => {
			releaseWrite = resolve;
		});
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";

		const commit = base.commitEdit();
		expect(base.busy).toBe(false);
		expect(base.commitInFlight).toBe(true);

		releaseWrite?.();
		await commit;
		expect(base.commitInFlight).toBe(false);
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

	it("accepting a change commits first", async () => {
		await base.accept("done");
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(App.AcceptBlock).toHaveBeenCalledWith("done");
	});

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
		base.busy = false;
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		base.command = "tighten this";
		await base.askAI();
		expect(base.command).toBe("tighten this");
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
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("grp", "new"));
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx", [], "head and tail")])])];
		await base.startEdit(block("done", "ctx", "head and tail"), 4);
		await base.splitAtCaret("done", 4);
		expect(App.SplitBlock).toHaveBeenCalledWith("done", "head", " and tail");
		expect(base.editingID).toBe("new");
		expect(base.editText).toBe(" and tail");
		expect(base.editCaret).toBe(0);
	});

	it("inserts an empty block above when the caret is at the start of non-empty text", async () => {
		vi.mocked(App.InsertBlockBefore).mockResolvedValueOnce(change("grp", "above"));
		await base.startEdit(block("done", "ctx", "untouched"), 0);
		await base.insertAbove("done");
		expect(App.InsertBlockBefore).toHaveBeenCalledWith("done", "");
		expect(App.SplitBlock).not.toHaveBeenCalled();
		expect(base.editingID).toBe("done");
		expect(base.editCaret).toBe(0);
	});

	it("splits an empty block rather than inserting above it", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("grp", "new"));
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx", [], "")])])];
		await base.startEdit(block("done", "ctx", ""), 0);
		await base.splitAtCaret("done", 0);
		expect(App.SplitBlock).toHaveBeenCalledWith("done", "", "");
		expect(base.editingID).toBe("new");
	});

	it("drops the selected run rather than keeping it in both halves", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("grp", "new"));
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx", [], "head SELECTED tail")])])];
		await base.startEdit(block("done", "ctx", "head SELECTED tail"), 5);
		await base.splitAtCaret("done", 5, 13);
		expect(App.SplitBlock).toHaveBeenCalledWith("done", "head ", " tail");
	});

	it("splits with the text currently in the editor, not the stored text", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("grp", "new"));
		await base.startEdit(block("done", "ctx", "old"), 0);
		base.editText = "typed more";
		await base.splitAtCaret("done", 5);
		expect(App.SplitBlock).toHaveBeenCalledWith("done", "typed", " more");
	});

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

describe("undo and redo", () => {
	function seedCtx(...children: Block[]): void {
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), ...children];
	}

	async function atSeam(): Promise<void> {
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx", [], "head tail")])])];
		await base.startEdit(block("done", "ctx", "head tail"), 4);
	}

	it("takes a split back through the batch that wrote it", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("split-1", "tail"));
		await atSeam();
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", " tail"));
		await base.splitAtCaret("done", 4);
		expect(base.canUndo()).toBe(true);

		seedCtx(block("done", "ctx", "head tail"));
		await base.undo();

		expect(App.UndoGroup).toHaveBeenCalledWith("split-1");
		expect(base.editingID).toBe("done");
		expect(base.editText).toBe("head tail");
		expect(base.editCaret).toBe(4);
	});

	it("takes an indent back", async () => {
		vi.mocked(App.IndentBlock).mockResolvedValueOnce(change("indent-1"));
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("gen", "go")];
		await base.reloadTopic("go");
		await base.startEdit(block("gen", "go", "gen"), 2);
		await base.indent("gen");

		await base.undo();

		expect(App.UndoGroup).toHaveBeenCalledWith("indent-1");
		expect(base.editingID).toBe("gen");
		expect(base.editCaret).toBe(2);
	});

	it("takes a merge back", async () => {
		vi.mocked(App.MergeBlocks).mockResolvedValueOnce(change("merge-1"));
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("done", "ctx")];
		await base.reloadTopic("go");
		await base.startEdit(block("done", "ctx", "done"), 0);
		await base.mergeIntoPrevious("done");
		expect(App.MergeBlocks).toHaveBeenCalledWith("ctx", "done");

		await base.undo();

		expect(App.UndoGroup).toHaveBeenCalledWith("merge-1");
		expect(base.editingID).toBe("done");
		expect(base.editCaret).toBe(0);
	});

	it("takes an insert-above back", async () => {
		vi.mocked(App.InsertBlockBefore).mockResolvedValueOnce(change("above-1", "above"));
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("done", "ctx")];
		await base.reloadTopic("go");
		await base.startEdit(block("done", "ctx", "done"), 0);
		await base.insertAbove("done");

		await base.undo();

		expect(App.UndoGroup).toHaveBeenCalledWith("above-1");
		expect(base.editingID).toBe("done");
		expect(base.editCaret).toBe(0);
	});

	it("leaves undo of uncommitted typing to the textarea", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("split-1", "tail"));
		await atSeam();
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", " tail"));
		await base.splitAtCaret("done", 4);

		base.editText = "changed tail";

		expect(base.canUndo()).toBe(false);
		await base.undo();
		expect(App.UndoGroup).not.toHaveBeenCalled();
	});

	it("redoes a split by performing it again", async () => {
		vi.mocked(App.SplitBlock)
			.mockResolvedValueOnce(change("split-1", "tail"))
			.mockResolvedValueOnce(change("split-2", "tail-again"));
		await atSeam();
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", " tail"));
		await base.splitAtCaret("done", 4);
		seedCtx(block("done", "ctx", "head tail"));
		await base.undo();
		expect(base.canRedo()).toBe(true);

		seedCtx(block("done", "ctx", "head"), block("tail-again", "ctx", " tail"));
		await base.redo();

		expect(App.SplitBlock).toHaveBeenLastCalledWith("done", "head", " tail");
		expect(base.editingID).toBe("tail-again");
		expect(base.editText).toBe(" tail");
		expect(base.canUndo()).toBe(true);
	});

	it("follows the tail's new id through a second redo", async () => {
		vi.mocked(App.SplitBlock)
			.mockResolvedValueOnce(change("g1", "b"))
			.mockResolvedValueOnce(change("g2", "c"))
			.mockResolvedValueOnce(change("g3", "b-again"))
			.mockResolvedValueOnce(change("g4", "c-again"));
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx", [], "one two three")])])];
		await base.startEdit(block("done", "ctx", "one two three"), 3);

		seedCtx(block("done", "ctx", "one"), block("b", "ctx", " two three"));
		await base.splitAtCaret("done", 3);
		seedCtx(block("done", "ctx", "one"), block("b", "ctx", " two"), block("c", "ctx", " three"));
		await base.splitAtCaret("b", 4);

		seedCtx(block("done", "ctx", "one"), block("b", "ctx", " two three"));
		await base.undo();
		seedCtx(block("done", "ctx", "one two three"));
		await base.undo();
		expect(base.editingID).toBe("done");

		seedCtx(block("done", "ctx", "one"), block("b-again", "ctx", " two three"));
		await base.redo();
		expect(base.editingID).toBe("b-again");

		expect(base.canRedo()).toBe(true);
		seedCtx(block("done", "ctx", "one"), block("b-again", "ctx", " two"), block("c-again", "ctx", " three"));
		await base.redo();

		expect(App.SplitBlock).toHaveBeenLastCalledWith("b-again", " two", " three");
		expect(base.editingID).toBe("c-again");
	});

	it("keeps the command when the backend refuses the rollback", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("split-1", "tail"));
		await atSeam();
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", " tail"));
		await base.splitAtCaret("done", 4);

		vi.mocked(App.UndoGroup).mockRejectedValueOnce(new Error("bridge down"));
		await base.undo();

		expect(base.error).not.toBe("");
		expect(base.canUndo()).toBe(true);
		expect(base.canRedo()).toBe(false);
	});

	it("survives moving to another block", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("split-1", "tail"));
		await atSeam();
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", " tail"));
		await base.splitAtCaret("done", 4);

		await base.startEdit(block("done", "ctx", "head"), 0);

		expect(base.canUndo()).toBe(true);
		seedCtx(block("done", "ctx", "head tail"));
		await base.undo();
		expect(App.UndoGroup).toHaveBeenCalledWith("split-1");
	});

	it("records the cleanup that discards a block left empty", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("split-1", "tail"));
		vi.mocked(App.DeleteBlock).mockResolvedValueOnce(change("discard-1"));
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx", [], "head")])])];
		await base.startEdit(block("done", "ctx", "head"), 4);
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", ""));
		await base.splitAtCaret("done", 4);

		seedCtx(block("done", "ctx", "head"));
		await base.commitEdit();
		expect(App.DeleteBlock).toHaveBeenCalledWith("tail");

		await base.undo();

		expect(App.UndoGroup).toHaveBeenCalledWith("discard-1");
	});

	it("records a gesture whose re-read fails, because the write did land", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("split-1", "tail"));
		await atSeam();

		reloadFixture.onSubtree = () => {
			throw new Error("bridge down");
		};
		await base.splitAtCaret("done", 4);
		reloadFixture.onSubtree = null;

		expect(base.error).not.toBe("");
		expect(base.canUndo()).toBe(true);
	});

	it("moves the command to redo when the re-read after an undo fails", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("split-1", "tail"));
		await atSeam();
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", " tail"));
		await base.splitAtCaret("done", 4);

		reloadFixture.onSubtree = () => {
			throw new Error("bridge down");
		};
		await base.undo();
		reloadFixture.onSubtree = null;

		expect(App.UndoGroup).toHaveBeenCalledWith("split-1");
		expect(base.error).not.toBe("");
		expect(base.canUndo()).toBe(false);
	});

	it("does not repeat a redo whose re-read failed", async () => {
		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("split-1", "tail"));
		await atSeam();
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", " tail"));
		await base.splitAtCaret("done", 4);
		seedCtx(block("done", "ctx", "head tail"));
		await base.undo();

		reloadFixture.onSubtree = () => {
			throw new Error("bridge down");
		};
		await base.redo();
		await base.redo();
		reloadFixture.onSubtree = null;

		expect(App.SplitBlock).toHaveBeenCalledTimes(2);
	});

	it("drops the redo branch when a new gesture is made", async () => {
		vi.mocked(App.SplitBlock)
			.mockResolvedValueOnce(change("split-1", "tail"))
			.mockResolvedValueOnce(change("split-2", "second"));
		await atSeam();
		seedCtx(block("done", "ctx", "head"), block("tail", "ctx", " tail"));
		await base.splitAtCaret("done", 4);
		seedCtx(block("done", "ctx", "head tail"));
		await base.undo();
		expect(base.canRedo()).toBe(true);

		seedCtx(block("done", "ctx", "head"), block("second", "ctx", " tail"));
		await base.splitAtCaret("done", 4);

		expect(base.canRedo()).toBe(false);
	});
});

describe("Tab and Shift+Tab", () => {
	it("indents under the previous sibling", async () => {
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("gen", "go")];
		await base.reloadTopic("go");
		await base.startEdit(block("gen", "go", "gen"), 0);
		await base.indent("gen");
		expect(App.IndentBlock).toHaveBeenCalledWith("gen");
		expect(base.editingID).toBe("gen");
	});

	it("is a silent no-op for a first child", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.indent("ctx");
		expect(App.IndentBlock).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

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
		expect(base.editCaret).toBe(3);
	});

	it("is a silent no-op in the first block of the page, whose predecessor is the title", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.mergeIntoPrevious("ctx");
		expect(App.MergeBlocks).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

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

	it("refuses to merge the page's own title row into anything", async () => {
		await base.startEdit(block("go", "", "go"), 0);
		await base.mergeIntoPrevious("go");
		expect(App.MergeBlocks).not.toHaveBeenCalled();
		expect(base.error).toBe("");
	});

	it("shows the joined text after a merge, not the text from before it", async () => {
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go", "ctxdone")];
		await base.startEdit(block("done", "ctx", "done"), 0);
		await base.mergeIntoPrevious("done");
		expect(base.editText).toBe("ctxdone");
	});
});

describe("placing the caret", () => {
	it("moves the editor only after the re-read, so it never mounts on the old text", async () => {
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go", "ctxdone")];
		const during: { id: string | null; text: string }[] = [];
		reloadFixture.onSubtree = () => {
			during.push({ id: base.editingID, text: base.editText });
		};

		await base.startEdit(block("done", "ctx", "done"), 0);
		await base.mergeIntoPrevious("done");

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

		expect(base.editingID).toBe("done");
		expect(base.editCaret).toBe(0);
		expect(base.editCaretNonce).toBe(opened + 1);
	});

	it("bumps the nonce for a merge and for a split", async () => {
		await base.startEdit(block("done", "ctx", "done"), 0);
		const opened = base.editCaretNonce;
		await base.mergeIntoPrevious("done");
		expect(base.editCaretNonce).toBe(opened + 1);

		vi.mocked(App.SplitBlock).mockResolvedValueOnce(change("grp", "new"));
		await base.splitAtCaret("ctx", 1);
		expect(base.editCaretNonce).toBe(opened + 2);
	});
});

describe("the reader's own caret", () => {
	it("survives a zoom-out instead of snapping back to where the session opened", async () => {
		await base.focus("ctx");
		await base.startEdit(block("done", "ctx", "done"), 0);
		const placed = base.editCaretNonce;

		base.syncCaret("done", 3);
		base.clearFocus();

		expect(base.editCaret).toBe(3);
		expect(base.editCaretNonce).toBe(placed);
	});

	it("ignores a report from an editor whose session has moved on", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 1);
		base.syncCaret("done", 3);
		expect(base.editCaret).toBe(1);
	});
});

describe("Delete at the end", () => {
	it("pulls the next visible block into this one", async () => {
		await base.startEdit(block("ctx", "go", "ctx"), 3);
		await base.pullNextInto("ctx", 3);
		expect(App.MergeBlocks).toHaveBeenCalledWith("ctx", "done");
		expect(base.editingID).toBe("ctx");
		expect(base.editCaret).toBe(3);
	});

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

describe("a ghost is on screen but no gesture reaches it", () => {
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
		reloadFixture.subtrees.go = [block("go", ""), block("ctx", "go"), block("gen", "go")];
	}

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
		expect(base.editingID).toBe("ctx");
	});

	it("moves down past the ghost as well", async () => {
		ghostBetween();
		await base.startEdit(block("ctx", "go", "ctx"), 0);
		await base.moveCaret("ctx", "down", 0);
		expect(base.editingID).toBe("gen");
	});
});

describe("a gesture in flight locks the next one out", () => {
	beforeEach(async () => {
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx")]), node("gen", "go")])];
		base.selectedTopicID = "go";
		await base.startEdit(block("gen", "go", "gen"), 3);
		base.busy = true;
	});

	it("refuses every structural gesture, then allows one once the flight ends", async () => {
		await base.splitAtCaret("gen", 1);
		await base.insertAbove("gen");
		await base.indent("gen");
		await base.outdent("done");
		await base.mergeIntoPrevious("gen");
		await base.pullNextInto("ctx", 3);
		expect(App.SplitBlock).not.toHaveBeenCalled();
		expect(App.InsertBlockBefore).not.toHaveBeenCalled();
		expect(App.IndentBlock).not.toHaveBeenCalled();
		expect(App.OutdentBlock).not.toHaveBeenCalled();
		expect(App.MergeBlocks).not.toHaveBeenCalled();

		base.busy = false;
		await base.indent("gen");
		expect(App.IndentBlock).toHaveBeenCalledWith("gen");
	});

	it("refuses to move the caret out of the block", async () => {
		await base.moveCaret("gen", "up", 0);
		expect(base.editingID).toBe("gen");
		expect(App.EditBlockContent).not.toHaveBeenCalled();
	});

	it("leaves the caret where it is rather than announcing a placement", async () => {
		const nonce = base.editCaretNonce;
		await base.mergeIntoPrevious("gen");
		expect(base.editCaret).toBe(3);
		expect(base.editCaretNonce).toBe(nonce);
	});
});

describe("navigation refuses while a write is in flight", () => {
	beforeEach(async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		base.busy = true;
	});

	it("does not collapse a node", async () => {
		await base.toggle("go");
		expect(base.collapsed.has("go")).toBe(false);
		expect(App.EditBlockContent).not.toHaveBeenCalled();
	});

	it("does not zoom in", async () => {
		await base.focus("ctx");
		expect(base.focusID).toBe(null);
		expect(App.EditBlockContent).not.toHaveBeenCalled();
	});

	it("does not zoom out", () => {
		base.focusID = "ctx";
		base.clearFocus();
		expect(base.focusID).toBe("ctx");
	});

	it("does not switch topic", async () => {
		await base.selectTopic("sqlite");
		expect(base.selectedTopicID).toBe("go");
		expect(App.EditBlockContent).not.toHaveBeenCalled();
	});

	it("does not leave the tree for another mode", async () => {
		shell.mode = "tree";
		await shell.setMode("graph");
		expect(shell.mode).toBe("tree");
		expect(App.EditBlockContent).not.toHaveBeenCalled();
	});

	it("allows them again once the flight ends", async () => {
		base.busy = false;
		await base.toggle("go");
		expect(base.collapsed.has("go")).toBe(true);
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
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

describe("a gesture that fails after its write still leaves one revision", () => {
	beforeEach(() => {
		base.forest = [node("go", "", [node("ctx", "go", [node("done", "ctx")]), node("gen", "go")])];
		base.selectedTopicID = "go";
	});

	it("does not write a second time after the indent behind the write fails", async () => {
		vi.mocked(App.IndentBlock).mockRejectedValueOnce(new Error("invalid ops"));
		await base.startEdit(block("gen", "go", "gen"), 0);
		base.editText = "after";
		await base.indent("gen");
		expect(base.editingID).toBe("gen");
		expect(base.editText).toBe("after");
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
		await base.commitEdit();
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
	});

	it("does not write a second time after the merge behind the write fails", async () => {
		vi.mocked(App.MergeBlocks).mockRejectedValueOnce(new Error("invalid ops"));
		await base.startEdit(block("gen", "go", "gen"), 0);
		base.editText = "after";
		await base.mergeIntoPrevious("gen");
		expect(base.editingID).toBe("gen");
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
		await base.commitEdit();
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
	});

	it("does not write a second time after the arrow's re-read fails", async () => {
		await base.startEdit(block("gen", "go", "gen"), 0);
		base.editText = "after";
		reloadFixture.onSubtree = () => {
			throw new Error("read failed");
		};
		await base.moveCaret("gen", "up", 0);
		reloadFixture.onSubtree = null;
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
		await base.startEdit(block("gen", "go", "after"), 0);
		base.editText = "after";
		await base.commitEdit();
		expect(App.EditBlockContent).toHaveBeenCalledTimes(1);
	});

	it("lands the caret after a merge whose re-read failed", async () => {
		await base.startEdit(block("gen", "go", "gen"), 0);
		reloadFixture.onSubtree = () => {
			throw new Error("read failed");
		};
		await base.mergeIntoPrevious("gen");
		reloadFixture.onSubtree = null;
		expect(App.MergeBlocks).toHaveBeenCalledWith("done", "gen");
		expect(base.editingID).toBe("done");
		expect(base.error).not.toBe("");
	});

	it("moves the session after an arrow whose re-read failed", async () => {
		await base.startEdit(block("gen", "go", "gen"), 0);
		base.editText = "after";
		reloadFixture.onSubtree = () => {
			throw new Error("read failed");
		};
		await base.moveCaret("gen", "up", 0);
		reloadFixture.onSubtree = null;
		expect(base.editingID).toBe("done");
		expect(base.error).not.toBe("");
	});
});

describe("a commit reports its write, not its re-read", () => {
	function readFails(): void {
		reloadFixture.onSubtree = () => {
			throw new Error("read failed");
		};
	}

	it("returns false and keeps the session when the write itself fails", async () => {
		vi.mocked(App.EditBlockContent).mockRejectedValueOnce(new Error("bridge down"));
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		base.editCaret = 3;
		expect(await base.commitEdit()).toBe(false);
		expect(base.editingID).toBe("ctx");
		expect(base.editText).toBe("after");
		expect(base.editCaret).toBe(3);
	});

	it("returns true when the write landed and only the re-read failed", async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		readFails();
		const closed = await base.commitEdit();
		reloadFixture.onSubtree = null;
		expect(closed).toBe(true);
		expect(App.EditBlockContent).toHaveBeenCalledWith("ctx", "after");
		expect(base.editingID).toBe(null);
		expect(base.error).not.toBe("");
	});

	it("returns true when the delete landed and only the re-read failed", async () => {
		await base.startEdit(block("done", "ctx", "something"), 0);
		base.editText = "";
		readFails();
		const closed = await base.commitEdit();
		reloadFixture.onSubtree = null;
		expect(closed).toBe(true);
		expect(App.DeleteBlock).toHaveBeenCalledWith("done");
		expect(base.editingID).toBe(null);
		expect(base.error).not.toBe("");
	});

	it("is visible from outside the store while it runs", async () => {
		let release = (): void => {};
		reloadFixture.writeGate = new Promise<void>((resolve) => {
			release = resolve;
		});
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		expect(base.commitInFlight).toBe(false);
		const closed = base.commitEdit();
		expect(base.commitInFlight).toBe(true);
		release();
		expect(await closed).toBe(true);
		expect(base.commitInFlight).toBe(false);
	});

	it("collapses the node when only the commit's re-read failed", async () => {
		await base.startEdit(block("ctx", "go", "before"), 0);
		base.editText = "after";
		readFails();
		await base.toggle("go");
		reloadFixture.onSubtree = null;
		expect(base.collapsed.has("go")).toBe(true);
		expect(base.editingID).toBe(null);
	});
});
