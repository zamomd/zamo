// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js";
import { base } from "../stores/base.svelte";
import { schedules } from "../stores/schedules.svelte";
import ScheduledTab from "./ScheduledTab.svelte";

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}));
const backend = vi.hoisted(() => ({
	toggled: [] as unknown[],
}));
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { TopicMap: async () => [], GetSubtree: async () => [], ListSchedules: async () => [] },
}));
vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/smartcron", () => ({
	Module: {
		SetEnabled: async (id: string, enabled: boolean) => {
			backend.toggled.push({ id, enabled });
			return { exists: true, instruction: "", spec: "", enabled, running: false };
		},
	},
}));

function entry(key: string, overrides: Record<string, unknown> = {}) {
	return { module: "smartcron", available: true, key, spec: "0 9 * * *", enabled: true, running: false, ...overrides };
}

beforeEach(() => {
	backend.toggled = [];
	base.openSchedules.clear();
	base.forest = [
		{
			block: {
				ID: "root",
				ParentID: "",
				Position: 0,
				Content: "Networking",
				Meta: null,
				Mode: Mode.ModeOutline,
				CreatedAt: "",
				UpdatedAt: "",
				Unreviewed: false,
			},
			children: [
				{
					block: {
						ID: "b1",
						ParentID: "root",
						Position: 0,
						Content: "# Go releases\nlist",
						Meta: null,
						Mode: Mode.ModeOutline,
						CreatedAt: "",
						UpdatedAt: "",
						Unreviewed: false,
					},
					children: [],
				},
			],
		},
	];
});

describe("ScheduledTab", () => {
	it("says so when nothing is scheduled", () => {
		schedules.entries = [];
		const { getByText } = render(ScheduledTab, { onjump: () => {}, onreview: () => {} });
		expect(getByText("(nothing scheduled)")).toBeTruthy();
	});

	it("renders a row per schedule with its block, outcome and availability", () => {
		schedules.entries = [
			entry("b1", { outcome: "ok", lastRun: "2026-09-21T07:00:00" }),
			entry("gone", { outcome: "skipped", detail: "target missing", enabled: false, lastRun: "2026-09-21T07:00:00" }),
			entry("x", {
				module: "elsewhere",
				available: false,
				outcome: "failed",
				detail: "provider unavailable",
				lastRun: "2026-09-21T07:00:00",
			}),
		];
		const { getByText, getAllByText, getAllByRole } = render(ScheduledTab, { onjump: () => {}, onreview: () => {} });

		expect(getByText("Go releases")).toBeTruthy();
		expect(getAllByText("(deleted block)").length).toBe(2);
		expect(getByText(/^ok/)).toBeTruthy();
		expect(getByText("skipped: target missing")).toBeTruthy();
		expect(getByText("failed: provider unavailable")).toBeTruthy();
		expect(getByText("module unavailable")).toBeTruthy();
		expect(getAllByRole("switch").length).toBe(2);
	});

	it("jumps to the block and opens its panel from the row", async () => {
		schedules.entries = [entry("b1")];
		const jumped: string[] = [];
		const { getByText } = render(ScheduledTab, { onjump: (id: string) => jumped.push(id), onreview: () => {} });

		await fireEvent.click(getByText("Go releases"));

		expect(jumped).toEqual(["b1"]);
		expect(base.openSchedules.has("b1")).toBe(true);
	});

	it("switches the schedule off from the row", async () => {
		schedules.entries = [entry("b1")];
		const { getByRole } = render(ScheduledTab, { onjump: () => {}, onreview: () => {} });

		await fireEvent.click(getByRole("switch"));

		await waitFor(() => expect(backend.toggled).toEqual([{ id: "b1", enabled: false }]));
	});

	it("leads from a skipped run to the review of its topic", async () => {
		schedules.entries = [
			entry("b1", { outcome: "skipped", detail: "3 unreviewed changes in scope", lastRun: "2026-09-21T07:00:00" }),
		];
		const reviewed: string[] = [];
		const { getByTitle } = render(ScheduledTab, {
			onjump: () => {},
			onreview: (topic: string) => reviewed.push(topic),
		});

		await fireEvent.click(getByTitle("review the changes in this topic"));

		expect(reviewed).toEqual(["root"]);
	});
});
