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
