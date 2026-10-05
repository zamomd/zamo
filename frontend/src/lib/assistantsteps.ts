// One step of an assistant run, as the backend announces it (see
// internal/wailsbind AgentStep).
export type Step = { kind: string; tool?: string; detail?: string }

// stepLabel is the line the panel shows for a step. nameOf turns a block id
// into something a person can read, or null when the base has no such block:
// an id is never shown.
export function stepLabel(step: Step, nameOf: (id: string) => string | null): string {
	if (step.kind === "thinking") return "thinking…"
	if (step.kind === "wrote") return "change written"
	if (step.kind !== "tool") return "working…"
	const name = step.detail ? nameOf(step.detail) : null
	switch (step.tool) {
		case "search":
			return step.detail ? `searching “${step.detail}”…` : "searching…"
		case "get_subtree":
			return name ? `reading “${name}”…` : "reading a topic…"
		case "get_backlinks":
			return name ? `following references to “${name}”…` : "following references…"
		case "topic_map":
			return "looking over the topics…"
		case "write_blocks":
			return step.detail ? `writing: ${step.detail}…` : "writing…"
		default:
			return "working…"
	}
}
