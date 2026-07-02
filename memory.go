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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// lockName is the advisory cross-process lock file kept at the vault root. It is
// a hidden non-markdown file, so Obsidian, git, and Search all ignore it.
const lockName = ".herrscher.lock"

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
	return &ObsidianMemory{root: r, lockFile: lf}, nil
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
			return func() {}
		case <-time.After(10 * time.Millisecond):
		}
	}
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

func (m *ObsidianMemory) recordUnlocked(n contracts.Node) error {
	if err := validKey(n.Key); err != nil {
		return err
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
	return m.recordUnlocked(n)
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
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		n := unmarshalNode(strings.TrimSuffix(path, ".md"), data)
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
		if d := strings.TrimSpace(strings.ToLower(n.Meta["domain"])); d != "" {
			tags[d] = true
		}
		for _, want := range q.Tags {
			if !tags[strings.ToLower(want)] {
				return false
			}
		}
	}
	return true
}
