# herrscher-obsidian-memory

**A co-edited markdown knowledge graph.** The Obsidian implementation of the
Herrscher `Memory` port. One node is one `.md` file, `Meta` is the frontmatter,
and `Links` are `[[wikilinks]]`.

The vault is a plain folder you can put under git, and Obsidian is the human UI
over it. It is not a database and it is not a curator: the proactive "nudge" loop
lives in the orchestrator, behind `contracts.CurationHook`.

Category: memory. Plugin kind: `obsidian`, registered via `contracts.Register`
with `CategoryMemory`. Status: live.

Ports: `Memory` (`Recall`, `Record`, `Search`, `Links`, `Unlink`, `Close`), plus
the optional capabilities `Locator`, `Deleter` and `Provisioner`
(`EnsureProject`, `EnsureAgent`).

## Install

```bash
herrscher plugin add github.com/Herrscherd/herrscher-obsidian-memory
```

A blank import wires the plugin into a Herrscher host, the xcaddy pattern:

```go
import _ "github.com/Herrscherd/herrscher-obsidian-memory"
```

## Configuration

| Setting | Environment | Default | What it is |
|---|---|---|---|
| `vault` | `OBSIDIAN_VAULT` | `~/.herrscher/memory` | where the vault lives, resolved at build time so `~` expands |
| `node-budget` | `OBSIDIAN_NODE_BUDGET` | `2000` | the per-node `Body` budget in runes. `0` disables it |

Both are optional.

## What a key may be

A key is a vault-relative path without the `.md`, such as `project/herrscher`. It
may not be absolute, and it may not contain `.`, `..` or an empty segment. That is
what keeps a key inside the vault.

It also may not contain `[`, `]`, `|` or a newline, and neither may a link's
relation. A key is not only a path: it is written into other notes as `[[key]]`
and read back out by a regexp. `a|b` would come back as a link to `a` with the
relation `b`, and `a]]b` as a link to `a`, silently, and only on the next read.

Wikilink syntax has no escape for these characters, so `Record` refuses such a key
or link target rather than storing an edge that means something else.

## Vault provisioning

The plugin factory uses `EnsureVault` rather than `New`. A missing vault directory
and a minimal `.obsidian/` app config are created, so the folder opens as a real
Obsidian vault with no manual setup. Existing `.obsidian/` files are never
overwritten.

The library-level `New` stays open-only and strict.

## Node kinds

`Organization`, `Project` and `Repo`/`Server` form the structural spine.

`Architecture`, `Production`, `Session`, `Decision` and `Transcript` are
documentary. `User` models the user, and `Agent` anchors a durable companion's
private memory.

`Domain` (`dev`, `research`, and so on) is a transverse root that groups projects
topically, above the spine. Set `InitSpec.Domain` when scaffolding with `Init` to
attach a project to one. The slug also lands in the project's `domain`
frontmatter, for tag search.

## Further reading

- [Herrscher docs](https://github.com/Herrscherd/herrscher-docs), page
  `plugins/memory`
- [contracts](https://github.com/Herrscherd/herrscher-contracts), for the port
  signatures
