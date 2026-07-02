# herrscher-obsidian-memory

The Obsidian implementation of the Herrscher `Memory` port: a co-edited markdown
knowledge graph. One node = one `.md` file, `Meta` ↔ frontmatter, `Links` ↔
`[[wikilinks]]`. The vault is a git-versioned folder; Obsidian is the human UI over
it.

## Node kinds

`Organization → Project → Repo/Server` form the structural spine; `Architecture`,
`Production`, `Session`, `Decision` are documentary; `User` models the user
(cross-cutting, evolving); `Agent` anchors a durable companion's private memory.
`Domain` (`dev`, `research`, …) is a transverse area-of-concern root that groups
projects topically above the spine — set `InitSpec.Domain` to attach a project to
one; the slug also lands in the project's `domain` frontmatter for tag search.

## Usage

A blank import wires the plugin into a Herrscher host (xcaddy pattern):

    import _ "github.com/Herrscherd/herrscher-obsidian-memory"

Config: `OBSIDIAN_VAULT` (required) — path to the vault directory.

## Curation

This plugin exposes only the passive verbs (Recall/Record/Search/Links). The
proactive "nudge" loop is the `contracts.CurationHook` seam, owned by the
[orchestrator](https://github.com/Herrscherd/herrscher-orchestrator) — not here.
