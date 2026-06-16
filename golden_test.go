package obsidian

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestGoldenVaultRecallTraverses(t *testing.T) {
	m, err := New("examples/vault")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sg, err := m.Recall(context.Background(), "herrscher/herrscher/index", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if sg.Root.Kind != contracts.KindProject {
		t.Fatalf("root should be a project, got %s", sg.Root.Kind)
	}
	reached := map[string]bool{}
	for _, n := range sg.Nodes {
		reached[n.Key] = true
	}
	if !reached["herrscher/herrscher/repos/contracts"] {
		t.Fatalf("project does not reach its contracts repo: %v", reached)
	}
}

func TestGoldenVaultSearchFindsDecision(t *testing.T) {
	m, _ := New("examples/vault")
	res, _ := m.Search(context.Background(), contracts.Query{Kinds: []contracts.NodeKind{contracts.KindDecision}})
	if len(res) == 0 {
		t.Fatalf("expected at least one decision node in the golden vault")
	}
}
