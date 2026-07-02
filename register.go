package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher-contracts"
)

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "obsidian",
			Category: contracts.CategoryMemory,
			Config: []contracts.Setting{
				{Key: "vault", Env: "OBSIDIAN_VAULT", Help: "path to the memory vault directory (default ~/.herrscher/memory)", Required: false},
			},
		},
		Memory: func(ctx context.Context, cfg contracts.PluginConfig) (contracts.Memory, error) {
			root := cfg.Get("vault")
			if root == "" {
				// Default to the shared vault under ~/.herrscher, which survives
				// worktree teardown. Resolved here (not as a static manifest Default)
				// because a manifest string cannot expand ~/$HOME.
				home, err := os.UserHomeDir()
				if err != nil {
					return nil, fmt.Errorf("obsidian: default vault path: %w", err)
				}
				root = filepath.Join(home, ".herrscher", "memory")
			}
			// EnsureVault (not New): provision a missing directory + .obsidian config
			// so the vault opens as an Obsidian vault with no manual setup.
			return EnsureVault(root)
		},
	})
}
