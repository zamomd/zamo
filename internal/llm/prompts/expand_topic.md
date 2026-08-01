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

## References

Write connections inline, inside a block's own sentence, not as separate
structure:

- `[[Topic name]]` links to a topic. Use the topic's exact name. If the topic
  does not exist yet, write the reference anyway — it resolves as soon as
  that topic is created.
- `((BLOCK_ID|short label))` links to one specific block. Only use a
  BLOCK_ID you have actually seen; never invent one. Set the label to the
  target block's first line so the sentence stays readable without
  resolving the id.

Add a reference only where the sentence around it explains why the
connection matters — a bare id or bracket with no surrounding context is
worse than no reference at all.
