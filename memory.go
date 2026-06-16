package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
)

var errEmptyProject = fmt.Errorf("obsidian: Init needs a Project name")

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
	if err := validKey(key); err != nil {
		return contracts.Node{}, err
	}
	data, err := os.ReadFile(keyToPath(m.root, key))
	if err != nil {
		return contracts.Node{}, fmt.Errorf("obsidian: load %q: %w", key, err)
	}
	return unmarshalNode(key, data), nil
}

// Record upserts a node: keyToPath is deterministic, so writing the same Key
// overwrites the same file (update in place, no duplicate).
func (m *ObsidianMemory) Record(_ context.Context, n contracts.Node) error {
	if err := validKey(n.Key); err != nil {
		return err
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

// Close is a no-op: the vault is plain files with nothing to release.
func (m *ObsidianMemory) Close() error { return nil }

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
