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
}));

vi.mock("../../bindings/github.com/cominddev/comind/internal/wailsbind", () => ({
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

vi.mock("../../bindings/github.com/cominddev/comind/internal/modules/basebackup", () => ({
	Module: {
		PendingRestore: async () => backend.pending,
		Restore: async () => backend.chosen,
		Backup: async () => "/tmp/base.comind-backup",
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

		expect(container.textContent).toContain("nothing changes until you restart");
		expect(chooseButton(container)).toBeDefined();
	});

	it("shows a staged restore and lets it be dropped", async () => {
		backend.pending = "/tmp/base.comind-backup";
		const { container } = render(SettingsView);

		await waitFor(() => {
			expect(container.textContent).toContain("/tmp/base.comind-backup");
		});
		// A base already waiting to be replaced offers no second restore.
		expect(restoreButton(container)).toBeUndefined();

		const cancel = [...container.querySelectorAll("button")].find((b) => b.textContent?.includes("Cancel the restore"));
		await fireEvent.click(cancel as Element);
		await waitFor(() => {
			expect(backend.cancelled).toBe(true);
		});
		expect(container.textContent).not.toContain("/tmp/base.comind-backup");
	});
});
