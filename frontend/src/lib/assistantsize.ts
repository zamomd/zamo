export const ASSISTANT_MIN = 280
export const ASSISTANT_STEP = 16

// Share of the window the panel may take, so what it talks about stays in view.
const MAX_SHARE = 0.5

export function assistantMax(windowWidth: number): number {
	return Math.max(ASSISTANT_MIN, Math.round(windowWidth * MAX_SHARE))
}

// Maps the width a drag asks for onto what the panel takes.
export function assistantWidth(desired: number, windowWidth: number): number {
	return Math.min(Math.max(desired, ASSISTANT_MIN), assistantMax(windowWidth))
}
