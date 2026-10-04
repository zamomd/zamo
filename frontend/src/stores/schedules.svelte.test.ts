import { beforeEach, describe, expect, it, vi } from "vitest";

const backend = vi.hoisted(() => ({
	entries: [] as unknown[],
	handlers: {} as Record<string, (e: { data: unknown }) => void>,
	reloads: 0,
	refreshes: 0,
}));

vi.mock("@wailsio/runtime", () => ({
	Events: {
		On: (name: string, fn: (e: { data: unknown }) => void) => {
			backend.handlers[name] = fn;
			return () => {};
		},
	},
}));
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { ListSchedules: async () => backend.entries },
}));
vi.mock("./base.svelte", () => ({
	base: {
		reload: async () => {
			backend.reloads++;
		},
	},
}));
vi.mock("./changes.svelte", () => ({
	changes: {
		refresh: async () => {
			backend.refreshes++;
		},
	},
}));

import { schedules } from "./schedules.svelte";

function entry(key: string, overrides: Record<string, unknown> = {}) {
	return { module: "smartcron", available: true, key, spec: "0 9 * * *", enabled: true, running: false, ...overrides };
}

beforeEach(() => {
	backend.entries = [];
	backend.reloads = 0;
	backend.refreshes = 0;
	schedules.entries = [];
});

describe("schedules store", () => {
	it("loads the list and indexes it by block", async () => {
		backend.entries = [entry("a"), entry("b", { outcome: "failed", detail: "provider unavailable" })];

		await schedules.refresh();

		expect(schedules.entries.length).toBe(2);
		expect(schedules.byKey.get("b")?.outcome).toBe("failed");
		expect(schedules.failing).toBe(1);
	});

	it("marks a running block from the start event and reloads the base after an ok run", async () => {
		schedules.start();
		backend.entries = [entry("a")];

		backend.handlers["schedule:run"]({ data: { module: "smartcron", key: "a", running: true } });
		expect(schedules.running.has("a")).toBe(true);

		backend.handlers["schedule:run"]({ data: { module: "smartcron", key: "a", running: false, outcome: "ok" } });
		await vi.waitFor(() => expect(schedules.running.has("a")).toBe(false));
		await vi.waitFor(() => expect(backend.reloads).toBe(1));
		expect(backend.refreshes).toBe(1);
	});

	it("does not reload the base after a run that wrote nothing", async () => {
		schedules.start();
		backend.handlers["schedule:run"]({ data: { module: "smartcron", key: "a", running: false, outcome: "skipped" } });
		await vi.waitFor(() => expect(schedules.entries).toEqual([]));
		expect(backend.reloads).toBe(0);
	});
});
