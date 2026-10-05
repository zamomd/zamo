import type { View } from "../../bindings/github.com/zamomd/zamo/internal/modules/smartcron"
import { callError } from "./callerror"

// The panel's logic lives here, out of the component, so it is testable
// without a DOM: when to show which control, and how to word a time and an
// attempt.

const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

function sameDay(a: Date, b: Date): boolean {
	return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

function clock(d: Date): string {
	return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`
}

// formatWhen words an instant relative to now: "today 15:30", "tomorrow
// 09:00", otherwise "Mon 28 Sep 09:00". Empty for no instant.
export function formatWhen(iso: string | undefined, now: Date): string {
	if (!iso) return ""
	const d = new Date(iso)
	if (Number.isNaN(d.getTime())) return ""
	const tomorrow = new Date(now)
	tomorrow.setDate(now.getDate() + 1)
	if (sameDay(d, now)) return `today ${clock(d)}`
	if (sameDay(d, tomorrow)) return `tomorrow ${clock(d)}`
	return `${days[d.getDay()]} ${d.getDate()} ${months[d.getMonth()]} ${clock(d)}`
}

const outcomeWords: Record<string, string> = {
	ok: "ok",
	no_change: "no change",
	skipped: "skipped",
	failed: "failed",
}

// outcomeLabel is "skipped: 3 unreviewed changes in scope" or just "ok".
export function outcomeLabel(outcome: string | undefined, detail: string | undefined): string {
	if (!outcome) return ""
	const word = outcomeWords[outcome] ?? outcome
	return detail ? `${word}: ${detail}` : word
}

// attemptLabel is the "last attempt" line, empty before the first attempt.
export function attemptLabel(view: View, now: Date): string {
	if (!view.lastRun) return ""
	return `${formatWhen(view.lastRun, now)} — ${outcomeLabel(view.outcome, view.detail)}`
}

// formProblem is what stops the form from being saved, or "".
export function formProblem(instruction: string, spec: string): string {
	if (instruction.trim() === "") return "the instruction is empty"
	if (spec.trim() === "") return "the cron expression is empty"
	return ""
}

export type RunControl = "none" | "run" | "run-blocked" | "cancel"

// runControl picks the run button's state: nothing before the schedule is
// saved, cancel while it runs, and run now otherwise — blocked, not hidden,
// while AI is off, so the owner sees why it does nothing.
export function runControl(view: View, aiEnabled: boolean): RunControl {
	if (!view.exists) return "none"
	if (view.running) return "cancel"
	return aiEnabled ? "run" : "run-blocked"
}

// invalidSpecMessage is the parser's own message for an invalid expression —
// the one binding error whose text is written for the owner — and "" for any
// other error.
export function invalidSpecMessage(err: unknown): string {
	const e = callError(err)
	return e.code === "schedule_invalid_spec" ? e.message : ""
}
