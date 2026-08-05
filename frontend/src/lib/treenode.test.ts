// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { base } from "../stores/base.svelte";
import TreeNode from "./TreeNode.svelte";

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}));
vi.mock("../../bindings/github.com/cominddev/comind/internal/wailsbind", () => ({
	App: {
		TopicMap: async () => [],
		GetSubtree: async () => [],
		EditBlockContent: async () => {},
	},
}));

function block(overrides: Partial<Block> = {}): Block {
	return {
		ID: "b1",
		ParentID: "root",
		Position: 1,
		Content: "a claim",
		Meta: null,
		CreatedAt: "",
		UpdatedAt: "",
		Unreviewed: false,
		...overrides,
	};
}

describe("TreeNode", () => {
	it("renders the block's markdown", () => {
		const { container } = render(TreeNode, {
			node: { block: block({ Content: "a **bold** claim" }), children: [] },
		});
		const content = container.querySelector(".content");
		expect(content?.textContent?.trim()).toBe("a bold claim");
		expect(content?.querySelector("strong")).not.toBeNull();
	});

	// The row's lanes are what reserve horizontal space beside the text. jsdom
	// lays nothing out, so the width itself is unassertable here; the lane count
	// is the structural fact behind it, and it must not change when a row enters
	// edit mode — a narrower or wider text column rewraps the block under the
	// pointer, which is what lands the caret on the wrong visual line.
	it("keeps the same lanes beside the text in both states", async () => {
		base.clearEdit();
		const { container } = render(TreeNode, {
			node: { block: block({ Content: "a claim long enough to wrap" }), children: [] },
		});
		const rendered = container.querySelectorAll(".gutter").length;
		await fireEvent.click(container.querySelector(".content") as Element);
		await waitFor(() => {
			expect(container.querySelector("textarea")).not.toBeNull();
		});
		expect(container.querySelectorAll(".gutter").length).toBe(rendered);
	});

	it("swaps the rendered row for a textarea on click", async () => {
		base.clearEdit();
		const { container } = render(TreeNode, {
			node: { block: block({ Content: "a claim" }), children: [] },
		});
		const content = container.querySelector(".content");
		expect(content).not.toBeNull();
		await fireEvent.click(content as Element);
		await waitFor(() => {
			expect(container.querySelector("textarea")).not.toBeNull();
		});
		expect(container.querySelector(".content")).toBeNull();
	});
});
