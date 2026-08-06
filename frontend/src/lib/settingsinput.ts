// submittedSwitch preserves the stored override until the control changes.
export function submittedSwitch(dirty: boolean, current: boolean, stored: boolean | null): boolean | null {
	return dirty ? current : stored;
}

export function submittedText(dirty: boolean, current: string, stored: string | null): string | null {
	return dirty ? current : stored;
}
