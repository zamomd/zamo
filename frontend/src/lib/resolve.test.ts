import { describe, expect, it } from "vitest";
import { Mode } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { makeResolver } from "./resolve";
import type { TreeNodeData } from "./tree";

function forest(): TreeNodeData[] {
	const root: TreeNodeData = {
		block: {
			ID: "go",
			ParentID: "",
			Position: 0,
			Content: "Go Contexts",
			Mode: Mode.ModeOutline,
			Meta: {},
			CreatedAt: "",
			UpdatedAt: "",
			Unreviewed: false,
		},
		children: [],
	};
	return [root];
}

describe("makeResolver", () => {
	it("resolves a topic reference to the root's first line", () => {
		expect(makeResolver(forest())({ kind: "topic", targetKey: "go contexts" })).toBe("Go Contexts");
	});

	it("returns null for a topic nothing roots", () => {
		expect(makeResolver(forest())({ kind: "topic", targetKey: "missing" })).toBeNull();
	});
});
