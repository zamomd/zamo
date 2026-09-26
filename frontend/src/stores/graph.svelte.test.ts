import { beforeEach, describe, expect, it, vi } from "vitest";

const graphFixture = vi.hoisted(() => ({
	calls: 0,
	respond: (): Promise<unknown> => Promise.resolve({ Topics: [], Edges: [] }),
}));
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		TopicGraph: () => {
			graphFixture.calls++;
			return graphFixture.respond();
		},
	},
}));

import { graph } from "./graph.svelte";

beforeEach(() => {
	graphFixture.calls = 0;
	graphFixture.respond = () => Promise.resolve({ Topics: [], Edges: [] });
	graph.topics = [];
	graph.edges = [];
	graph.loading = false;
	graph.error = "";
});

describe("load", () => {
	it("reads the topics and edges and lays them out", async () => {
		graphFixture.respond = () =>
			Promise.resolve({
				Topics: [
					{ ID: "a", Title: "Topic A", Blocks: 3 },
					{ ID: "b", Title: "Topic B", Blocks: 1 },
				],
				Edges: [{ From: "a", To: "b" }],
			});

		await graph.load();

		expect(graph.topics.map((t) => t.ID)).toEqual(["a", "b"]);
		expect(graph.edges).toHaveLength(1);
		expect(graph.positions.size).toBe(2);
		expect(graph.error).toBe("");
		expect(graph.loading).toBe(false);
	});

	it("drops the old map when the read fails, rather than showing it beside an error", async () => {
		graphFixture.respond = () => Promise.resolve({ Topics: [{ ID: "a", Title: "A", Blocks: 1 }], Edges: [] });
		await graph.load();
		expect(graph.topics).toHaveLength(1);

		graphFixture.respond = () => Promise.reject(new Error("database is locked"));
		await graph.load();

		expect(graph.error).not.toBe("");
		expect(graph.topics).toHaveLength(0);
		expect(graph.positions.size).toBe(0);
		expect(graph.loading).toBe(false);
	});

	it("treats null slices as an empty base", async () => {
		graphFixture.respond = () => Promise.resolve({ Topics: null, Edges: null });
		await graph.load();
		expect(graph.topics).toHaveLength(0);
		expect(graph.edges).toHaveLength(0);
		expect(graph.positions.size).toBe(0);
		expect(graph.error).toBe("");
	});

	it("does not run two reads at once", async () => {
		let release: () => void = () => {};
		graphFixture.respond = () =>
			new Promise((resolve) => {
				release = () => resolve({ Topics: [], Edges: [] });
			});

		const first = graph.load();
		const second = graph.load();
		expect(graph.loading).toBe(true);
		release();
		await Promise.all([first, second]);

		expect(graphFixture.calls).toBe(1);
	});
});
