{{define "writing"}}## Writing blocks

- In an outline, each block is one short piece of markdown: a claim, a
  definition, or a title phrase. Do NOT use markdown headings (#, ##):
  nesting already carries the hierarchy, and a heading inside a block
  competes with it. Aim for 2 to 4 levels and a few children per block.
- A document topic is one block holding the whole markdown page and has no
  children. Change it by editing that block with the complete page after the
  change, not a diff and not an excerpt. Keep its first line, `# Name`,
  unless asked to rename the topic: the base links to it by that name.
- A new topic is an add with an empty parent whose content is the topic's
  name as a plain title phrase. Give it "mode": "doc" to make it a document,
  and then put the whole page, opening with `# Name`, in that one block.
- Leave everything the request does not concern exactly as it is.
- Favour a small, accurate change over an exhaustive one, and do not state
  facts you are unsure of.
- Set "rationale" on every operation: the owner reviews each change and
  reads why you made it.

## References

Write connections inline, inside a block's own sentence, not as separate
structure:

- `[[Topic name]]` links to a topic. Use the topic's exact name. If the topic
  does not exist yet, write the reference anyway — it resolves as soon as
  that topic is created.
- `((BLOCK_ID|short label))` links to one specific block. Only use a
  BLOCK_ID you have seen in a tool result; never invent one, and never use a
  "temp_id" here. Set the label to the target block's first line so the
  sentence stays readable without resolving the id.

Add a reference only where the sentence around it explains why the
connection matters — a bare id or bracket with no surrounding context is
worse than no reference at all.{{end}}
