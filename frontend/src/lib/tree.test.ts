import { describe, expect, it } from "vitest";
import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { crumbTrail, findNode, type TreeNodeData, topicOf, topicRows } from "./tree";

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

	it("derives the title from the block's content", () => {
		expect(topicRows(forest, null)[0].title).toBe("go");
	});

	// The sidebar is a one-line index, so both levels go through the same
	// stripper the crumbs use. The forest above cannot show this: block() sets
	// Content to the id, which has no markdown in it to strip.
	it("strips markdown from both the topic and its children", () => {
		const child: TreeNodeData = { block: { ...block("kid", "root"), Content: "**token** buckets" }, children: [] };
		const styled: TreeNodeData = {
			block: { ...block("root", ""), Content: "# Rate limiting" },
			children: [child],
		};
		const rows = topicRows([styled], "root");
		expect(rows[0].title).toBe("Rate limiting");
		expect(rows[0].children.map((c) => c.title)).toEqual(["token buckets"]);
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

describe("findNode", () => {
	it("returns the node with its children, not just the block", () => {
		const forest = [node("go", "", [node("ctx", "go", [node("done", "ctx")])])];
		const hit = findNode(forest, "ctx");
		expect(hit?.block.ID).toBe("ctx");
		expect(hit?.children.map((c) => c.block.ID)).toEqual(["done"]);
	});

	it("returns null for an id the forest does not have", () => {
		expect(findNode([node("go", "")], "nope")).toBe(null);
	});
});

describe("crumbTrail", () => {
	it("returns the ancestors root-first, excluding the block itself", () => {
		const forest = [node("go", "", [node("ctx", "go", [node("done", "ctx")])])];
		expect(crumbTrail(forest, "done")).toEqual([
			{ id: "go", title: "go" },
			{ id: "ctx", title: "ctx" },
		]);
	});

	it("returns an empty trail for a topic root, which is not the same as absent", () => {
		expect(crumbTrail([node("go", "")], "go")).toEqual([]);
	});

	it("returns null for an id the forest does not have", () => {
		expect(crumbTrail([node("go", "")], "nope")).toBe(null);
	});

	it("strips markdown from the crumb titles", () => {
		// A crumb is a one-line label, so it goes through the same stripper as
		// the sidebar rather than showing a heading's hashes.
		const deep = node("child", "root");
		const root: TreeNodeData = {
			block: { ...deep.block, ID: "root", ParentID: "", Content: "# Rate limiting" },
			children: [deep],
		};
		expect(crumbTrail([root], "child")).toEqual([{ id: "root", title: "Rate limiting" }]);
	});
});
