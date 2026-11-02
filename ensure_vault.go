package obsidian

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// EnsureVault opens (creating if absent) the vault at root and additionally
// writes a minimal .obsidian/ app config when it is missing, so the Obsidian app
// opens the folder as a vault without prompting. It is the create-or-open
// superset of New: New stays open-only/strict (no .obsidian/ writes); EnsureVault
// is what the manifest/host use. Existing .obsidian/ files are never overwritten.
func EnsureVault(root string) (*ObsidianMemory, error) {
	m, err := New(root)
	if err != nil {
		return nil, err
	}
	if err := m.ensureObsidianDir(); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}

func (m *ObsidianMemory) ensureObsidianDir() error {
	if err := m.root.MkdirAll(".obsidian", 0o700); err != nil {
		return fmt.Errorf("obsidian: create .obsidian dir: %w", err)
	}
	for _, name := range []string{".obsidian/app.json", ".obsidian/appearance.json"} {
		if _, err := m.root.Stat(name); err == nil {
			continue
		}
		if err := m.root.WriteFile(name, []byte("{}\n"), 0o600); err != nil {
			return fmt.Errorf("obsidian: write %s: %w", name, err)
		}
	}
	return nil
}

// EnsureAgent ensures the private KindAgent root node at key exists, creating it
// with title as its heading only when absent (idempotent, it never overwrites an
// existing node). Callers pass contracts.AgentKey(name) so the node lands exactly
// where the orchestrator's scope will look.
func (m *ObsidianMemory) EnsureAgent(ctx context.Context, key, title string) error {
	return m.ensure(ctx, contracts.Node{Key: key, Kind: contracts.KindAgent, Title: title})
}

// EnsureProject ensures the shared KindProject root node at key exists, creating
// it only when absent (idempotent). Callers pass contracts.ProjectKey(name).
func (m *ObsidianMemory) EnsureProject(ctx context.Context, key, title string) error {
	return m.ensure(ctx, contracts.Node{Key: key, Kind: contracts.KindProject, Title: title})
}

var _ contracts.Provisioner = (*ObsidianMemory)(nil)
