import { describe, expect, it } from "vitest";
import { submittedSwitch } from "./settingsinput";

describe("submittedSwitch", () => {
	it("submits what the user just set", () => {
		expect(submittedSwitch(true, false, null)).toBe(false);
		expect(submittedSwitch(true, true, false)).toBe(true);
	});

	it("submits nothing when the user never touched it and nothing is stored", () => {
		expect(submittedSwitch(false, true, null)).toBe(null);
	});

	// The regression this function exists for: the environment wins, so the
	// control is locked and can never be dirty. Keying on the winning layer
	// submitted null and erased the stored decision, which came back as AI
	// switching itself on once the variable went away.
	it("resubmits the stored override the user cannot currently edit", () => {
		expect(submittedSwitch(false, true, false)).toBe(false);
		expect(submittedSwitch(false, false, true)).toBe(true);
	});
});
