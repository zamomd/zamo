import { describe, expect, it } from "vitest"
import type { Destination } from "../../bindings/github.com/zamomd/zamo/internal/modules/export/models"
import { resultLine, scheduleChoices, scheduleLine } from "./exportdestination"

const dest = (over: Partial<Destination> = {}): Destination => ({
	dir: "/vault",
	commit: false,
	message: "",
	spec: "@daily",
	scheduled: false,
	running: false,
	...over,
})

describe("scheduleChoices", () => {
	it("offers the presets for a preset", () => {
		expect(scheduleChoices("@daily").map((c) => c.spec)).toEqual(["@hourly", "@daily", "@weekly"])
	})

	it("adds an expression that is none of them", () => {
		expect(scheduleChoices("0 9 * * 1").at(-1)).toEqual({ spec: "0 9 * * 1", label: "0 9 * * 1" })
	})
})

describe("resultLine", () => {
	it("names the directory when git is off", () => {
		expect(resultLine({ dir: "/vault", commit: "" }, false)).toBe("Exported to /vault")
	})

	it("names the commit it made", () => {
		expect(resultLine({ dir: "/vault", commit: "abcdef1234567890" }, true)).toBe(
			"Exported to /vault and committed abcdef1",
		)
	})

	it("says so when nothing changed", () => {
		expect(resultLine({ dir: "/vault", commit: "" }, true)).toContain("no commit")
	})
})

describe("scheduleLine", () => {
	const now = new Date(2026, 9, 5, 8, 0)

	it("is empty for an unscheduled destination that never ran", () => {
		expect(scheduleLine(dest(), now)).toBe("")
	})

	it("says when it runs next and how the last run went", () => {
		const line = scheduleLine(
			dest({
				scheduled: true,
				nextRun: new Date(2026, 9, 5, 9, 0).toISOString(),
				lastRun: new Date(2026, 9, 5, 7, 0).toISOString(),
				outcome: "ok",
				detail: "committed abcdef1",
			}),
			now,
		)
		expect(line).toBe("next today 09:00 · last scheduled today 07:00 — ok: committed abcdef1")
	})

	it("keeps the last run of a schedule since switched off", () => {
		const line = scheduleLine(
			dest({ lastRun: new Date(2026, 9, 5, 7, 0).toISOString(), outcome: "failed", detail: "disk full" }),
			now,
		)
		expect(line).toBe("last scheduled today 07:00 — failed: disk full")
	})
})
