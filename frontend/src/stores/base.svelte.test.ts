import { beforeEach, describe, expect, it, vi } from "vitest";

// The store subscribes to a Wails event in its constructor and calls bindings
// on load; neither exists outside the webview, so both are faked here. The
// tests below drive the forest directly and never touch the backend.
vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}));
vi.mock("../../bindings/github.com/cominddev/comind/internal/wailsbind", () => ({
	App: {},
}));
vi.mock("./changes.svelte", () => ({
	changes: { blocks: [], edges: [], count: 0, refresh: async () => {} },
}));

import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
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
