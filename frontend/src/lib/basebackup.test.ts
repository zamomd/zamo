// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SettingsView from "../SettingsView.svelte";

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}));

const backend = vi.hoisted(() => ({
	pending: "",
	chosen: "",
	cancelled: false,
	exportDir: "",
}));

vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		Settings: async () => ({
			declarative: false,
			aiEnabled: { value: true, origin: "", override: null },
			provider: { value: "", origin: "", override: null },
			model: { value: "", origin: "", override: null },
			baseUrl: { value: "", origin: "", override: null },
			apiKey: { value: "", origin: "", override: null },
		}),
	},
}));

vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/export", () => ({
	Module: {
		Export: async () => backend.exportDir,
	},
}));

vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/vaultimport", () => ({
	Module: {
		Preview: async () => ({ token: "" }),
		Apply: async () => ({}),
		Discard: async () => {},
	},
}));

vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/basebackup", () => ({
	Module: {
		PendingRestore: async () => backend.pending,
		Restore: async () => backend.chosen,
		Backup: async () => "/tmp/base.zamo-backup",
		CancelRestore: async () => {
			backend.cancelled = true;
		},
	},
}));

beforeEach(() => {
	backend.pending = "";
	backend.chosen = "";
	backend.cancelled = false;
});

const restoreButton = (c: Element) =>
	[...c.querySelectorAll("button")].find((b) => b.textContent?.includes("Restore from a backup"));
const chooseButton = (c: Element) =>
	[...c.querySelectorAll("button")].find((b) => b.textContent?.includes("Choose a backup"));

const exportButton = (c: Element) =>
	[...c.querySelectorAll("button")].find((b) => b.textContent?.includes("Export as Markdown"));

describe("exporting the base as markdown", () => {
	beforeEach(() => {
		backend.exportDir = "";
	});

	it("reports where the export landed", async () => {
		backend.exportDir = "/vault";
		const { container } = render(SettingsView);
		await waitFor(() => {
			expect(exportButton(container)).toBeDefined();
		});

		await fireEvent.click(exportButton(container) as Element);

		await waitFor(() => {
			expect(container.textContent).toContain("Exported to /vault");
		});
	});

	it("says nothing when the directory dialog is dismissed", async () => {
		const { container } = render(SettingsView);
		await waitFor(() => {
			expect(exportButton(container)).toBeDefined();
		});

		await fireEvent.click(exportButton(container) as Element);
		await Promise.resolve();

		expect(container.textContent).not.toContain("Exported to");
		expect(container.textContent).not.toContain("error:");
	});
});

describe("backing up and restoring the base", () => {
	// The webview implements no confirm() (see AGENTS.md), so the warning
	// before a restore has to be a real element on the screen.
	it("warns in the screen before it opens any file dialog", async () => {
		const { container } = render(SettingsView);
		await waitFor(() => {
			expect(restoreButton(container)).toBeDefined();
		});
		expect(chooseButton(container)).toBeUndefined();

		await fireEvent.click(restoreButton(container) as Element);

		expect(container.textContent).toContain("nothing changes until you quit Zamo");
		expect(chooseButton(container)).toBeDefined();
	});

	it("shows a staged restore and lets it be dropped", async () => {
		backend.pending = "/tmp/base.zamo-backup";
		const { container } = render(SettingsView);

		await waitFor(() => {
			expect(container.textContent).toContain("/tmp/base.zamo-backup");
		});
		// A base already waiting to be replaced offers no second restore.
		expect(restoreButton(container)).toBeUndefined();

		const cancel = [...container.querySelectorAll("button")].find((b) => b.textContent?.includes("Cancel the restore"));
		await fireEvent.click(cancel as Element);
		await waitFor(() => {
			expect(backend.cancelled).toBe(true);
		});
		expect(container.textContent).not.toContain("/tmp/base.zamo-backup");
	});
});
