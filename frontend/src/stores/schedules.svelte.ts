import { Events } from "@wailsio/runtime"
import { SvelteSet } from "svelte/reactivity"
import type { ScheduleEntry } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind"
import { App } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind"
import { base } from "./base.svelte"
import { changes } from "./changes.svelte"

const scheduleRunEvent = "schedule:run"

type RunEvent = { module?: string; key?: string; running?: boolean; outcome?: string }

// The scheduled work of every module, shared by the block glyphs, the panel
// and the Changes screen's scheduled tab. The backend announces each run
// starting and finishing, so the list never polls.
class SchedulesStore {
	entries = $state<ScheduleEntry[]>([])
	running = new SvelteSet<string>()
	byKey = $derived(new Map(this.entries.map((e) => [e.key, e])))
	count = $derived(this.entries.length)
	failing = $derived(this.entries.filter((e) => e.outcome === "failed").length)

	start(): () => void {
		void this.refresh()
		return Events.On(scheduleRunEvent, (e) => {
			const data = (e.data ?? {}) as RunEvent
			void this.onRun(data)
		})
	}

	async refresh(): Promise<void> {
		try {
			this.entries = (await App.ListSchedules()) ?? []
		} catch {
			// The list is decoration over the base; a failed refresh keeps the last one.
		}
	}

	private async onRun(e: RunEvent): Promise<void> {
		if (!e.key) return
		if (e.running) {
			this.running.add(e.key)
			return
		}
		this.running.delete(e.key)
		await this.refresh()
		// Only an ok run wrote a revision the tree and the review queue must
		// show; anything else left the base as it was.
		if (e.outcome === "ok") {
			await Promise.all([base.reload(), changes.refresh()])
		}
	}
}

export const schedules = new SchedulesStore()
