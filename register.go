package obsidian

import (
	"context"

	"github.com/Herrscherd/herrscher-contracts"
)

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
