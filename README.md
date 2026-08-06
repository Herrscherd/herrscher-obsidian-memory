# herrscher-obsidian-memory

**A co-edited markdown knowledge graph.** The Obsidian implementation of the
Herrscher `Memory` port: one node = one `.md` file, `Meta` ↔ frontmatter,
`Links` ↔ `[[wikilinks]]`. The vault is a plain git-versionable folder; Obsidian
is the human UI over it. It is not a database and not a curator — the proactive
"nudge" loop lives in the orchestrator behind `contracts.CurationHook`.

## Role · Category · Ports · Config · Status · Repo

| Aspect | Value |
|--------|-------|
| **Role** | Stores and recalls the knowledge graph as markdown files in an Obsidian vault |
| **Category** | Memory |
| **Plugin kind** | `obsidian` (registered via `contracts.Register`, `CategoryMemory`) |
| **Ports implemented** | `Memory` (`Recall`, `Record`, `Search`, `Links`, `Unlink`, `Close`), plus the optional capabilities `Locator`, `Deleter`, `Provisioner` (`EnsureProject`, `EnsureAgent`) |
| **Config & env** | `OBSIDIAN_VAULT` (setting `vault`, optional — default `~/.herrscher/memory`, resolved at build time so `~` expands), `OBSIDIAN_NODE_BUDGET` (setting `node-budget`, optional — per-node `Body` budget in runes, default `2000`, `0` disables) |
| **Contracts** | `github.com/Herrscherd/herrscher-contracts v0.2.13` |
| **Status** | live |
| **Repo** | [herrscher-obsidian-memory](https://github.com/Herrscherd/herrscher-obsidian-memory) |

## Install

```bash
herrscher plugin add github.com/Herrscherd/herrscher-obsidian-memory
```

A blank import wires the plugin into a Herrscher host (xcaddy pattern):

```go
import _ "github.com/Herrscherd/herrscher-obsidian-memory"
```

## What a key may be

A key is a vault-relative path without the `.md` (`project/herrscher`), so it may
not be absolute or contain `.` / `..` / empty segments — that is what keeps a key
inside the vault. It also may not contain `[`, `]`, `|`, or a newline, and
neither may a link's relation: a key is not only a path, it is written into other
notes as `[[key]]` and read back out by a regexp. `a|b` would come back as a link
to `a` with the relation `b`, and `a]]b` as a link to `a` — silently, and only on
the next read. Wikilink syntax has no escape for these, so `Record` refuses such a
key or link target rather than storing an edge that means something else.

## Vault provisioning

The plugin factory uses `EnsureVault`, not `New`: a missing vault directory and a
minimal `.obsidian/` app config are created so the folder opens as a real
Obsidian vault with no manual setup. Existing `.obsidian/` files are never
overwritten. The library-level `New` stays open-only and strict.

## Node kinds

`Organization → Project → Repo/Server` form the structural spine; `Architecture`,
`Production`, `Session`, `Decision`, `Transcript` are documentary; `User` models
the user; `Agent` anchors a durable companion's private memory. `Domain`
(`dev`, `research`, …) is a transverse root that groups projects topically above
the spine — set `InitSpec.Domain` when scaffolding with `Init` to attach a
project to one; the slug also lands in the project's `domain` frontmatter for tag
search.

## Further reading

- [Herrscher docs](https://github.com/Herrscherd/herrscher-docs) — `plugins/memory`
- [contracts](https://github.com/Herrscherd/herrscher-contracts) — port signatures
