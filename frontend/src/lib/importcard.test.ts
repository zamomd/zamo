// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import SettingsView from "../SettingsView.svelte"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))

const backend = vi.hoisted(() => ({
	plan: null as null | Record<string, unknown>,
	applied: [] as string[],
	discarded: 0,
	applyError: "",
}))

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
}))

vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/export", () => ({
	Module: {
		Export: async () => "",
		Destination: async () => ({
			dir: "",
			commit: false,
			message: "",
			spec: "@daily",
			scheduled: false,
			running: false,
		}),
	},
}))

vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/basebackup", () => ({
	Module: {
		PendingRestore: async () => "",
		Restore: async () => "",
		Backup: async () => "",
		CancelRestore: async () => {},
	},
}))

vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/vaultimport", () => ({
	Module: {
		Preview: async () =>
			backend.plan ?? {
				token: "",
				dir: "",
				format: "",
				pages: 0,
				blocks: 0,
				links: 0,
				conflicts: null,
				warnings: null,
			},
		Apply: async (token: string) => {
			if (backend.applyError !== "") {
				// The Go side's envelope, as the Wails runtime delivers it.
				throw new Error(JSON.stringify({ cause: { code: backend.applyError, message: "developer text" } }))
			}
			backend.applied.push(token)
			return { groupId: "G", pages: 2, blocks: 5 }
		},
		Discard: async () => {
			backend.discarded++
		},
	},
}))

const twoPages = () => ({
	token: "tok",
	dir: "/vault",
	format: "Obsidian vault",
	pages: 2,
	blocks: 5,
	links: 1,
	conflicts: [{ page: "Inbox", path: "Inbox.md", kind: "exists", detail: "" }],
	warnings: [
		{ page: "Home", kind: "alias", detail: "[[Go|x]]" },
		{ page: "Home", kind: "alias", detail: "[[Go|y]]" },
	],
})

beforeEach(() => {
	backend.plan = null
	backend.applied = []
	backend.discarded = 0
	backend.applyError = ""
})

const button = (c: Element, text: string) =>
	[...c.querySelectorAll("button")].find((b) => b.textContent?.includes(text))

async function preview(plan: Record<string, unknown> | null) {
	backend.plan = plan
	const { container } = render(SettingsView)
	await waitFor(() => {
		expect(button(container, "Import a vault")).toBeDefined()
	})
	await fireEvent.click(button(container, "Import a vault") as Element)
	return container
}

describe("importing a vault", () => {
	it("shows nothing when the directory dialog is dismissed", async () => {
		const container = await preview(null)
		await Promise.resolve()
		expect(container.textContent).not.toContain("Import 0 pages")
		expect(container.textContent).not.toContain("error:")
	})

	it("shows the plan and writes only when the user confirms", async () => {
		const container = await preview(twoPages())
		await waitFor(() => {
			expect(container.textContent).toContain("Obsidian vault at /vault: 2 pages, 5 blocks, 1 link")
		})
		expect(container.textContent).toContain("Inbox")
		expect(container.textContent).toContain("Link aliases dropped")
		expect(container.textContent).toContain("2")
		expect(backend.applied).toEqual([])

		await fireEvent.click(button(container, "Import 2 pages") as Element)

		await waitFor(() => {
			expect(container.textContent).toContain("Imported 2 pages")
		})
		expect(backend.applied).toEqual(["tok"])
		expect(button(container, "Import 2 pages")).toBeUndefined()
	})

	it("cancels through the backend so the preview cannot be applied later", async () => {
		const container = await preview(twoPages())
		await waitFor(() => {
			expect(button(container, "Cancel")).toBeDefined()
		})

		await fireEvent.click(button(container, "Cancel") as Element)

		await waitFor(() => {
			expect(button(container, "Import 2 pages")).toBeUndefined()
		})
		expect(backend.discarded).toBe(1)
		expect(backend.applied).toEqual([])
	})

	it("keeps the plan on the screen when every page is in conflict", async () => {
		const container = await preview({ ...twoPages(), pages: 0, blocks: 0, links: 0 })
		await waitFor(() => {
			expect(container.textContent).toContain("Inbox")
		})
		const apply = button(container, "Import 0 pages") as HTMLButtonElement
		expect(apply.disabled).toBe(true)
	})

	it("keeps the plan and shows the error when the write is refused", async () => {
		backend.applyError = "import_stale_preview"
		const container = await preview(twoPages())
		await waitFor(() => {
			expect(button(container, "Import 2 pages")).toBeDefined()
		})

		await fireEvent.click(button(container, "Import 2 pages") as Element)

		await waitFor(() => {
			expect(container.textContent).toContain("error: The preview is out of date. Run it again.")
		})
		expect(container.textContent).not.toContain("developer text")
		await waitFor(() => {})
		expect(button(container, "Import 2 pages")).toBeDefined()
	})
})
