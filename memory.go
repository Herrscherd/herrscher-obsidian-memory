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

	"github.com/Herrscherd/herrscher-contracts"
)

// ObsidianMemory implements contracts.Memory over a markdown vault. All file I/O
// goes through an *os.Root so a malicious key or an in-vault symlink can never
// escape the root. A mutex serializes writes (Links is read-modify-write).
type ObsidianMemory struct {
	mu   sync.Mutex
	root *os.Root
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
	return &ObsidianMemory{root: r}, nil
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
	if err := m.root.WriteFile(rel, []byte(marshalNode(n)), 0o644); err != nil {
		return fmt.Errorf("obsidian: write %q: %w", n.Key, err)
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
func (m *ObsidianMemory) Record(_ context.Context, n contracts.Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recordUnlocked(n)
}

// Recall loads the root node and breadth-first follows its links up to depth.
func (m *ObsidianMemory) Recall(_ context.Context, key string, depth int) (contracts.Subgraph, error) {
	root, err := m.load(key)
	if err != nil {
		return contracts.Subgraph{}, err
	}
	sg := contracts.Subgraph{Root: root}
	seen := map[string]bool{key: true}
	edges := map[contracts.Link]bool{}
	frontier := []contracts.Node{root}
	for d := 0; d < depth && len(frontier) > 0; d++ {
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

// Links adds a typed edge from→to as a [[to|rel]] wikilink in the source's body.
// It is idempotent on the target: if an edge to `to` already exists it is left
// untouched (the vault document owns the relation label, since a human co-edits
// it), so re-linking with a different rel does not rewrite their prose.
func (m *ObsidianMemory) Links(_ context.Context, from, to, rel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

// Close releases the vault root handle.
func (m *ObsidianMemory) Close() error { return m.root.Close() }

func (m *ObsidianMemory) Search(_ context.Context, q contracts.Query) ([]contracts.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fsys := m.root.FS()
	var out []contracts.Node
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
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
		for _, want := range q.Tags {
			if !tags[strings.ToLower(want)] {
				return false
			}
		}
	}
	return true
}
