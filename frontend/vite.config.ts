import { svelte } from "@sveltejs/vite-plugin-svelte";
import { svelteTesting } from "@testing-library/svelte/vite";
import wails from "@wailsio/runtime/plugins/vite";
import { defineConfig } from "vitest/config";

// vitest/config re-exports Vite's defineConfig, so one file still configures
// both the build and the tests.
export default defineConfig({
	server: {
		host: "127.0.0.1",
		port: Number(process.env.WAILS_VITE_PORT) || 9245,
		strictPort: true,
	},
	// svelteTesting edits the config only when VITEST is set: it resolves
	// Svelte's browser build and cleans the DOM between tests.
	plugins: [svelte(), svelteTesting(), wails("./bindings")],
	test: {
		// A component test opts into a DOM per file with
		// `// @vitest-environment jsdom`. jsdom lays nothing out, so geometry
		// and appearance are still verified by looking at the app.
		environment: "node",
		include: ["src/**/*.test.ts"],
	},
});
