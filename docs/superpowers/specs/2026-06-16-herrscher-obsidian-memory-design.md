# Design — Herrscher Memory module (Obsidian plugin)

**Date:** 2026-06-16
**Status:** approved (brainstorm), spec under review
**Repos touched:** `herrscher-contracts` (add the `Memory` port), `herrscher-obsidian-memory` (new plugin, this repo)

## 1. North star

Herrscher is a hyper-modular AI harness: every capability is a plugin behind a
neutral contract (`Gateway`, `Backend`, `Memory`, `Orchestrator`). You compose the
harness with a blank import + rebuild, and swap any module without touching the
rest.

The **Memory module** is the harness's persistent recall. Conceptually it is a
**co-edited knowledge graph**: nodes are notes, edges are links. The human poses the
structure (key files, links) inside Obsidian; the agent graves sessions, decisions
and observations through the Memory port. Obsidian stays the human UI; the markdown
vault — a git-versioned folder — is the source of truth ("gravé dans le marbre").

The key win over flat memory systems: **cross-project links**. A decision in project
A that constrains project B is visible to the agent by following the edge.

## 2. Two layers, two repos

```
herrscher-contracts (existing repo)        ← ADD the Memory port
  ├─ Memory interface : Recall / Record / Search / Links
  ├─ neutral types    : Node, Link, NodeKind, Query, Subgraph
  ├─ MemoryFactory    : func(ctx, PluginConfig) (Memory, error)
  └─ Registry         : Plugin.Memory field + Memories() query
       (CategoryMemory already exists in manifest.go)

herrscher-obsidian-memory (new repo — this one)   ← the plugin
  ├─ implements contracts.Memory over a markdown vault
  ├─ self-registers in init() (xcaddy pattern, like the other plugins)
  ├─ reads/writes frontmatter + body + [[wikilinks]]
  └─ templates for the 6 node kinds
```

The core/host only ever knows the `Memory` port. Obsidian is one implementation
among future ones (SQLite, vector store) — that is the "switch".

**Dependency direction (recorded):** the Orchestrator has a hard dependency on
Memory (the registry must verify a Memory plugin is present when the Orchestrator
category is active). Memory depends on no one. Therefore **Memory ships first** and
unblocks the Orchestrator; not the reverse.

## 3. The `Memory` contract (neutral surface)

Four verbs, with no mention of files or Obsidian:

- **`Recall(ctx, key string, depth int) (Subgraph, error)`** — fetch a node by key
  and follow its links up to `depth`. This is "the agent sees the links".
- **`Record(ctx, Node) error`** — upsert a node by key (no duplicates).
- **`Search(ctx, Query) ([]Node, error)`** — find nodes by keyword/tag/kind without
  knowing the exact key.
- **`Links(ctx, from, to string, rel string) error`** + a query form — create/inspect
  an edge as a first-class operation.

Neutral types (exact Go shapes finalized in the plan):

```go
type NodeKind string // Organization, Project, Repo, Server, Architecture, Production, Session, Decision, User

type Node struct {
    Key   string            // stable identity (vault-relative path in the Obsidian impl)
    Kind  NodeKind
    Title string
    Body  string            // markdown
    Links []Link
    Meta  map[string]string // frontmatter-ish; dates, status, tags
}

type Link struct {
    To  string // target node key
    Rel string // semantic relation: "depends-on", "decided-in", "applies-to", ...
}

type Query struct {
    Text  string
    Kinds []NodeKind
    Tags  []string
    Limit int
}

type Subgraph struct {
    Root  Node
    Nodes []Node // reachable within depth
    Edges []Link
}
```

`Record` is **upsert by `Key`**: re-recording an existing key updates that node
rather than creating a duplicate (mirrors the auto-memory rule "update the file
rather than create a duplicate").

## 4. The Obsidian plugin (vault mapping)

- 1 node = 1 `.md` file. `Key` = vault-relative path/name of the note.
- `Links` = `[[wikilinks]]` in the body; `Meta` = YAML frontmatter (`type:`, `rel:`,
  dates, tags). The plugin parses both on read and emits both on write.
- The vault is a git-versioned folder = "gravé dans le marbre". Obsidian is the
  human UI over the same files.
- `Search` (first slice): frontmatter/tag/kind filter + full-text substring over the
  vault. No vector index yet — that is a different Memory plugin later.

### 4.1 Hierarchy (nestable)

The mapping is **not** flat. The default at creation is `1 repo = 1 project = 1
Obsidian folder`, but it is only a default — creation is free and the structure
nests:

```
Organization   (edge + its servers; or a vendor/domain)   — optional, not every project has one
   └─ Project   (Takt — the unit of work; may aggregate N repos)
        ├─ Repo    (the 8 Takt repos: core + 7 wrappers)
        └─ Server  (the N servers edge manages)
```

A `Project` is the unit of work; it maps to one repo by default but can aggregate
several (Takt = one project, eight repos). `Organization` is an optional tier above
projects for complex real setups (edge managing multiple servers). `Repo` and
`Server` are first-class nodes so the agent can grave decisions/sessions specific to
one repo or one server, linked back to their project.

### 4.2 The 9 node kinds (templates)

| Kind           | Purpose                                                       | Lives at                                   |
|----------------|---------------------------------------------------------------|--------------------------------------------|
| `Organization` | Optional top grouping; links to its projects & topology       | `<org>/index.md`                           |
| `Project`      | Root node per project; state + links to repos/servers & deps  | `[<org>/]<project>/index.md`               |
| `Repo`         | One code repository under a project                           | `[<org>/]<project>/repos/<repo>.md`        |
| `Server`       | One server/host under a project or org                        | `[<org>/]<project>/servers/<server>.md`    |
| `Architecture` | Frozen architecture decisions (living doc, read first)        | `…/<project>/architecture.md`              |
| `Production`   | Deploy/prod state                                             | `…/<project>/production.md`                |
| `Session`      | One work session: date, what was done, decisions, files (a **summary**, not a transcript) | `sessions/YYYY-MM-DD-<slug>.md` |
| `Decision`     | One ADR: context, choice, reason, rejected alternatives; reusable cross-project | `decisions/<slug>.md`     |
| `User`         | Model of the user — identity, work preferences, interaction style; cross-cutting & evolving, **not** dated/factual | `user/<slug>.md` |

`Organization`/`Project`/`Repo`/`Server` form the structural spine.
`Project`/`Session`/`Decision` are factual and dated ("what happened"). `User` is
cross-cutting and evolving ("who you are, applies everywhere"). A `User` preference
is a node **linked** to the projects/decisions it applies to, so a `Recall` of a
project surfaces the user's way of working via the edge.

### 4.3 `init` — the scaffolder (must be rock-solid)

`init` creates the base files for a new org/project/repo/server from the templates
above. Requirements:

- **Idempotent**: re-running `init` on an existing target never overwrites or
  destroys existing content — it only fills in what's missing.
- **Structure-guaranteed**: produces the canonical layout (frontmatter `type:`,
  required links wired, dated where relevant) so every node is well-formed for
  Recall/Search from creation.
- **Free nesting**: target can be a bare project (`projets/<name>/`) or nested under
  an org (`<org>/<project>/`); `init` creates intermediate nodes (the org/project
  `index.md`) if absent and links children to parents.
- Surfaced as a plugin entry point (callable by the host/CLI), not a passive write.

## 5. The nudge (curation seam — defined, NOT implemented)

The **curation loop** is the mechanism that decides *what to record, when, and in
what form*, without being asked: trigger (end of session / periodic) → select what
deserves to survive → synthesize (summary, not transcript) → `Record` with links.

This loop belongs to the **Orchestrator** and is **out of scope to implement here.**
This spec only **defines the seam** — the interface/hook point the Orchestrator will
drive — so that:

- the `Memory` contract exposes only the passive verbs (Recall/Record/Search/Links);
- the curation behaviour (trigger + selection + synthesis) sits above the port;
- when the Orchestrator arrives, it plugs into the documented seam without changing
  the contract or the Obsidian plugin.

No host-side loop, no scheduler, no auto-write in this slice. Just the seam.

### 4.4 Golden example — the reference vault (dogfood: Herrscher)

A reference vault is committed in the repo (e.g. `examples/vault/`) modelling
**Herrscher itself**: org `Herrscherd` → project `Herrscher` → repos `contracts`,
`discord-gateway`, `claude-backend`, `obsidian-memory`, plus an `architecture.md`,
sample `Session` and `Decision` nodes, and a `User` node. It exercises the
Organization → Project → Repo spine on real, always-current architecture.

It serves three roles: (1) living documentation of "what good looks like", (2) the
**test fixture** for Recall/Search/Links round-trips, (3) the canonical output shape
`init` must reproduce. `init` run against an empty dir must converge to this shape.

`Server` is not exercised by the Herrscher vault (repo-centric); it is covered by the
templates and a dedicated unit test so the kind stays well-formed.

## 6. Scope

**In scope (this slice):**
1. `Memory` port + neutral types + `MemoryFactory` + `Plugin.Memory` field +
   `Registry.Memories()` in `herrscher-contracts`.
2. `herrscher-obsidian-memory` plugin: implements Recall/Record/Search/Links over a
   markdown vault; self-registers in `init()`; parses/emits frontmatter + wikilinks;
   templates for all 9 node kinds (incl. `Organization`, `Repo`, `Server`, `User`);
   supports the nestable Organization → Project → Repo/Server hierarchy.
3. The `init` scaffolder: idempotent, structure-guaranteed, free nesting (§4.3).
4. The golden example vault (dogfood: Herrscher) as living doc + test fixture (§4.4).
5. The curation seam: defined as an interface/hook, documented, not implemented.

**Out of scope:**
- The curation loop implementation (Orchestrator owns it).
- Vector/semantic search (a future Memory plugin).
- Any change to `Gateway`/`Backend`/core command routing.
- The Orchestrator itself.

## 7. Testing

- **Contracts:** the port compiles; a recording stub `Memory` satisfies the
  interface; registry `Memories()` returns memory-category plugins only.
- **Plugin:** round-trip tests against a temp vault dir — `Record` then `Recall`
  returns the node with parsed links; `Search` filters by kind/tag/text; `Links`
  creates an edge visible to `Recall` at depth; upsert updates in place (no dup).
- Follow the existing repos' purity/test conventions.
