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
	// svelteTesting only changes Vitest runs and cleans their DOM.
	plugins: [svelte(), svelteTesting(), wails("./bindings")],
	test: {
		// Component tests opt into jsdom per file; it does not provide layout.
		environment: "node",
		include: ["src/**/*.test.ts"],
	},
});
