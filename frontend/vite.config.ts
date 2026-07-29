import { svelte } from "@sveltejs/vite-plugin-svelte";
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
	plugins: [svelte(), wails("./bindings")],
	test: {
		// Stores and helpers are plain TypeScript; nothing here needs a DOM.
		// Layout and appearance are verified by looking at the app, not here.
		environment: "node",
		include: ["src/**/*.test.ts"],
	},
});
