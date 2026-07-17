package obsidian

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestDeleteRemovesNode(t *testing.T) {
	m, _ := New(t.TempDir())
	defer m.Close()
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "a/b", Kind: contracts.KindDecision}); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "a/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Locate(ctx, "a/b"); err == nil {
		t.Fatal("node should be gone after Delete")
	}
}

func TestDeleteIdempotent(t *testing.T) {
	m, _ := New(t.TempDir())
	defer m.Close()
	if err := m.Delete(context.Background(), "never/existed"); err != nil {
		t.Fatalf("Delete of absent key must be nil, got %v", err)
	}
}
