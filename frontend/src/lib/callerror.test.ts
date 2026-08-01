import { describe, expect, it } from "vitest";
import { callError, messageFor } from "./callerror";

describe("callError", () => {
	it("reads the payload the newer runtime attaches as a cause", () => {
		const err = new Error("envelope", { cause: { code: "not_found", message: "block 01X: not found" } });
		expect(callError(err)).toEqual({ code: "not_found", message: "block 01X: not found" });
	});

	it("reads the payload the alpha runtime stringifies into the message", () => {
		const err = new Error(JSON.stringify({ message: "call failed", cause: { code: "busy", message: "locked" } }));
		expect(callError(err)).toEqual({ code: "busy", message: "locked" });
	});

	it("downgrades anything unrecognised to internal", () => {
		expect(callError("a string")).toEqual({ code: "internal", message: "a string" });
	});
});

describe("messageFor", () => {
	it("explains a name no reference can reach", () => {
		const err = new Error("x", {
			cause: { code: "unreferenceable_topic", message: "topic name cannot be referenced" },
		});
		expect(messageFor(err)).toMatch(/can't be linked to/);
	});

	it("points at the batch when a rename is reviewed piecemeal", () => {
		const err = new Error("x", {
			cause: { code: "atomic_group", message: "change must be reviewed as one group" },
		});
		expect(messageFor(err)).toMatch(/whole batch/);
	});

	// The Go text is a wrapped, id-bearing developer string and must never reach
	// the user, so an unmapped code falls back rather than passing it through.
	it("never shows the developer text behind an unmapped code", () => {
		const err = new Error("x", { cause: { code: "internal", message: "apply ops group 01X: disk on fire" } });
		expect(messageFor(err)).toBe("Something went wrong. Please try again.");
	});
});
