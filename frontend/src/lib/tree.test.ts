import { describe, expect, it } from "vitest";
import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { type TreeNodeData, topicOf } from "./tree";

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
