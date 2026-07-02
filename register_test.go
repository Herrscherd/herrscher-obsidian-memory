package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// findPlugin returns the registered obsidian memory plugin.
func findPlugin(t *testing.T) contracts.Plugin {
	t.Helper()
	for _, p := range contracts.Default.Memories() {
		if p.Manifest.Kind == "obsidian" {
			return p
		}
	}
	t.Fatal("obsidian memory plugin not registered")
	return contracts.Plugin{}
}

func TestManifestVaultIsOptional(t *testing.T) {
	p := findPlugin(t)
	for _, s := range p.Manifest.Config {
		if s.Key == "vault" && s.Required {
			t.Fatal("vault setting must be optional (Required=false)")
		}
	}
	if _, err := contracts.Resolve(p.Manifest.Config, func(string) string { return "" }); err != nil {
		t.Fatalf("Resolve with empty env: %v", err)
	}
}

func TestFactoryUsesExplicitVaultAndInitsObsidian(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	p := findPlugin(t)
	mem, err := p.Memory(context.Background(), contracts.PluginConfig{Settings: map[string]string{"vault": root}})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer mem.Close()
	if _, err := os.Stat(filepath.Join(root, ".obsidian", "app.json")); err != nil {
		t.Fatalf("factory did not init .obsidian: %v", err)
	}
}

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
