// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import ExportDestination from "./ExportDestination.svelte"

const backend = vi.hoisted(() => ({
	dest: { dir: "", commit: false, message: "msg", spec: "@daily", scheduled: false, running: false },
	picked: "/vault",
	configured: [] as unknown[],
	refuse: "",
	result: { dir: "/vault", commit: "" },
	forgotten: false,
}))

vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/export", () => ({
	Module: {
		Destination: async () => ({ ...backend.dest }),
		ChooseDestination: async () => {
			backend.dest = { ...backend.dest, dir: backend.picked }
			return { ...backend.dest }
		},
		Configure: async (opts: { commit: boolean; message: string; spec: string; scheduled: boolean }) => {
			backend.configured.push(opts)
			if (backend.refuse !== "") {
				throw new Error("refused", { cause: { code: backend.refuse, message: "refused" } })
			}
			backend.dest = { ...backend.dest, ...opts }
			return { ...backend.dest }
		},
		ExportNow: async () => backend.result,
		ForgetDestination: async () => {
			backend.forgotten = true
			backend.dest = { ...backend.dest, dir: "" }
		},
	},
}))

const button = (c: Element, text: string) =>
	[...c.querySelectorAll("button")].find((b) => b.textContent?.includes(text))

beforeEach(() => {
	backend.dest = { dir: "", commit: false, message: "msg", spec: "@daily", scheduled: false, running: false }
	backend.configured = []
	backend.refuse = ""
	backend.result = { dir: "/vault", commit: "" }
	backend.forgotten = false
})

async function withDestination() {
	backend.dest.dir = "/vault"
	const view = render(ExportDestination)
	await waitFor(() => expect(button(view.container, "Export now")).toBeDefined())
	return view
}

describe("the kept export", () => {
	it("offers only to choose a directory until one is chosen", async () => {
		const { container } = render(ExportDestination)
		await waitFor(() => expect(button(container, "Choose a directory")?.disabled).toBe(false))
		expect(button(container, "Export now")).toBeUndefined()

		await fireEvent.click(button(container, "Choose a directory") as Element)

		await waitFor(() => expect(container.textContent).toContain("/vault"))
		expect(button(container, "Export now")).toBeDefined()
	})

	it("saves a schedule picked from the menu and switches it off again", async () => {
		const { container } = await withDestination()
		const menu = container.querySelector("select") as HTMLSelectElement

		await fireEvent.change(menu, { target: { value: "@hourly" } })
		await waitFor(() => expect(backend.configured).toHaveLength(1))
		expect(backend.configured[0]).toMatchObject({ spec: "@hourly", scheduled: true })

		await fireEvent.change(menu, { target: { value: "" } })
		await waitFor(() => expect(backend.configured).toHaveLength(2))
		expect(backend.configured[1]).toMatchObject({ spec: "@hourly", scheduled: false })
	})

	it("shows the commit message only while committing", async () => {
		const { container } = await withDestination()
		expect(container.querySelector('input[type="text"]')).toBeNull()

		await fireEvent.click(container.querySelector('input[type="checkbox"]') as Element)

		await waitFor(() => expect(container.querySelector('input[type="text"]')).not.toBeNull())
		expect(backend.configured[0]).toMatchObject({ commit: true })
	})

	it("puts the form back when a change is refused", async () => {
		backend.dest.commit = true
		const { container } = await withDestination()
		const field = container.querySelector('input[type="text"]') as HTMLInputElement
		backend.refuse = "export_bad_message"

		await fireEvent.input(field, { target: { value: "{{.Nope" } })
		await fireEvent.change(field)

		await waitFor(() => expect(container.querySelector('[role="alert"]')?.textContent).toContain("template"))
		expect(field.value).toBe("msg")
	})

	it("reports the commit an export made", async () => {
		backend.dest.commit = true
		backend.result = { dir: "/vault", commit: "abcdef1234567890" }
		const { container } = await withDestination()

		await fireEvent.click(button(container, "Export now") as Element)

		await waitFor(() =>
			expect(container.querySelector('[role="status"]')?.textContent).toBe("Exported to /vault and committed abcdef1"),
		)
	})

	it("forgets the destination only after a second click", async () => {
		const { container } = await withDestination()

		await fireEvent.click(button(container, "Forget…") as Element)
		expect(backend.forgotten).toBe(false)
		await fireEvent.click(button(container, "Forget it") as Element)

		await waitFor(() => expect(button(container, "Choose a directory")).toBeDefined())
		expect(backend.forgotten).toBe(true)
	})
})
