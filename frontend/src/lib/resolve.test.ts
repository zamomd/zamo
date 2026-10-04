import { describe, expect, it } from "vitest"
import { Author, Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { aliasIndex, findTopicRoot, makeResolver } from "./resolve"
import type { TreeNodeData } from "./tree"

function forest(): TreeNodeData[] {
	const root: TreeNodeData = {
		block: {
			ID: "go",
			ParentID: "",
			Position: 0,
			Content: "Go Contexts",
			Mode: Mode.ModeOutline,
			CreatedBy: Author.AuthorOwner,
			CreatedAt: "",
			UpdatedAt: "",
			Unreviewed: false,
		},
		children: [],
	}
	return [root]
}

const aliases = aliasIndex([{ TopicID: "go", Alias: "Ctx  Package" }])

describe("makeResolver", () => {
	it("resolves a topic reference to the root's first line", () => {
		expect(makeResolver(forest(), aliases)({ kind: "topic", targetKey: "go contexts" })).toBe("Go Contexts")
	})

	it("returns null for a topic nothing roots", () => {
		expect(makeResolver(forest(), aliases)({ kind: "topic", targetKey: "missing" })).toBeNull()
	})

	it("shows an alias as its owner wrote it", () => {
		expect(makeResolver(forest(), aliases)({ kind: "topic", targetKey: "ctx package" })).toBe("Ctx  Package")
	})
})

describe("findTopicRoot", () => {
	it("reaches a topic through its alias", () => {
		expect(findTopicRoot(forest(), aliases, "ctx package")?.block.ID).toBe("go")
	})

	it("ignores an alias of a topic the forest does not hold", () => {
		expect(findTopicRoot(forest(), aliasIndex([{ TopicID: "gone", Alias: "old" }]), "old")).toBeNull()
	})
})
