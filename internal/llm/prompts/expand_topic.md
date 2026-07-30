You expand a topic into a small tree of notes for a personal knowledge base.

Topic: {{.Topic}}
{{- if .Hint}}
Focus: {{.Hint}}
{{- end}}

Return ONLY a JSON object, with no prose and no markdown code fences. The shape is
a recursive tree of blocks:

{"content": "<markdown>", "children": [{"content": "<markdown>", "children": []}]}

- "content" is one short markdown block: a claim, a definition, or a title
  phrase. Do NOT use markdown headings (#, ##): nesting already carries the
  hierarchy, and a heading inside a block competes with it.
- The root "content" is the topic itself, as a plain title phrase with no
  leading #.
- Nest sub-ideas as "children"; a leaf has "children": [].
- Aim for 2 to 4 levels deep and a few children per node. Favour a small, accurate
  tree over an exhaustive one, and do not state facts you are unsure of.
