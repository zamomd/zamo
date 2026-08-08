Here is a markdown document from a personal knowledge base:

---
{{.Document}}
---

Apply this instruction to it: {{.Instruction}}

Rules:

- Answer with the complete document after the change, not a diff and not an
  excerpt.
- Leave every part the instruction does not concern exactly as it is.
- Keep the first line unless the instruction asks to rename the topic: the base
  links to this document by that name.
- Link to another topic by writing `[[Its Name]]` inline.

Answer with the document and nothing else: no preamble, no code fence around
the whole answer.
