// Package obsidian implements the contracts.Memory port over an Obsidian-style
// markdown vault: one node per .md file, frontmatter for Meta, [[wikilinks]] for
// Links. The vault is a git-versioned folder; Obsidian is the human UI over it.
package obsidian

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher-contracts"
)

// lockName is the advisory cross-process lock file kept at the vault root. It is
// a hidden non-markdown file, so Obsidian, git, and Search all ignore it.
const lockName = ".herrscher.lock"

// lockPollInterval is how often flock retries the non-blocking cross-process lock
// while a peer holds it, until ctx expires.
const lockPollInterval = 10 * time.Millisecond

// ObsidianMemory implements contracts.Memory over a markdown vault. All file I/O
// goes through an *os.Root so a malicious key or an in-vault symlink can never
// escape the root. The mutex serializes writes within this process; lockFile
// serializes them across processes (the daemon spawns one bridge subprocess per
// session, all sharing the same vault), and every write lands atomically via a
// temp file + rename so a vault never sees a torn document.
type ObsidianMemory struct {
	mu       sync.Mutex
	root     *os.Root
	lockFile *os.File
	now      func() time.Time // injectable clock for capturedAt stamping (tests override)

	// parseCache memoizes the parsed Node of each .md file so a Search walk only
	// re-reads and re-parses files whose size or mtime changed since last seen,
	// turning the repeated whole-vault read+parse+regex into a stat per file for
	// the unchanged majority. Guarded by mu (Search holds it); every write bumps
	// the file's mtime via the atomic rename, so stale entries self-invalidate.
	parseCache map[string]cachedNode

	// budget is the per-node Body rune budget enforced by Record; 0 disables it.
	// Guarded by mu (SetNodeBudget writes it, recordUnlocked reads it under mu).
	budget int
}

type cachedNode struct {
	mod  time.Time
	size int64
	node contracts.Node
}

// New opens (creating if absent) a vault directory and returns a Memory over it.
func New(root string) (*ObsidianMemory, error) {
	if root == "" {
		return nil, fmt.Errorf("obsidian: empty vault root")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("obsidian: create vault root: %w", err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("obsidian: open vault root: %w", err)
	}
	lf, err := r.OpenFile(lockName, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("obsidian: open vault lock: %w", err)
	}
	return &ObsidianMemory{root: r, lockFile: lf, now: time.Now, parseCache: map[string]cachedNode{}}, nil
}

// flock takes the exclusive cross-process lock and returns its release func. The
// in-process mutex must already be held so the lock is taken at most once per
// process at a time. It polls the non-blocking lock so a stuck peer cannot pin
// the call past ctx; on timeout or a real lock error it logs and proceeds
// best-effort (the in-process mutex still serializes this process). Callers
// defer the returned func.
func (m *ObsidianMemory) flock(ctx context.Context) func() {
	for {
		err := lockFD(m.lockFile.Fd())
		if err == nil {
			return func() { _ = unlockFD(m.lockFile.Fd()) }
		}
		if err != syscall.EWOULDBLOCK {
			fmt.Fprintf(os.Stderr, "obsidian: vault lock: %v\n", err)
			return func() {}
		}
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "obsidian: vault lock: proceeding without cross-process lock: %v\n", ctx.Err())
			return func() {}
		case <-time.After(lockPollInterval):
		}
	}
}

// SetNodeBudget sets the per-node Body budget in runes; 0 (the default) disables
// enforcement. When positive, Record returns *contracts.BudgetError for any node
// whose Body exceeds it — the caller must consolidate/replace to fit (G1).
func (m *ObsidianMemory) SetNodeBudget(runes int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.budget = runes
}

func (m *ObsidianMemory) loadUnlocked(key string) (contracts.Node, error) {
	if err := validKey(key); err != nil {
		return contracts.Node{}, err
	}
	data, err := m.root.ReadFile(keyToRel(key))
	if err != nil {
		return contracts.Node{}, fmt.Errorf("obsidian: load %q: %w", key, err)
	}
	return unmarshalNode(key, data), nil
}

// recordUnlocked upserts a node, reading the existing file (if any) to preserve a
// prior capturedAt stamp. Callers that already know no prior file exists, or that
// carry the loaded node, use recordUnlockedNoReload to skip that read.
func (m *ObsidianMemory) recordUnlocked(n contracts.Node) error {
	// KindTranscript is the append-only raw archival tier (G7): it must be stored
	// verbatim, so the per-node Body budget never applies to it. Every distilled
	// kind is still subject to the budget.
	if m.budget > 0 && n.Kind != contracts.KindTranscript {
		if r := utf8.RuneCountInString(n.Body); r > m.budget {
			return &contracts.BudgetError{Key: n.Key, Runes: r, Limit: m.budget}
		}
	}
	return m.writeNode(n, true)
}

// recordUnlockedNoReload writes without reading the existing file for a prior
// capturedAt: for ensure the file is known-absent, and for Links the caller passes
// the already-loaded node (which carries any prior stamp), so the extra read would
// be pure overhead.
func (m *ObsidianMemory) recordUnlockedNoReload(n contracts.Node) error {
	return m.writeNode(n, false)
}

func (m *ObsidianMemory) writeNode(n contracts.Node, reloadPrior bool) error {
	if err := validKey(n.Key); err != nil {
		return err
	}
	// Stamp capturedAt (RFC3339 UTC) so recall can rank by recency. Only when
	// absent: a caller-supplied value is kept, and on upsert an existing stored
	// value is preserved so re-recording the same fact does not reset its age.
	if n.Meta["capturedAt"] == "" {
		at := m.now().UTC().Format(time.RFC3339)
		if reloadPrior {
			if existing, err := m.loadUnlocked(n.Key); err == nil {
				if prior := existing.Meta["capturedAt"]; prior != "" {
					at = prior
				}
			}
		}
		if n.Meta == nil {
			n.Meta = map[string]string{}
		}
		n.Meta["capturedAt"] = at
	}
	// Stamp lastSeen (RFC3339 UTC) — the staleness machine's age basis. Unlike
	// capturedAt, it is NOT preserved on upsert: an ordinary re-record bumps it
	// to now (reactivation), while the curator sweep re-supplies the existing
	// value so a state-only write leaves the node's age untouched.
	if n.Meta[contracts.MetaLastSeen] == "" {
		if n.Meta == nil {
			n.Meta = map[string]string{}
		}
		n.Meta[contracts.MetaLastSeen] = m.now().UTC().Format(time.RFC3339)
	}
	rel := keyToRel(n.Key)
	if dir := filepath.Dir(rel); dir != "." {
		if err := m.root.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("obsidian: mkdir for %q: %w", n.Key, err)
		}
	}
	// Write to a temp sibling then rename: rename is atomic on a POSIX
	// filesystem, so a reader (or a crash) never observes a half-written node.
	tmp := rel + ".tmp"
	if err := m.root.WriteFile(tmp, []byte(marshalNode(n)), 0o644); err != nil {
		return fmt.Errorf("obsidian: write %q: %w", n.Key, err)
	}
	if err := m.root.Rename(tmp, rel); err != nil {
		_ = m.root.Remove(tmp)
		return fmt.Errorf("obsidian: commit %q: %w", n.Key, err)
	}
	return nil
}

func (m *ObsidianMemory) load(key string) (contracts.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadUnlocked(key)
}

// Record upserts a node: keyToRel is deterministic, so writing the same Key
// overwrites the same file (update in place, no duplicate).
func (m *ObsidianMemory) Record(ctx context.Context, n contracts.Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.flock(ctx)()
	return m.recordUnlocked(n)
}

// Recall loads the root node and breadth-first follows its links up to depth.
// It holds the in-process mutex and the cross-process lock for the whole walk so
// a concurrent writer can't make it see a node mid-update or miss a just-written
// one (every read goes through loadUnlocked under that lock).
func (m *ObsidianMemory) Recall(ctx context.Context, key string, depth int) (contracts.Subgraph, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.flock(ctx)()
	root, err := m.loadUnlocked(key)
	if err != nil {
		return contracts.Subgraph{}, err
	}
	sg := contracts.Subgraph{Root: root}
	seen := map[string]bool{key: true}
	edges := map[contracts.Link]bool{}
	frontier := []contracts.Node{root}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		if err := ctx.Err(); err != nil {
			return contracts.Subgraph{}, err
		}
		var next []contracts.Node
		for _, n := range frontier {
			for _, l := range n.Links {
				if !edges[l] {
					edges[l] = true
					sg.Edges = append(sg.Edges, l)
				}
				if seen[l.To] {
					continue
				}
				seen[l.To] = true
				child, err := m.loadUnlocked(l.To)
				if err != nil {
					continue // dangling link: skip, do not fail the whole recall
				}
				if child.Meta[contracts.MetaState] == contracts.StateArchived {
					continue // archived neighbor: hide from graph expansion (root is always returned)
				}
				if child.Kind == contracts.KindTranscript {
					continue // raw G7 transcript: never surfaces via graph expansion (invisible by default, no IncludeRaw concept on Recall)
				}
				sg.Nodes = append(sg.Nodes, child)
				next = append(next, child)
			}
		}
		frontier = next
	}
	return sg, nil
}

// Links adds a typed edge from→to as a [[to|rel]] wikilink in the source's body.
// It is idempotent on the target: if an edge to `to` already exists it is left
// untouched (the vault document owns the relation label, since a human co-edits
// it), so re-linking with a different rel does not rewrite their prose.
func (m *ObsidianMemory) Links(ctx context.Context, from, to, rel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.flock(ctx)()
	n, err := m.loadUnlocked(from)
	if err != nil {
		return err
	}
	for _, l := range n.Links {
		if l.To == to {
			return nil // already linked
		}
	}
	n.Links = append(n.Links, contracts.Link{To: to, Rel: rel})
	return m.recordUnlockedNoReload(n)
}

// Unlink removes the edge from→to (the inverse of Links): it drops every
// contracts.Link with To == to from the source node AND excises the matching
// [[to|rel]] wikilink text from its body, since the obsidian body is the source
// of truth for edges (marshalNode only appends, never removes, so dropping the
// Link alone would round-trip straight back). Idempotent: an absent edge is a
// no-op. Under the same lock as Links; a human co-owns the note.
func (m *ObsidianMemory) Unlink(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.flock(ctx)()
	n, err := m.loadUnlocked(from)
	if err != nil {
		return err
	}
	kept := n.Links[:0:0]
	found := false
	for _, l := range n.Links {
		if l.To == to {
			found = true
			continue
		}
		kept = append(kept, l)
	}
	if !found {
		return nil // no edge to `to` — no-op, no needless mtime bump
	}
	n.Links = kept
	n.Body = exciseWikilinks(n.Body, to)
	return m.recordUnlockedNoReload(n)
}

// Close releases the vault lock and root handle.
func (m *ObsidianMemory) Close() error {
	if m.lockFile != nil {
		_ = m.lockFile.Close()
	}
	return m.root.Close()
}

func (m *ObsidianMemory) Search(ctx context.Context, q contracts.Query) ([]contracts.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.flock(ctx)()
	fsys := m.root.FS()
	var out []contracts.Node
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(path, ".md") {
			return nil // skip dirs, symlinks, and non-markdown
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if c, ok := m.parseCache[path]; ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
			if matchesQuery(c.node, q) {
				out = append(out, c.node)
			}
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		n := unmarshalNode(strings.TrimSuffix(path, ".md"), data)
		m.parseCache[path] = cachedNode{mod: info.ModTime(), size: info.Size(), node: n}
		if matchesQuery(n, q) {
			out = append(out, n)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("obsidian: search: %w", err)
	}
	if q.Ranked {
		// matchesQuery already gated membership, so every node here is a genuine
		// match — ranking only orders them by relevance (highest first), then the
		// Limit below takes the top-K. A stable sort keeps walk order among ties.
		scores := make(map[string]float64, len(out))
		for _, n := range out {
			s, _ := contracts.Score(q.Text, m.now().UTC(), n)
			scores[n.Key] = s
		}
		sort.SliceStable(out, func(i, j int) bool { return scores[out[i].Key] > scores[out[j].Key] })
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func matchesQuery(n contracts.Node, q contracts.Query) bool {
	if n.Meta[contracts.MetaState] == contracts.StateArchived && !q.IncludeArchived {
		return false
	}
	if n.Kind == contracts.KindTranscript && !q.IncludeRaw {
		return false // G7 raw archival tier: hidden unless the caller opts in
	}
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
		rawTags := strings.ToLower(n.Meta["tags"])
		domain := strings.TrimSpace(strings.ToLower(n.Meta["domain"]))
		for _, want := range q.Tags {
			w := strings.ToLower(want)
			if domain != "" && w == domain {
				continue
			}
			found := false
			for _, t := range strings.Split(rawTags, ",") {
				if strings.TrimSpace(t) == w {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
