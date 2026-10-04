import { describe, expect, it } from "vitest";
import { SIDEBAR_MIN, sidebarMax, sidebarSize } from "./sidebarsize";

describe("sidebarSize", () => {
	it("follows the pointer between the bounds", () => {
		expect(sidebarSize(300, 800)).toEqual({ hidden: false, width: 300 });
	});

	it("holds the minimum width until the pointer is halfway past it", () => {
		expect(sidebarSize(SIDEBAR_MIN / 2, 800)).toEqual({ hidden: false, width: SIDEBAR_MIN });
	});

	it("hides once the pointer is past half the minimum", () => {
		expect(sidebarSize(SIDEBAR_MIN / 2 - 1, 800).hidden).toBe(true);
		expect(sidebarSize(-40, 800).hidden).toBe(true);
	});

	it("stops at the maximum", () => {
		expect(sidebarSize(2000, 800)).toEqual({ hidden: false, width: 800 });
	});

	it("keeps the minimum when the maximum is smaller", () => {
		expect(sidebarSize(300, 100).width).toBe(SIDEBAR_MIN);
	});
});

describe("sidebarMax", () => {
	it("leaves room for the main area", () => {
		expect(sidebarMax(1000)).toBe(600);
	});

	it("never drops under the minimum", () => {
		expect(sidebarMax(100)).toBe(SIDEBAR_MIN);
	});
});
