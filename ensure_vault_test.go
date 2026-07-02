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

func TestEnsureAgentAndProjectAreIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	m, err := EnsureVault(root)
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	defer m.Close()
	ctx := context.Background()

	// Use the interface, exactly as the host will (type-asserted Provisioner).
	var p contracts.Provisioner = m

	if err := p.EnsureProject(ctx, contracts.ProjectKey("game"), "game"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := p.EnsureAgent(ctx, contracts.AgentKey("scripter"), "scripter"); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}

	// Mutate the project node, then re-ensure: an existing node is never clobbered.
	proj, err := m.load(contracts.ProjectKey("game"))
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	proj.Body = "hand-edited"
	if err := m.Record(ctx, proj); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := p.EnsureProject(ctx, contracts.ProjectKey("game"), "game"); err != nil {
		t.Fatalf("EnsureProject (2nd): %v", err)
	}
	got, err := m.load(contracts.ProjectKey("game"))
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if got.Body != "hand-edited" {
		t.Fatalf("EnsureProject clobbered an existing node: body=%q", got.Body)
	}
	if got.Kind != contracts.KindProject {
		t.Fatalf("project kind: got %q, want %q", got.Kind, contracts.KindProject)
	}

	// The provisioned roots make a scoped round-trip work with no missing-root error.
	s := contracts.MemoryScope{Project: contracts.ProjectKey("game"), Agent: contracts.AgentKey("scripter")}
	if err := contracts.RecordShared(ctx, m, s, contracts.Node{Key: "facts/eco", Kind: contracts.KindDecision, Title: "eco"}); err != nil {
		t.Fatalf("RecordShared: %v", err)
	}
	if err := contracts.RecordPrivate(ctx, m, s, contracts.Node{Key: "skills/ds", Kind: contracts.KindDecision, Title: "ds"}); err != nil {
		t.Fatalf("RecordPrivate: %v", err)
	}
	if _, err := contracts.RecallScoped(ctx, m, s, 1); err != nil {
		t.Fatalf("RecallScoped: %v", err)
	}
}
