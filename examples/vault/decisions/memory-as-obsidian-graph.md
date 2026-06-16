---
type: decision
date: 2026-06-16
title: Memory as a co-edited Obsidian graph
---
Context: the platform needs persistent recall the Orchestrator can build on.
Choice: model Memory as a markdown knowledge graph (nodes = notes, edges =
[[wikilinks]]), with Obsidian as the human UI, behind a neutral Memory port.
Rejected: a flat KV store (no cross-project links); a vector-only store (opaque,
not human-editable). A future plugin may add semantic search behind the same port.

## Liens
- [[herrscher/herrscher/repos/obsidian-memory|realized-by]]
