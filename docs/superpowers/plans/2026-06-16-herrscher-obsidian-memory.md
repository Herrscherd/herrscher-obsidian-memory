# Herrscher Obsidian Memory — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the neutral `Memory` port in `herrscher-contracts` and a first implementation, `herrscher-obsidian-memory`, that stores a co-edited markdown knowledge graph (Organization → Project → Repo/Server, plus Architecture/Production/Session/Decision/User) and supports Recall/Record/Search/Links + an idempotent `init` scaffolder.

**Architecture:** Two layers. (1) `herrscher-contracts` gains the `Memory` interface, neutral types (`Node`, `Link`, `NodeKind`, `Query`, `Subgraph`), a `MemoryFactory`, the `Plugin.Memory` field, the `Registry.Memories()` query, and a documented (unimplemented) `CurationHook` seam. (2) `herrscher-obsidian-memory` implements `Memory` over a vault directory: one node = one `.md` file, `Meta` ↔ flat YAML frontmatter, `Links` ↔ `[[wikilinks]]` in the body. The curation loop is NOT implemented — only its seam is declared, for the future Orchestrator.

**Tech Stack:** Go 1.23, standard library only (no external deps — matches the house style of `herrscher-contracts`). Tests use the stdlib `testing` package and `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-06-16-herrscher-obsidian-memory-design.md`

---

## File Structure

**Repo `herrscher-contracts` (existing — `/home/shan/dev/herrscher-contracts`):**
- Create `memory.go` — `Memory` interface, `NodeKind` + constants, `Node`, `Link`, `Query`, `Subgraph`, `CurationHook` seam.
- Create `memory_test.go` — a recording stub satisfies `Memory`; `CurationHook` compiles.
- Modify `registry.go` — add `MemoryFactory`, `Plugin.Memory`, `Registry.Memories()`.
- Modify `registry_test.go` — assert `Memories()` isolates the memory-category plugin.

**Repo `herrscher-obsidian-memory` (new — `/home/shan/dev/herrscher-obsidian-memory`):**
- Modify `go.mod` — add the `herrscher-contracts` require + replace directive.
- Create `paths.go` — key↔file path mapping, kind→layout helpers.
- Create `vault.go` — marshal/unmarshal a `contracts.Node` ↔ `.md` bytes (frontmatter + wikilinks).
- Create `memory.go` — `ObsidianMemory` struct: `New`, `Record`, `Recall`, `Search`, `Links`, `Close`.
- Create `scaffold.go` — `Init` (idempotent base-file creator).
- Create `register.go` — `init()` self-registration + `MemoryFactory` wiring.
- Create `paths_test.go`, `vault_test.go`, `memory_test.go`, `scaffold_test.go`.
- Create `examples/vault/…` — the golden Herrscher reference vault (+ `golden_test.go`).

---

## Phase A — the `Memory` port (`herrscher-contracts`)

### Task 1: Memory interface, neutral types, and the curation seam

**Files:**
- Create: `/home/shan/dev/herrscher-contracts/memory.go`
- Test: `/home/shan/dev/herrscher-contracts/memory_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-contracts/memory_test.go`:

```go
package contracts

import (
	"context"
	"testing"
)

// recMemory is a recording stub proving the Memory interface is implementable.
type recMemory struct {
	recorded []Node
	closed   bool
}

func (m *recMemory) Recall(_ context.Context, key string, _ int) (Subgraph, error) {
	return Subgraph{Root: Node{Key: key}}, nil
}
func (m *recMemory) Record(_ context.Context, n Node) error { m.recorded = append(m.recorded, n); return nil }
func (m *recMemory) Search(_ context.Context, _ Query) ([]Node, error) { return nil, nil }
func (m *recMemory) Links(_ context.Context, _, _, _ string) error     { return nil }
func (m *recMemory) Close() error                                      { m.closed = true; return nil }

func TestMemoryInterfaceIsImplementable(t *testing.T) {
	var mem Memory = &recMemory{}
	if err := mem.Record(context.Background(), Node{Key: "k", Kind: KindProject}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	sg, err := mem.Recall(context.Background(), "k", 1)
	if err != nil || sg.Root.Key != "k" {
		t.Fatalf("Recall returned %+v, %v", sg, err)
	}
	if err := mem.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNodeKindConstants(t *testing.T) {
	kinds := []NodeKind{
		KindOrganization, KindProject, KindRepo, KindServer,
		KindArchitecture, KindProduction, KindSession, KindDecision, KindUser,
	}
	if len(kinds) != 9 {
		t.Fatalf("expected 9 node kinds, got %d", len(kinds))
	}
}

// curStub proves the CurationHook seam is implementable (no production impl ships here).
type curStub struct{ called bool }

func (c *curStub) Consolidate(_ context.Context) error { c.called = true; return nil }

func TestCurationHookIsImplementable(t *testing.T) {
	var h CurationHook = &curStub{}
	if err := h.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run 'Memory|NodeKind|CurationHook' -v`
Expected: FAIL — build error, `Memory`/`Node`/`KindProject`/… undefined.

- [ ] **Step 3: Write minimal implementation**

Create `/home/shan/dev/herrscher-contracts/memory.go`:

```go
package contracts

import "context"

// NodeKind classifies a memory node. The structural spine
// (Organization → Project → Repo/Server) plus the documentary kinds.
type NodeKind string

const (
	KindOrganization NodeKind = "organization"
	KindProject      NodeKind = "project"
	KindRepo         NodeKind = "repo"
	KindServer       NodeKind = "server"
	KindArchitecture NodeKind = "architecture"
	KindProduction   NodeKind = "production"
	KindSession      NodeKind = "session"
	KindDecision     NodeKind = "decision"
	KindUser         NodeKind = "user"
)

// Link is a directed, typed edge to another node, identified by its Key.
type Link struct {
	To  string // target node Key
	Rel string // semantic relation: "depends-on", "decided-in", "applies-to", "contains", …
}

// Node is one unit of memory: a stable Key, a Kind, human Title/Body (markdown),
// outbound Links, and flat Meta (dates, status, tags). It is storage-neutral —
// the Obsidian plugin maps Meta to frontmatter and Links to [[wikilinks]], but
// the contract says nothing about files.
type Node struct {
	Key   string
	Kind  NodeKind
	Title string
	Body  string
	Links []Link
	Meta  map[string]string
}

// Query selects nodes without knowing their Key. An empty Query matches nothing
// useful; callers set at least Text or Kinds.
type Query struct {
	Text  string
	Kinds []NodeKind
	Tags  []string
	Limit int // 0 = no limit
}

// Subgraph is a Recall result: the root node plus every node reachable within the
// requested depth and the edges connecting them.
type Subgraph struct {
	Root  Node
	Nodes []Node
	Edges []Link
}

// Memory is the persistent-recall port. Implementations store a knowledge graph
// (the Obsidian plugin uses a markdown vault). The host/orchestrator drives only
// these passive verbs; the curation behaviour lives above the port (see
// CurationHook).
type Memory interface {
	// Recall fetches the node at key and follows its links up to depth (0 = root
	// only), returning the reachable subgraph.
	Recall(ctx context.Context, key string, depth int) (Subgraph, error)
	// Record upserts a node by Key — re-recording an existing Key updates it in
	// place rather than creating a duplicate.
	Record(ctx context.Context, n Node) error
	// Search finds nodes matching the query (keyword/kind/tag).
	Search(ctx context.Context, q Query) ([]Node, error)
	// Links creates a typed edge from one node to another.
	Links(ctx context.Context, from, to, rel string) error
	// Close releases any resources the implementation holds.
	Close() error
}

// CurationHook is the SEAM for proactive curation — the "nudge" that decides what
// to record, when, and in what form (select → summarize → Record). It is declared
// here but DELIBERATELY NOT IMPLEMENTED: the future Orchestrator implements it and
// drives Memory.Record. Keeping it out of the Memory interface preserves the port
// as passive verbs only.
type CurationHook interface {
	Consolidate(ctx context.Context) error
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run 'Memory|NodeKind|CurationHook' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add memory.go memory_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(memory): neutral Memory port, node types, and curation seam"
```

---

### Task 2: Wire the Memory factory into the registry

**Files:**
- Modify: `/home/shan/dev/herrscher-contracts/registry.go`
- Test: `/home/shan/dev/herrscher-contracts/registry_test.go`

- [ ] **Step 1: Write the failing test**

Append to `/home/shan/dev/herrscher-contracts/registry_test.go`:

```go
func TestRegistryIsolatesMemory(t *testing.T) {
	var r Registry
	r.Register(Plugin{
		Manifest: Manifest{Kind: "obsidian", Category: CategoryMemory},
		Memory:   func(context.Context, PluginConfig) (Memory, error) { return nil, nil },
	})
	r.Register(Plugin{
		Manifest: Manifest{Kind: "claude", Category: CategoryBackend},
		Backend:  func(context.Context, PluginConfig) (Backend, error) { return nil, nil },
	})

	got := r.Memories()
	if len(got) != 1 || got[0].Manifest.Kind != "obsidian" {
		t.Fatalf("Memories() did not isolate the memory plugin: %+v", got)
	}
	if len(r.Backends()) != 1 {
		t.Fatalf("Backends() should still see exactly one backend")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestRegistryIsolatesMemory -v`
Expected: FAIL — `Plugin` has no field `Memory`; `r.Memories` undefined.

- [ ] **Step 3: Write minimal implementation**

In `/home/shan/dev/herrscher-contracts/registry.go`, add `MemoryFactory` to the factory type block:

```go
type (
	GatewayFactory func(ctx context.Context, cfg PluginConfig) (GatewaySet, error)
	BackendFactory func(ctx context.Context, cfg PluginConfig) (Backend, error)
	MemoryFactory  func(ctx context.Context, cfg PluginConfig) (Memory, error)
)
```

Add the `Memory` field to `Plugin` (after `Backend`):

```go
type Plugin struct {
	Manifest Manifest
	Gateway  GatewayFactory // set iff Manifest.Category == CategoryGateway
	Backend  BackendFactory // set iff Manifest.Category == CategoryBackend
	Memory   MemoryFactory  // set iff Manifest.Category == CategoryMemory
}
```

Add the query (next to `Gateways()`/`Backends()`):

```go
func (r *Registry) Memories() []Plugin { return r.byCategory(CategoryMemory) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS (all tests, including the new one).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add registry.go registry_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat(memory): MemoryFactory, Plugin.Memory, Registry.Memories()"
```

---

## Phase B — the Obsidian plugin (`herrscher-obsidian-memory`)

### Task 3: Wire the module dependency on contracts

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/go.mod`

- [ ] **Step 1: Add the require + replace directive**

Edit `/home/shan/dev/herrscher-obsidian-memory/go.mod` so it reads exactly:

```
module github.com/Herrscherd/herrscher-obsidian-memory

go 1.23

require github.com/Herrscherd/herrscher-contracts v0.0.0

replace github.com/Herrscherd/herrscher-contracts => ../herrscher-contracts
```

- [ ] **Step 2: Verify it resolves**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go mod tidy && go build ./... 2>&1 || echo "no packages yet — OK"`
Expected: no error about the replace target (the contracts module resolves). "no packages yet" is fine since there is no `.go` source.

- [ ] **Step 3: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add go.mod go.sum 2>/dev/null; git add go.mod
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "build: depend on herrscher-contracts via replace directive"
```

---

### Task 4: Key ↔ file path mapping

**Files:**
- Create: `/home/shan/dev/herrscher-obsidian-memory/paths.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/paths_test.go`

A node `Key` is its vault-relative path **without** the `.md` extension (e.g.
`herrscher/repos/contracts`). The file lives at `<root>/<key>.md`. Keys are always
forward-slash; conversion to the OS separator happens only at the filesystem edge.

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-obsidian-memory/paths_test.go`:

```go
package obsidian

import (
	"path/filepath"
	"testing"
)

func TestKeyToPathAndBack(t *testing.T) {
	root := "/tmp/vault"
	got := keyToPath(root, "herrscher/repos/contracts")
	want := filepath.Join(root, "herrscher", "repos", "contracts.md")
	if got != want {
		t.Fatalf("keyToPath = %q, want %q", got, want)
	}
	if k := pathToKey(root, want); k != "herrscher/repos/contracts" {
		t.Fatalf("pathToKey = %q, want %q", k, "herrscher/repos/contracts")
	}
}

func TestPathToKeyRejectsNonMarkdown(t *testing.T) {
	if k := pathToKey("/tmp/vault", "/tmp/vault/notes/x.txt"); k != "" {
		t.Fatalf("pathToKey on non-.md should be empty, got %q", k)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestKeyToPath -v`
Expected: FAIL — `keyToPath`/`pathToKey` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `/home/shan/dev/herrscher-obsidian-memory/paths.go`:

```go
// Package obsidian implements the contracts.Memory port over an Obsidian-style
// markdown vault: one node per .md file, frontmatter for Meta, [[wikilinks]] for
// Links. The vault is a git-versioned folder; Obsidian is the human UI over it.
package obsidian

import (
	"path/filepath"
	"strings"
)

// keyToPath maps a vault-relative key ("a/b/c") to its on-disk .md file.
func keyToPath(root, key string) string {
	parts := strings.Split(key, "/")
	parts[len(parts)-1] += ".md"
	return filepath.Join(append([]string{root}, parts...)...)
}

// pathToKey is the inverse: an absolute .md path under root → its key, or "" if
// the path is not a .md file under root.
func pathToKey(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || !strings.HasSuffix(rel, ".md") {
		return ""
	}
	rel = strings.TrimSuffix(rel, ".md")
	return filepath.ToSlash(rel)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestKeyToPath|TestPathToKey' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add paths.go paths_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat: vault key↔path mapping"
```

---

### Task 5: Marshal/unmarshal a Node ↔ markdown (frontmatter + wikilinks)

**Files:**
- Create: `/home/shan/dev/herrscher-obsidian-memory/vault.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/vault_test.go`

Format: a node file is `---\n<flat key: value frontmatter>\n---\n<body>`. `type` and
`title` are reserved frontmatter keys mapped to `Node.Kind`/`Node.Title`; all other
frontmatter pairs go to `Node.Meta`. Links are the `[[to]]` / `[[to|rel]]`
occurrences in the body. On marshal, any link in `Node.Links` not already present in
the body is appended under a managed `## Liens` section as `- [[to|rel]]`, so the
agent's links persist without clobbering the human-authored body (co-edition).

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-obsidian-memory/vault_test.go`:

```go
package obsidian

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	n := contracts.Node{
		Key:   "herrscher/index",
		Kind:  contracts.KindProject,
		Title: "Herrscher",
		Body:  "The modular AI harness.\n\nSee [[herrscher/repos/contracts|depends-on]].\n",
		Links: []contracts.Link{
			{To: "herrscher/repos/contracts", Rel: "depends-on"}, // already in body
			{To: "herrscher/repos/gateway", Rel: "contains"},     // must be appended
		},
		Meta: map[string]string{"tags": "platform, go", "status": "active"},
	}

	data := marshalNode(n)
	if !strings.Contains(data, "type: project") || !strings.Contains(data, "title: Herrscher") {
		t.Fatalf("frontmatter missing type/title:\n%s", data)
	}
	if !strings.Contains(data, "[[herrscher/repos/gateway|contains]]") {
		t.Fatalf("missing link not appended:\n%s", data)
	}

	got := unmarshalNode("herrscher/index", []byte(data))
	if got.Kind != contracts.KindProject || got.Title != "Herrscher" {
		t.Fatalf("kind/title lost: %+v", got)
	}
	if got.Meta["status"] != "active" || got.Meta["tags"] != "platform, go" {
		t.Fatalf("meta lost: %+v", got.Meta)
	}
	if len(got.Links) != 2 {
		t.Fatalf("expected 2 links, got %d: %+v", len(got.Links), got.Links)
	}
	// Re-marshalling must be stable (no second "## Liens" growth).
	if strings.Count(marshalNode(got), "## Liens") > 1 {
		t.Fatalf("marshal is not idempotent on links section")
	}
}

func TestUnmarshalNoFrontmatter(t *testing.T) {
	got := unmarshalNode("loose/note", []byte("just a body, no fences\n"))
	if got.Key != "loose/note" || got.Body == "" {
		t.Fatalf("body-only note mishandled: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestMarshal|TestUnmarshal' -v`
Expected: FAIL — `marshalNode`/`unmarshalNode` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `/home/shan/dev/herrscher-obsidian-memory/vault.go`:

```go
package obsidian

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
)

var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)

const liensHeader = "## Liens"

// marshalNode renders a node as frontmatter + body. Links absent from the body are
// appended under a managed "## Liens" section so they survive a round-trip.
func marshalNode(n contracts.Node) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: " + string(n.Kind) + "\n")
	if n.Title != "" {
		b.WriteString("title: " + n.Title + "\n")
	}
	keys := make([]string, 0, len(n.Meta))
	for k := range n.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic output
	for _, k := range keys {
		b.WriteString(k + ": " + n.Meta[k] + "\n")
	}
	b.WriteString("---\n")

	body := n.Body
	present := map[string]bool{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		present[m[1]] = true
	}
	var missing []string
	for _, l := range n.Links {
		if !present[l.To] {
			tag := l.To
			if l.Rel != "" {
				tag += "|" + l.Rel
			}
			missing = append(missing, "- [["+tag+"]]")
			present[l.To] = true
		}
	}
	b.WriteString(body)
	if len(missing) > 0 {
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n" + liensHeader + "\n" + strings.Join(missing, "\n") + "\n")
	}
	return b.String()
}

// unmarshalNode parses a node file. Frontmatter is flat "key: value"; type/title
// are reserved, the rest become Meta. Links are parsed from [[to|rel]] in the body.
func unmarshalNode(key string, data []byte) contracts.Node {
	n := contracts.Node{Key: key, Meta: map[string]string{}}
	s := string(data)
	body := s

	if strings.HasPrefix(s, "---\n") {
		if end := strings.Index(s[4:], "\n---"); end >= 0 {
			front := s[4 : 4+end]
			body = strings.TrimPrefix(s[4+end+4:], "\n")
			for _, line := range strings.Split(front, "\n") {
				k, v, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				k, v = strings.TrimSpace(k), strings.TrimSpace(v)
				switch k {
				case "type":
					n.Kind = contracts.NodeKind(v)
				case "title":
					n.Title = v
				default:
					n.Meta[k] = v
				}
			}
		}
	}
	n.Body = body
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		n.Links = append(n.Links, contracts.Link{To: m[1], Rel: m[2]})
	}
	return n
}
```

Import block is exactly: `regexp`, `sort`, `strings`, and `github.com/Herrscherd/herrscher-contracts`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestMarshal|TestUnmarshal' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add vault.go vault_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat: Node ↔ markdown (frontmatter + wikilinks) round-trip"
```

---

### Task 6: ObsidianMemory — New, Record (upsert), Recall (depth BFS)

**Files:**
- Create: `/home/shan/dev/herrscher-obsidian-memory/memory.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`:

```go
package obsidian

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func newTestMem(t *testing.T) *ObsidianMemory {
	t.Helper()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestRecordThenRecall(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "p/index", Kind: contracts.KindProject, Title: "P"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	sg, err := m.Recall(ctx, "p/index", 0)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if sg.Root.Title != "P" || sg.Root.Kind != contracts.KindProject {
		t.Fatalf("recalled node wrong: %+v", sg.Root)
	}
}

func TestRecordUpsertsNoDuplicate(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "p/index", Kind: contracts.KindProject, Title: "Old"})
	_ = m.Record(ctx, contracts.Node{Key: "p/index", Kind: contracts.KindProject, Title: "New"})
	sg, _ := m.Recall(ctx, "p/index", 0)
	if sg.Root.Title != "New" {
		t.Fatalf("upsert did not update in place: %+v", sg.Root)
	}
}

func TestRecallFollowsLinksToDepth(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject,
		Links: []contracts.Link{{To: "b", Rel: "depends-on"}}})
	_ = m.Record(ctx, contracts.Node{Key: "b", Kind: contracts.KindRepo,
		Links: []contracts.Link{{To: "c", Rel: "contains"}}})
	_ = m.Record(ctx, contracts.Node{Key: "c", Kind: contracts.KindRepo})

	sg, err := m.Recall(ctx, "a", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	keys := map[string]bool{}
	for _, n := range sg.Nodes {
		keys[n.Key] = true
	}
	if !keys["b"] || keys["c"] {
		t.Fatalf("depth-1 should include b but not c; got %v", keys)
	}
	if len(sg.Edges) == 0 {
		t.Fatalf("edges should be populated")
	}
}

func TestRecallMissingKeyErrors(t *testing.T) {
	m := newTestMem(t)
	if _, err := m.Recall(context.Background(), "nope", 0); err == nil {
		t.Fatalf("expected error recalling missing key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestRecord|TestRecall' -v`
Expected: FAIL — `New`/`ObsidianMemory` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `/home/shan/dev/herrscher-obsidian-memory/memory.go`:

```go
package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher-contracts"
)

// ObsidianMemory implements contracts.Memory over a markdown vault rooted at root.
type ObsidianMemory struct {
	root string
}

// New opens (creating if absent) a vault directory and returns a Memory over it.
func New(root string) (*ObsidianMemory, error) {
	if root == "" {
		return nil, fmt.Errorf("obsidian: empty vault root")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("obsidian: create vault root: %w", err)
	}
	return &ObsidianMemory{root: root}, nil
}

func (m *ObsidianMemory) load(key string) (contracts.Node, error) {
	data, err := os.ReadFile(keyToPath(m.root, key))
	if err != nil {
		return contracts.Node{}, fmt.Errorf("obsidian: load %q: %w", key, err)
	}
	return unmarshalNode(key, data), nil
}

// Record upserts a node: keyToPath is deterministic, so writing the same Key
// overwrites the same file (update in place, no duplicate).
func (m *ObsidianMemory) Record(_ context.Context, n contracts.Node) error {
	if n.Key == "" {
		return fmt.Errorf("obsidian: Record needs a Key")
	}
	path := keyToPath(m.root, n.Key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("obsidian: mkdir for %q: %w", n.Key, err)
	}
	if err := os.WriteFile(path, []byte(marshalNode(n)), 0o644); err != nil {
		return fmt.Errorf("obsidian: write %q: %w", n.Key, err)
	}
	return nil
}

// Recall loads the root node and breadth-first follows its links up to depth.
func (m *ObsidianMemory) Recall(_ context.Context, key string, depth int) (contracts.Subgraph, error) {
	root, err := m.load(key)
	if err != nil {
		return contracts.Subgraph{}, err
	}
	sg := contracts.Subgraph{Root: root}
	seen := map[string]bool{key: true}
	frontier := []contracts.Node{root}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []contracts.Node
		for _, n := range frontier {
			for _, l := range n.Links {
				sg.Edges = append(sg.Edges, l)
				if seen[l.To] {
					continue
				}
				seen[l.To] = true
				child, err := m.load(l.To)
				if err != nil {
					continue // dangling link: skip, do not fail the whole recall
				}
				sg.Nodes = append(sg.Nodes, child)
				next = append(next, child)
			}
		}
		frontier = next
	}
	return sg, nil
}

// Close is a no-op: the vault is plain files with nothing to release.
func (m *ObsidianMemory) Close() error { return nil }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestRecord|TestRecall' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat: ObsidianMemory New/Record(upsert)/Recall(depth BFS)"
```

---

### Task 7: Search (kind / tag / text filter)

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`

- [ ] **Step 1: Write the failing test**

Append to `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`:

```go
func TestSearchByKindTagText(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a/index", Kind: contracts.KindProject,
		Title: "Alpha", Body: "uses NATS", Meta: map[string]string{"tags": "platform, go"}})
	_ = m.Record(ctx, contracts.Node{Key: "dec/x", Kind: contracts.KindDecision,
		Title: "Choose NATS", Body: "transport choice"})

	byKind, _ := m.Search(ctx, contracts.Query{Kinds: []contracts.NodeKind{contracts.KindDecision}})
	if len(byKind) != 1 || byKind[0].Key != "dec/x" {
		t.Fatalf("kind filter wrong: %+v", byKind)
	}
	byText, _ := m.Search(ctx, contracts.Query{Text: "nats"}) // case-insensitive, both match
	if len(byText) != 2 {
		t.Fatalf("text filter expected 2, got %d", len(byText))
	}
	byTag, _ := m.Search(ctx, contracts.Query{Tags: []string{"go"}})
	if len(byTag) != 1 || byTag[0].Key != "a/index" {
		t.Fatalf("tag filter wrong: %+v", byTag)
	}
	lim, _ := m.Search(ctx, contracts.Query{Text: "nats", Limit: 1})
	if len(lim) != 1 {
		t.Fatalf("limit not honored: %d", len(lim))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestSearch -v`
Expected: FAIL — `Search` not defined on `*ObsidianMemory`.

- [ ] **Step 3: Write minimal implementation**

Add to `/home/shan/dev/herrscher-obsidian-memory/memory.go` (and add `"strings"` to imports):

```go
// Search walks the vault, unmarshals each node, and returns those matching every
// non-empty facet of the query (kind ∈ Kinds, every tag present, text substring in
// title+body — all case-insensitive). Limit caps the result count (0 = no cap).
func (m *ObsidianMemory) Search(_ context.Context, q contracts.Query) ([]contracts.Node, error) {
	var out []contracts.Node
	err := filepath.Walk(m.root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		key := pathToKey(m.root, path)
		if key == "" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n := unmarshalNode(key, data)
		if matchesQuery(n, q) {
			out = append(out, n)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("obsidian: search: %w", err)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func matchesQuery(n contracts.Node, q contracts.Query) bool {
	if len(q.Kinds) > 0 {
		ok := false
		for _, k := range q.Kinds {
			if n.Kind == k {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if q.Text != "" {
		hay := strings.ToLower(n.Title + "\n" + n.Body)
		if !strings.Contains(hay, strings.ToLower(q.Text)) {
			return false
		}
	}
	if len(q.Tags) > 0 {
		tags := map[string]bool{}
		for _, t := range strings.Split(n.Meta["tags"], ",") {
			tags[strings.TrimSpace(strings.ToLower(t))] = true
		}
		for _, want := range q.Tags {
			if !tags[strings.ToLower(want)] {
				return false
			}
		}
	}
	return true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestSearch -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat: Search by kind/tag/text with limit"
```

---

### Task 8: Links (create a typed edge as a first-class op)

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`

- [ ] **Step 1: Write the failing test**

Append to `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`:

```go
func TestLinksCreatesEdgeVisibleToRecall(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject})
	_ = m.Record(ctx, contracts.Node{Key: "b", Kind: contracts.KindRepo})

	if err := m.Links(ctx, "a", "b", "contains"); err != nil {
		t.Fatalf("Links: %v", err)
	}
	sg, _ := m.Recall(ctx, "a", 1)
	found := false
	for _, n := range sg.Nodes {
		if n.Key == "b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("edge a→b not visible via Recall: %+v", sg)
	}
}

func TestLinksIsIdempotent(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject})
	_ = m.Links(ctx, "a", "b", "contains")
	_ = m.Links(ctx, "a", "b", "contains")
	n, _ := m.load("a")
	count := 0
	for _, l := range n.Links {
		if l.To == "b" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate edge created: %d", count)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestLinks -v`
Expected: FAIL — `Links` not defined on `*ObsidianMemory`.

- [ ] **Step 3: Write minimal implementation**

Add to `/home/shan/dev/herrscher-obsidian-memory/memory.go`:

```go
// Links adds a typed edge from→to. It loads the source node, appends the link if
// absent (idempotent), and re-Records it. The edge then appears in the source's
// body as a [[to|rel]] wikilink.
func (m *ObsidianMemory) Links(ctx context.Context, from, to, rel string) error {
	n, err := m.load(from)
	if err != nil {
		return err
	}
	for _, l := range n.Links {
		if l.To == to {
			return nil // already linked
		}
	}
	n.Links = append(n.Links, contracts.Link{To: to, Rel: rel})
	return m.Record(ctx, n)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestLinks -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat: Links — idempotent typed edge creation"
```

---

### Task 9: `Init` — the idempotent scaffolder

**Files:**
- Create: `/home/shan/dev/herrscher-obsidian-memory/scaffold.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/scaffold_test.go`

`Init` scaffolds the canonical base files for a project (optionally under an org)
plus its repos and servers. It is idempotent: it only creates missing nodes and
never overwrites an existing file. It wires child→parent links (`Repo`/`Server`
`belongs-to` `Project`; `Project` `belongs-to` `Organization`).

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-obsidian-memory/scaffold_test.go`:

```go
package obsidian

import (
	"context"
	"os"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestInitScaffoldsHierarchy(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{
		Org:     "herrscher",
		Project: "herrscher",
		Repos:   []string{"contracts", "gateway"},
		Servers: []string{"vps-1"},
	}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("Init: %v", err)
	}

	proj, err := m.load("herrscher/herrscher/index")
	if err != nil {
		t.Fatalf("project node missing: %v", err)
	}
	if proj.Kind != contracts.KindProject {
		t.Fatalf("project kind wrong: %s", proj.Kind)
	}
	if _, err := m.load("herrscher/herrscher/repos/contracts"); err != nil {
		t.Fatalf("repo node missing: %v", err)
	}
	if _, err := m.load("herrscher/herrscher/servers/vps-1"); err != nil {
		t.Fatalf("server node missing: %v", err)
	}
	if _, err := m.load("herrscher/index"); err != nil {
		t.Fatalf("org node missing: %v", err)
	}
	// architecture + production base docs exist
	if _, err := m.load("herrscher/herrscher/architecture"); err != nil {
		t.Fatalf("architecture doc missing: %v", err)
	}
}

func TestInitIsIdempotentAndNeverOverwrites(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{Project: "solo", Repos: []string{"solo"}}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	// User edits the project node by hand.
	path := keyToPath(m.root, "projets/solo/index")
	if err := os.WriteFile(path, []byte("---\ntype: project\n---\nHUMAN EDIT\n"), 0o644); err != nil {
		t.Fatalf("hand edit: %v", err)
	}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) == "" || !contains(string(got), "HUMAN EDIT") {
		t.Fatalf("Init overwrote a human-edited file: %q", string(got))
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestInit -v`
Expected: FAIL — `InitSpec`/`Init` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `/home/shan/dev/herrscher-obsidian-memory/scaffold.go`:

```go
package obsidian

import (
	"context"
	"os"

	"github.com/Herrscherd/herrscher-contracts"
)

// InitSpec describes a project to scaffold. Org is optional; when empty the
// project lives flat under "projets/<Project>".
type InitSpec struct {
	Org     string
	Project string
	Repos   []string
	Servers []string
}

// base returns the vault path prefix for the project ("<org>/<project>" or
// "projets/<project>").
func (s InitSpec) base() string {
	if s.Org != "" {
		return s.Org + "/" + s.Project
	}
	return "projets/" + s.Project
}

// Init scaffolds the canonical layout. It only writes nodes that do not yet exist
// (idempotent, never overwrites), and links children to their parents.
func (m *ObsidianMemory) Init(ctx context.Context, s InitSpec) error {
	if s.Project == "" {
		return errEmptyProject
	}
	base := s.base()

	if s.Org != "" {
		orgKey := s.Org + "/index"
		if err := m.ensure(ctx, contracts.Node{Key: orgKey, Kind: contracts.KindOrganization,
			Title: s.Org, Links: []contracts.Link{{To: base + "/index", Rel: "contains"}}}); err != nil {
			return err
		}
	}

	projKey := base + "/index"
	projLinks := []contracts.Link{}
	if s.Org != "" {
		projLinks = append(projLinks, contracts.Link{To: s.Org + "/index", Rel: "belongs-to"})
	}
	if err := m.ensure(ctx, contracts.Node{Key: projKey, Kind: contracts.KindProject,
		Title: s.Project, Links: projLinks}); err != nil {
		return err
	}
	if err := m.ensure(ctx, contracts.Node{Key: base + "/architecture", Kind: contracts.KindArchitecture,
		Title: s.Project + " — architecture", Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}}}); err != nil {
		return err
	}
	if err := m.ensure(ctx, contracts.Node{Key: base + "/production", Kind: contracts.KindProduction,
		Title: s.Project + " — production", Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}}}); err != nil {
		return err
	}

	for _, r := range s.Repos {
		if err := m.ensure(ctx, contracts.Node{Key: base + "/repos/" + r, Kind: contracts.KindRepo,
			Title: r, Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}}}); err != nil {
			return err
		}
	}
	for _, sv := range s.Servers {
		if err := m.ensure(ctx, contracts.Node{Key: base + "/servers/" + sv, Kind: contracts.KindServer,
			Title: sv, Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}}}); err != nil {
			return err
		}
	}
	return nil
}

// ensure Records the node only if its file does not already exist.
func (m *ObsidianMemory) ensure(ctx context.Context, n contracts.Node) error {
	if _, err := os.Stat(keyToPath(m.root, n.Key)); err == nil {
		return nil // exists — never overwrite
	}
	return m.Record(ctx, n)
}
```

Add the sentinel error to `memory.go` (top level, after the imports):

```go
var errEmptyProject = fmt.Errorf("obsidian: Init needs a Project name")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestInit -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add scaffold.go memory.go scaffold_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat: idempotent Init scaffolder (org→project→repos/servers)"
```

---

### Task 10: Self-registration into the plugin registry

**Files:**
- Create: `/home/shan/dev/herrscher-obsidian-memory/register.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/register_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-obsidian-memory/register_test.go`:

```go
package obsidian

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestSelfRegisteredAsMemory(t *testing.T) {
	found := false
	for _, p := range contracts.Default.Memories() {
		if p.Manifest.Kind == "obsidian" {
			found = true
			if p.Memory == nil {
				t.Fatalf("obsidian plugin registered without a Memory factory")
			}
		}
	}
	if !found {
		t.Fatalf("obsidian plugin did not self-register as a memory plugin")
	}
}

func TestFactoryBuildsMemory(t *testing.T) {
	cfg := contracts.PluginConfig{Settings: map[string]string{"vault": t.TempDir()}}
	var factory contracts.MemoryFactory
	for _, p := range contracts.Default.Memories() {
		if p.Manifest.Kind == "obsidian" {
			factory = p.Memory
		}
	}
	mem, err := factory(context.Background(), cfg)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := mem.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestSelfRegistered|TestFactory' -v`
Expected: FAIL — the plugin is not registered (no `register.go`).

- [ ] **Step 3: Write minimal implementation**

Create `/home/shan/dev/herrscher-obsidian-memory/register.go`:

```go
package obsidian

import (
	"context"

	"github.com/Herrscherd/herrscher-contracts"
)

// init self-registers the Obsidian memory plugin into the global registry. A blank
// import of this package (in the host's generated plugins.go) makes it discoverable
// with no host wiring. The factory maps the neutral PluginConfig onto New.
func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "obsidian",
			Category: contracts.CategoryMemory,
			Config: []contracts.Setting{
				{Key: "vault", Env: "OBSIDIAN_VAULT", Help: "path to the memory vault directory", Required: true},
			},
		},
		Memory: func(ctx context.Context, cfg contracts.PluginConfig) (contracts.Memory, error) {
			return New(cfg.Get("vault"))
		},
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: PASS (all tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add register.go register_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "feat: self-register the Obsidian memory plugin (xcaddy pattern)"
```

---

### Task 11: Golden example vault (dogfood Herrscher) + fixture test

**Files:**
- Create: `/home/shan/dev/herrscher-obsidian-memory/examples/vault/herrscher/index.md`
- Create: `…/examples/vault/herrscher/herrscher/index.md`
- Create: `…/examples/vault/herrscher/herrscher/architecture.md`
- Create: `…/examples/vault/herrscher/herrscher/repos/contracts.md`
- Create: `…/examples/vault/herrscher/herrscher/repos/obsidian-memory.md`
- Create: `…/examples/vault/user/preferences.md`
- Create: `…/examples/vault/decisions/memory-as-obsidian-graph.md`
- Test: `/home/shan/dev/herrscher-obsidian-memory/golden_test.go`

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-obsidian-memory/golden_test.go`:

```go
package obsidian

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestGoldenVaultRecallTraverses(t *testing.T) {
	m, err := New("examples/vault")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sg, err := m.Recall(context.Background(), "herrscher/herrscher/index", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if sg.Root.Kind != contracts.KindProject {
		t.Fatalf("root should be a project, got %s", sg.Root.Kind)
	}
	reached := map[string]bool{}
	for _, n := range sg.Nodes {
		reached[n.Key] = true
	}
	if !reached["herrscher/herrscher/repos/contracts"] {
		t.Fatalf("project does not reach its contracts repo: %v", reached)
	}
}

func TestGoldenVaultSearchFindsDecision(t *testing.T) {
	m, _ := New("examples/vault")
	res, _ := m.Search(context.Background(), contracts.Query{Kinds: []contracts.NodeKind{contracts.KindDecision}})
	if len(res) == 0 {
		t.Fatalf("expected at least one decision node in the golden vault")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestGolden -v`
Expected: FAIL — `examples/vault` is empty, Recall errors on missing root.

- [ ] **Step 3: Create the golden vault files**

`examples/vault/herrscher/index.md`:

```markdown
---
type: organization
title: Herrscherd
---
The GitHub org hosting the Herrscher platform repos.

## Liens
- [[herrscher/herrscher/index|contains]]
```

`examples/vault/herrscher/herrscher/index.md`:

```markdown
---
type: project
status: active
tags: platform, go
title: Herrscher
---
A hyper-modular AI harness: every capability is a plugin behind a neutral contract
(Gateway, Backend, Memory, Orchestrator).

## Liens
- [[herrscher/index|belongs-to]]
- [[herrscher/herrscher/architecture|documented-by]]
- [[herrscher/herrscher/repos/contracts|contains]]
- [[herrscher/herrscher/repos/obsidian-memory|contains]]
```

`examples/vault/herrscher/herrscher/architecture.md`:

```markdown
---
type: architecture
title: Herrscher — architecture
---
Plugins self-register in init() (xcaddy pattern); the host resolves a PluginConfig
from each plugin's declared Settings. Categories: gateway, backend, memory,
orchestrator. The Orchestrator has a hard dependency on Memory.

## Liens
- [[herrscher/herrscher/index|belongs-to]]
```

`examples/vault/herrscher/herrscher/repos/contracts.md`:

```markdown
---
type: repo
title: herrscher-contracts
---
The shared ABI: plugin manifest, capability model, and the neutral ports
(Gateway, Backend, Memory).

## Liens
- [[herrscher/herrscher/index|belongs-to]]
```

`examples/vault/herrscher/herrscher/repos/obsidian-memory.md`:

```markdown
---
type: repo
title: herrscher-obsidian-memory
---
The first Memory implementation: a co-edited markdown knowledge graph.

## Liens
- [[herrscher/herrscher/index|belongs-to]]
- [[herrscher/herrscher/repos/contracts|depends-on]]
- [[decisions/memory-as-obsidian-graph|realizes]]
```

`examples/vault/user/preferences.md`:

```markdown
---
type: user
title: User — preferences
---
Identity: GitHub Akayashuu, commit email sauvageleo1@gmail.com.
Work style: clean code, minimal comments. Prefers clarifying a question over
guessing.

## Liens
- [[herrscher/herrscher/index|applies-to]]
```

`examples/vault/decisions/memory-as-obsidian-graph.md`:

```markdown
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestGolden -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add examples golden_test.go
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "test: golden Herrscher reference vault + fixture tests"
```

---

### Task 12: Full suite + README

**Files:**
- Create: `/home/shan/dev/herrscher-obsidian-memory/README.md`

- [ ] **Step 1: Run the full suite in both repos**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS (existing suite + the new memory/registry tests).

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go vet ./... && go test ./...`
Expected: PASS, no vet warnings.

- [ ] **Step 2: Write the README**

Create `/home/shan/dev/herrscher-obsidian-memory/README.md`:

```markdown
# herrscher-obsidian-memory

The Obsidian implementation of the Herrscher `Memory` port: a co-edited markdown
knowledge graph. One node = one `.md` file, `Meta` ↔ frontmatter, `Links` ↔
`[[wikilinks]]`. The vault is a git-versioned folder; Obsidian is the human UI over
it.

## Node kinds

`Organization → Project → Repo/Server` form the structural spine; `Architecture`,
`Production`, `Session`, `Decision` are documentary; `User` models the user
(cross-cutting, evolving).

## Usage

A blank import wires the plugin into a Herrscher host (xcaddy pattern):

    import _ "github.com/Herrscherd/herrscher-obsidian-memory"

Config: `OBSIDIAN_VAULT` (required) — path to the vault directory.

## Curation

This plugin exposes only the passive verbs (Recall/Record/Search/Links). The
proactive "nudge" loop is the `contracts.CurationHook` seam, implemented later by
the Orchestrator — not here.

See `docs/superpowers/specs/2026-06-16-herrscher-obsidian-memory-design.md`.
```

- [ ] **Step 3: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add README.md
git -c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com commit -m "docs: README for the Obsidian memory plugin"
```

- [ ] **Step 4: Push both repos**

```bash
cd /home/shan/dev/herrscher-contracts && git push
cd /home/shan/dev/herrscher-obsidian-memory && git push -u origin HEAD
```

---

## Notes for the implementer

- **House style:** no external dependencies — stdlib only, matching `herrscher-contracts`. Minimal comments (the user prefers clean code over commentary), but keep the doc comments that explain *why* (port purpose, seam intent).
- **Commit identity:** every commit uses `-c user.name=Akayashuu -c user.email=sauvageleo1@gmail.com` (the user's real git identity).
- **The curation loop is out of scope.** Only `contracts.CurationHook` is declared. Do not implement a scheduler, a host hook, or any auto-write.
- **Dangling links** in Recall are skipped, not fatal — a co-edited vault will transiently reference not-yet-created notes.
