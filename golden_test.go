package obsidian

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const goldenVault = "examples/vault"

func copyGoldenVault(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	must(t, filepath.WalkDir(goldenVault, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(goldenVault, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, privateDirMode)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, privateFileMode)
	}))
	return dst
}

func TestGoldenVaultRecallTraverses(t *testing.T) {
	m := mustNew(t, copyGoldenVault(t))
	closeOnCleanup(t, m)
	sg, err := m.Recall(context.Background(), "herrscher/herrscher/index", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if sg.Root.Kind != contracts.KindProject {
		t.Fatalf("root should be a project, got %s", sg.Root.Kind)
	}
	reached := make(map[string]bool)
	for _, n := range sg.Nodes {
		reached[n.Key] = true
	}
	if !reached["herrscher/herrscher/repos/contracts"] {
		t.Fatalf("project does not reach its contracts repo: %v", reached)
	}
}

func TestGoldenVaultSearchFindsDecision(t *testing.T) {
	m := mustNew(t, copyGoldenVault(t))
	closeOnCleanup(t, m)
	res, err := m.Search(context.Background(), contracts.Query{Kinds: []contracts.NodeKind{contracts.KindDecision}})
	must(t, err)
	if len(res) == 0 {
		t.Fatalf("expected at least one decision node in the golden vault")
	}
}
