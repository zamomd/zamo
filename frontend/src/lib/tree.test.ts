import { describe, expect, it } from "vitest";
import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { type TreeNodeData, topicOf, topicRows } from "./tree";

// block builds the minimum Block the tree helpers read; the rest of the shape
// never reaches them.
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

const forest: TreeNodeData[] = [
	node("go", "", [node("ctx", "go", [node("done", "ctx")])]),
	node("sqlite", "", [node("wal", "sqlite")]),
];

describe("topicOf", () => {
	it("returns a topic root's own id", () => {
		expect(topicOf(forest, "go")).toBe("go");
	});

	it("finds the topic of a nested block", () => {
		expect(topicOf(forest, "done")).toBe("go");
	});

	it("searches every tree, not just the first", () => {
		expect(topicOf(forest, "wal")).toBe("sqlite");
	});

	it("returns null for a block that is not in the forest", () => {
		expect(topicOf(forest, "missing")).toBe(null);
	});

	it("returns null for an empty forest", () => {
		expect(topicOf([], "go")).toBe(null);
	});
});

describe("topicRows", () => {
	it("lists every topic in forest order", () => {
		const rows = topicRows(forest, null);
		expect(rows.map((r) => r.id)).toEqual(["go", "sqlite"]);
	});

	it("uses the block content as the title", () => {
		expect(topicRows(forest, null)[0].title).toBe("go");
	});

	it("expands only the selected topic", () => {
		const rows = topicRows(forest, "go");
		expect(rows[0].selected).toBe(true);
		expect(rows[0].children.map((c) => c.id)).toEqual(["ctx"]);
		expect(rows[1].selected).toBe(false);
		expect(rows[1].children).toEqual([]);
	});

	it("expands one level only", () => {
		const rows = topicRows(forest, "go");
		// "done" is a grandchild and must not appear in the sidebar.
		expect(rows[0].children.map((c) => c.id)).not.toContain("done");
	});

	it("returns an empty list for an empty forest", () => {
		expect(topicRows([], "go")).toEqual([]);
	});
});
