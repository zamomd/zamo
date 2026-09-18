import { describe, expect, it } from "vitest";
import { type Block, Mode } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import {
	crumbTrail,
	findNode,
	nextVisible,
	prevSibling,
	prevVisible,
	type TreeNodeData,
	topicOf,
	topicRows,
	visibleOrder,
} from "./tree";

function block(id: string, parent: string): Block {
	return {
		ID: id,
		ParentID: parent,
		Position: 0,
		Content: id,
		Mode: Mode.ModeOutline,
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

	it("names a document topic by its first line, not by the whole document", () => {
		const doc: TreeNodeData = {
			block: { ...block("root", ""), Mode: Mode.ModeDoc, Content: "# TCP IP\n\nTCP/IP is the protocol suite." },
			children: [],
		};
		expect(topicRows([doc], null)[0].title).toBe("TCP IP");
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
		const deep = node("child", "root");
		const root: TreeNodeData = {
			block: { ...deep.block, ID: "root", ParentID: "", Content: "# Rate limiting" },
			children: [deep],
		};
		expect(crumbTrail([root], "child")).toEqual([{ id: "root", title: "Rate limiting" }]);
	});
});

const none = new Set<string>();

describe("visibleOrder", () => {
	const forest = [
		node("go", "", [node("ctx", "go", [node("done", "ctx")])]),
		node("sqlite", "", [node("wal", "sqlite")]),
	];

	it("walks the whole forest in document order when nothing is collapsed", () => {
		expect(visibleOrder(forest, new Set(), none)).toEqual(["go", "ctx", "done", "sqlite", "wal"]);
	});

	it("keeps a collapsed node itself but drops everything under it", () => {
		expect(visibleOrder(forest, new Set(["ctx"]), none)).toEqual(["go", "ctx", "sqlite", "wal"]);
	});

	it("drops a whole branch when its top is collapsed", () => {
		expect(visibleOrder(forest, new Set(["go"]), none)).toEqual(["go", "sqlite", "wal"]);
	});

	it("is empty for an empty forest", () => {
		expect(visibleOrder([], new Set(), none)).toEqual([]);
	});
});

describe("prevVisible and nextVisible", () => {
	const forest = [node("go", "", [node("ctx", "go", [node("done", "ctx")]), node("gen", "go")])];

	it("steps into a child, because a child is what is visible next", () => {
		expect(nextVisible(forest, "go", new Set(), none)).toBe("ctx");
	});

	it("steps out of a branch to the parent's next child", () => {
		expect(nextVisible(forest, "done", new Set(), none)).toBe("gen");
	});

	it("returns null at the end of the visible order", () => {
		expect(nextVisible(forest, "gen", new Set(), none)).toBe(null);
	});

	it("returns null at the start of the visible order", () => {
		expect(prevVisible(forest, "go", new Set(), none)).toBe(null);
	});

	it("walks back into the deepest visible descendant, not to the sibling's own row", () => {
		expect(prevVisible(forest, "gen", new Set(), none)).toBe("done");
	});

	it("skips a collapsed block's hidden children", () => {
		expect(prevVisible(forest, "gen", new Set(["ctx"]), none)).toBe("ctx");
	});

	it("returns null for an id the forest does not have", () => {
		expect(prevVisible(forest, "nope", new Set(), none)).toBe(null);
		expect(nextVisible(forest, "nope", new Set(), none)).toBe(null);
	});
});

describe("prevSibling", () => {
	const forest = [node("go", "", [node("ctx", "go", [node("done", "ctx")]), node("gen", "go")])];

	it("returns the sibling directly above, ignoring how deep it is", () => {
		expect(prevSibling(forest, "gen", none)).toBe("ctx");
	});

	it("returns null for a first child, which has nothing to be indented under", () => {
		expect(prevSibling(forest, "ctx", none)).toBe(null);
	});

	it("returns null for a topic root", () => {
		expect(prevSibling(forest, "go", none)).toBe(null);
	});

	it("returns null for an id the forest does not have", () => {
		expect(prevSibling(forest, "nope", none)).toBe(null);
	});
});

describe("the exclusion set", () => {
	const forest = [node("go", "", [node("ctx", "go"), node("gone", "go", [node("kid", "gone")]), node("gen", "go")])];
	const ghosts = new Set(["gone"]);

	it("drops the excluded row but keeps its children, which are on screen and real", () => {
		expect(visibleOrder(forest, new Set(), ghosts)).toEqual(["go", "ctx", "kid", "gen"]);
	});

	it("still hides an excluded row's children when it is collapsed", () => {
		expect(visibleOrder(forest, new Set(["gone"]), ghosts)).toEqual(["go", "ctx", "gen"]);
	});

	it("walks past an excluded row to the one above it on screen", () => {
		expect(prevVisible(forest, "gen", new Set(), ghosts)).toBe("kid");
	});

	it("walks past an excluded row going down", () => {
		expect(nextVisible(forest, "ctx", new Set(), ghosts)).toBe("kid");
	});

	it("never returns an excluded row even when it is the only one between", () => {
		const flat = [node("go", "", [node("gone", "go"), node("gen", "go")])];
		expect(prevVisible(flat, "gen", new Set(), new Set(["gone"]))).toBe("go");
		expect(nextVisible(flat, "go", new Set(), new Set(["gone"]))).toBe("gen");
	});

	it("skips an excluded sibling when looking for the one to indent under", () => {
		expect(prevSibling(forest, "gen", ghosts)).toBe("ctx");
	});

	it("has no sibling to indent under when every sibling above is excluded", () => {
		const flat = [node("go", "", [node("gone", "go"), node("gen", "go")])];
		expect(prevSibling(flat, "gen", new Set(["gone"]))).toBe(null);
	});

	it("still finds an excluded row's children, which have siblings of their own", () => {
		const deep = [node("go", "", [node("gone", "go", [node("a", "gone"), node("b", "gone")])])];
		expect(prevSibling(deep, "b", new Set(["gone"]))).toBe("a");
		expect(prevSibling(deep, "a", new Set(["gone"]))).toBe(null);
	});
});
