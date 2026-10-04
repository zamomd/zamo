export const SIDEBAR_MIN = 160
export const SIDEBAR_STEP = 16

// Share of the window the sidebar may take, so the main area never disappears.
const MAX_SHARE = 0.6

export function sidebarMax(windowWidth: number): number {
	return Math.max(SIDEBAR_MIN, Math.round(windowWidth * MAX_SHARE))
}

export interface SidebarSize {
	hidden: boolean
	width: number
}

// Maps the width a drag asks for onto what the sidebar does: it never renders
// narrower than SIDEBAR_MIN, and a request under half of that hides it.
export function sidebarSize(desired: number, max: number): SidebarSize {
	return {
		hidden: desired < SIDEBAR_MIN / 2,
		width: Math.min(Math.max(desired, SIDEBAR_MIN), Math.max(SIDEBAR_MIN, max)),
	}
}
