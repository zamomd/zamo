import type { Destination, Result } from "../../bindings/github.com/zamomd/zamo/internal/modules/export"
import { formatWhen, outcomeLabel } from "./schedule"

// The kept export's logic lives here, out of the component, so it is testable
// without a DOM.

export type ScheduleChoice = { spec: string; label: string }

const presets: ScheduleChoice[] = [
	{ spec: "@hourly", label: "Every hour" },
	{ spec: "@daily", label: "Every day" },
	{ spec: "@weekly", label: "Every week" },
]

// scheduleChoices is what the schedule menu offers: the presets, and the
// destination's own expression when it is none of them, so a schedule set
// some other way is shown rather than silently replaced.
export function scheduleChoices(spec: string): ScheduleChoice[] {
	if (spec === "" || presets.some((p) => p.spec === spec)) return presets
	return [...presets, { spec, label: spec }]
}

// repoLine says which git repository the commits go into. It matters most
// when that is a repository above the directory, one the owner keeps for
// something else.
export function repoLine(dest: Destination): string {
	if (dest.repo === "") return "Commits go into the repository this directory is in, or a new one started here."
	if (dest.repo === dest.dir) return "Commits go into the repository in this directory."
	return `Commits go into the repository at ${dest.repo}, which this directory is part of.`
}

// resultLine words how an export the owner just ran went.
export function resultLine(result: Result, commits: boolean): string {
	if (!commits) return `Exported to ${result.dir}`
	if (result.commit === "") return `Exported to ${result.dir} — nothing changed, so no commit`
	return `Exported to ${result.dir} and committed ${result.commit.slice(0, 7)}`
}

// scheduleLine is the line under the schedule menu: when it runs next and how
// the last scheduled run went. Empty when there is nothing to say.
export function scheduleLine(dest: Destination, now: Date): string {
	const parts: string[] = []
	if (dest.running) parts.push("exporting now…")
	else if (dest.scheduled && dest.nextRun) parts.push(`next ${formatWhen(dest.nextRun, now)}`)
	if (dest.lastRun) {
		parts.push(`last scheduled ${formatWhen(dest.lastRun, now)} — ${outcomeLabel(dest.outcome, dest.detail)}`)
	}
	return parts.join(" · ")
}
