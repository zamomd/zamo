import { describe, expect, it } from "vitest"
import type { View } from "../../bindings/github.com/zamomd/zamo/internal/modules/smartcron/models.js"
import { attemptLabel, formatWhen, formProblem, invalidSpecMessage, runControl } from "./schedule"

const now = new Date("2026-09-21T09:00:00")

function view(overrides: Partial<View> = {}): View {
	return { exists: true, instruction: "refresh", spec: "0 9 * * *", enabled: true, running: false, ...overrides }
}

describe("formatWhen", () => {
	it("names today and tomorrow, and dates further out", () => {
		expect(formatWhen("2026-09-21T15:30:00", now)).toBe("today 15:30")
		expect(formatWhen("2026-09-22T09:00:00", now)).toBe("tomorrow 09:00")
		expect(formatWhen("2026-09-28T09:00:00", now)).toBe("Mon 28 Sep 09:00")
	})

	it("is empty for no time", () => {
		expect(formatWhen(undefined, now)).toBe("")
	})
})

describe("attemptLabel", () => {
	it("is empty before the first attempt", () => {
		expect(attemptLabel(view(), now)).toBe("")
	})

	it("reads outcome and detail with the attempt time", () => {
		expect(attemptLabel(view({ lastRun: "2026-09-21T07:00:00", outcome: "ok" }), now)).toBe("today 07:00 — ok")
		expect(
			attemptLabel(
				view({ lastRun: "2026-09-20T09:00:00", outcome: "skipped", detail: "3 unreviewed changes in scope" }),
				now,
			),
		).toBe("Sun 20 Sep 09:00 — skipped: 3 unreviewed changes in scope")
		expect(attemptLabel(view({ lastRun: "2026-09-21T08:00:00", outcome: "no_change" }), now)).toBe(
			"today 08:00 — no change",
		)
	})
})

describe("formProblem", () => {
	it("wants an instruction and a spec", () => {
		expect(formProblem("  ", "0 9 * * *")).toBe("the instruction is empty")
		expect(formProblem("refresh", "")).toBe("the cron expression is empty")
		expect(formProblem("refresh", "0 9 * * *")).toBe("")
	})
})

describe("runControl", () => {
	it("offers cancel while running, run now when saved, and nothing before saving", () => {
		expect(runControl(view({ running: true }), true)).toBe("cancel")
		expect(runControl(view(), true)).toBe("run")
		expect(runControl(view({ exists: false }), true)).toBe("none")
	})

	it("keeps run now but blocks it while AI is off", () => {
		expect(runControl(view(), false)).toBe("run-blocked")
	})
})

describe("invalidSpecMessage", () => {
	it("passes the parser's message through for an invalid expression and nothing else", () => {
		const invalid = new Error(
			JSON.stringify({ cause: { code: "schedule_invalid_spec", message: 'cron expression "x" is not valid' } }),
		)
		expect(invalidSpecMessage(invalid)).toBe('cron expression "x" is not valid')
		const other = new Error(JSON.stringify({ cause: { code: "not_found", message: "block gone" } }))
		expect(invalidSpecMessage(other)).toBe("")
	})
})
