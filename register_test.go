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
