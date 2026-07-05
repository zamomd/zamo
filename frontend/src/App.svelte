<script lang="ts">
  // Raw skeleton screen: proves the frontend reaches the Go core through
  // the generated bindings. The real tree view arrives with slice #1.
  import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";
  import type { Block } from "../bindings/github.com/cominddev/comind/internal/knowledge/models.js";

  let status: string = $state("calling TopicMap()…");

  App.TopicMap()
    .then((topics: Block[]) => {
      status = `TopicMap() → ${topics.length} topics`;
    })
    .catch((err: unknown) => {
      status = `TopicMap() → ${String(err)}`;
    });
</script>

<main>
  <h1>Comind</h1>
  <p>Skeleton: frontend ↔ Go core wired.</p>
  <p><code>{status}</code></p>
</main>

<style>
  main {
    font-family: system-ui, sans-serif;
    padding: 2rem;
  }
</style>
