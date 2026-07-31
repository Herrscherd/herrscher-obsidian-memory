package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Herrscherd/herrscher-contracts"
)

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "obsidian",
			Category: contracts.CategoryMemory,
			Status:   contracts.StatusLive,
			Config: []contracts.Setting{
				{Key: "vault", Env: "OBSIDIAN_VAULT", Help: "path to the memory vault directory (default ~/.herrscher/memory)", Required: false},
				{Key: "node-budget", Env: "OBSIDIAN_NODE_BUDGET", Help: "per-node Body budget in runes; 0 disables (default 2000)", Required: false},
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
			mem, err := EnsureVault(root)
			if err != nil {
				return nil, err
			}
			budget := 2000
			if v := cfg.Get("node-budget"); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					return nil, fmt.Errorf("obsidian: node-budget: %w", err)
				}
				budget = n
			}
			mem.SetNodeBudget(budget)
			return mem, nil
		},
	})
}
