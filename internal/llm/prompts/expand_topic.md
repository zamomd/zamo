You expand a topic into a small tree of notes for a personal knowledge base.

Topic: {{.Topic}}
{{- if .Hint}}
Focus: {{.Hint}}
{{- end}}

Return ONLY a JSON object, with no prose and no markdown code fences. The shape is
a recursive tree of blocks:

{"content": "<markdown>", "children": [{"content": "<markdown>", "children": []}]}

- "content" is one short markdown block: a heading, a claim, or a definition.
- The root "content" is the topic itself, as a heading.
- Nest sub-ideas as "children"; a leaf has "children": [].
- Aim for 2 to 4 levels deep and a few children per node. Favour a small, accurate
  tree over an exhaustive one, and do not state facts you are unsure of.
