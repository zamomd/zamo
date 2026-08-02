// Deciding what a settings submission carries is the one piece of the screen
// with a rule rather than a binding, so it lives here where it can be tested.

// submittedSwitch reports what a boolean setting should submit: null means
// "not overridden", leaving the declared value in charge.
//
// It keys on the stored override rather than on the winning layer. An override
// the user cannot currently edit — because the environment wins and the
// control is locked, so it can never be dirty — still has to be resubmitted,
// or saving any other field erases a decision that is merely invisible.
export function submittedSwitch(dirty: boolean, current: boolean, stored: boolean | null): boolean | null {
	return dirty ? current : stored;
}
