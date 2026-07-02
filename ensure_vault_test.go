package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestEnsureVaultCreatesObsidianConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	m, err := EnsureVault(root)
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	defer m.Close()

	for _, name := range []string{".obsidian/app.json", ".obsidian/appearance.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	if err := m.Record(context.Background(), contracts.Node{Key: "n", Kind: contracts.KindDecision, Title: "t"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	sg, err := m.Recall(context.Background(), "n", 0)
	if err != nil || sg.Root.Key != "n" {
		t.Fatalf("Recall: %+v err=%v", sg, err)
	}
}

func TestEnsureVaultNeverOverwritesExistingObsidianConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(root, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := []byte(`{"theme":"obsidian"}`)
	if err := os.WriteFile(filepath.Join(root, ".obsidian", "app.json"), custom, 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := EnsureVault(root)
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	defer m.Close()

	got, err := os.ReadFile(filepath.Join(root, ".obsidian", "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("app.json overwritten: got %q, want %q", got, custom)
	}
}
