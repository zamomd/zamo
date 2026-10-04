// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Block } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js"
import { Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js"
import { ai } from "../stores/ai.svelte"
import SchedulePanel from "./SchedulePanel.svelte"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
const backend = vi.hoisted(() => ({
	view: { exists: false, instruction: "", spec: "", enabled: false, running: false } as Record<string, unknown>,
	saved: [] as unknown[],
	removed: [] as string[],
	ran: [] as string[],
	previewError: "",
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { TopicMap: async () => [], GetSubtree: async () => [], ListSchedules: async () => [] },
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/smartcron", () => ({
	Module: {
		Get: async () => backend.view,
		Save: async (id: string, instruction: string, spec: string, enabled: boolean) => {
			backend.saved.push({ id, instruction, spec, enabled })
			backend.view = { exists: true, instruction, spec, enabled, running: false }
			return backend.view
		},
		Remove: async (id: string) => {
			backend.removed.push(id)
		},
		RunNow: async (id: string) => {
			backend.ran.push(id)
		},
		Cancel: async () => {},
		Preview: async () => {
			if (backend.previewError !== "") {
				throw new Error(JSON.stringify({ cause: { code: "schedule_invalid_spec", message: backend.previewError } }))
			}
			return { next: ["2026-09-22T09:00:00", "2026-09-23T09:00:00", "2026-09-24T09:00:00"], zone: "CEST (UTC+02:00)" }
		},
	},
}))

const block: Block = {
	ID: "b1",
	ParentID: "root",
	Position: 1,
	Content: "Go releases",
	Meta: null,
	Mode: Mode.ModeOutline,
	CreatedAt: "",
	UpdatedAt: "",
	Unreviewed: false,
}

beforeEach(() => {
	backend.view = { exists: false, instruction: "", spec: "", enabled: false, running: false }
	backend.saved = []
	backend.removed = []
	backend.ran = []
	backend.previewError = ""
	ai.enabled = true
})

describe("SchedulePanel", () => {
	it("opens empty for a block without a schedule and saves a new one", async () => {
		const { getByLabelText, getByText, queryByText } = render(SchedulePanel, { block })
		await waitFor(() => expect(getByLabelText("instruction")).toBeTruthy())
		expect(queryByText("run now")).toBeNull()

		await fireEvent.input(getByLabelText("instruction"), { target: { value: "refresh the list" } })
		await fireEvent.input(getByLabelText("cron"), { target: { value: "0 9 * * *" } })
		await waitFor(() => expect(getByText(/next: .*09:00/)).toBeTruthy())
		await fireEvent.click(getByText("save"))

		await waitFor(() =>
			expect(backend.saved).toEqual([{ id: "b1", instruction: "refresh the list", spec: "0 9 * * *", enabled: true }]),
		)
		await waitFor(() => expect(getByText("run now")).toBeTruthy())
	})

	it("shows the parser's error and blocks save on an invalid expression", async () => {
		backend.previewError = 'cron expression "every monday" is not valid'
		const { getByLabelText, getByText } = render(SchedulePanel, { block })
		await waitFor(() => expect(getByLabelText("cron")).toBeTruthy())

		await fireEvent.input(getByLabelText("instruction"), { target: { value: "refresh" } })
		await fireEvent.input(getByLabelText("cron"), { target: { value: "every monday" } })

		await waitFor(() => expect(getByText('cron expression "every monday" is not valid')).toBeTruthy())
		expect((getByText("save") as HTMLButtonElement).disabled).toBe(true)
	})

	it("confirms removal inside the panel", async () => {
		backend.view = { exists: true, instruction: "refresh", spec: "0 9 * * *", enabled: true, running: false }
		const { getByText, queryByText } = render(SchedulePanel, { block })
		await waitFor(() => expect(getByText("remove")).toBeTruthy())

		await fireEvent.click(getByText("remove"))
		expect(backend.removed).toEqual([])
		await fireEvent.click(getByText("no"))
		expect(queryByText("yes")).toBeNull()
		await fireEvent.click(getByText("remove"))
		await fireEvent.click(getByText("yes"))

		await waitFor(() => expect(backend.removed).toEqual(["b1"]))
	})

	it("keeps the form usable but blocks run now while AI is off", async () => {
		backend.view = { exists: true, instruction: "refresh", spec: "0 9 * * *", enabled: true, running: false }
		ai.enabled = false
		const { getByText, getByLabelText } = render(SchedulePanel, { block })
		await waitFor(() => expect(getByText("run now")).toBeTruthy())

		expect((getByText("run now") as HTMLButtonElement).disabled).toBe(true)
		expect((getByLabelText("instruction") as HTMLTextAreaElement).disabled).toBe(false)
	})

	it("always says that schedules run only while Zamo is open", async () => {
		const { getByText } = render(SchedulePanel, { block })
		await waitFor(() => expect(getByText("runs only while Zamo is open")).toBeTruthy())
	})
})
