Write a markdown document about **{{.Topic}}** for a personal knowledge base.
{{if .Hint}}
Focus on: {{.Hint}}
{{end}}
Rules:

- Open with a single `# {{.Topic}}` heading. It names the topic and must be
  written exactly as given — the rest of the base links to it by that name.
- Write ordinary markdown below it: paragraphs, `##` sections, lists, fenced
  code where code belongs. No front matter, no HTML.
- Link to another topic by writing `[[Its Name]]` inline in a sentence.
- Be concrete and short. A reader should learn something on every line.

Answer with the document and nothing else: no preamble, no code fence around
the whole answer.
