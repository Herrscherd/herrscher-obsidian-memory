package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func newTestMem(t *testing.T) *ObsidianMemory {
	t.Helper()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestRecordThenRecall(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "p/index", Kind: contracts.KindProject, Title: "P"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	sg, err := m.Recall(ctx, "p/index", 0)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if sg.Root.Title != "P" || sg.Root.Kind != contracts.KindProject {
		t.Fatalf("recalled node wrong: %+v", sg.Root)
	}
}

func TestRecordUpsertsNoDuplicate(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "p/index", Kind: contracts.KindProject, Title: "Old"})
	_ = m.Record(ctx, contracts.Node{Key: "p/index", Kind: contracts.KindProject, Title: "New"})
	sg, _ := m.Recall(ctx, "p/index", 0)
	if sg.Root.Title != "New" {
		t.Fatalf("upsert did not update in place: %+v", sg.Root)
	}
}

func TestRecallFollowsLinksToDepth(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject,
		Links: []contracts.Link{{To: "b", Rel: "depends-on"}}})
	_ = m.Record(ctx, contracts.Node{Key: "b", Kind: contracts.KindRepo,
		Links: []contracts.Link{{To: "c", Rel: "contains"}}})
	_ = m.Record(ctx, contracts.Node{Key: "c", Kind: contracts.KindRepo})

	sg, err := m.Recall(ctx, "a", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	keys := map[string]bool{}
	for _, n := range sg.Nodes {
		keys[n.Key] = true
	}
	if !keys["b"] || keys["c"] {
		t.Fatalf("depth-1 should include b but not c; got %v", keys)
	}
	if len(sg.Edges) == 0 {
		t.Fatalf("edges should be populated")
	}
}

func TestRecallMissingKeyErrors(t *testing.T) {
	m := newTestMem(t)
	if _, err := m.Recall(context.Background(), "nope", 0); err == nil {
		t.Fatalf("expected error recalling missing key")
	}
}

func TestSearchByKindTagText(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a/index", Kind: contracts.KindProject,
		Title: "Alpha", Body: "uses NATS", Meta: map[string]string{"tags": "platform, go"}})
	_ = m.Record(ctx, contracts.Node{Key: "dec/x", Kind: contracts.KindDecision,
		Title: "Choose NATS", Body: "transport choice"})

	byKind, _ := m.Search(ctx, contracts.Query{Kinds: []contracts.NodeKind{contracts.KindDecision}})
	if len(byKind) != 1 || byKind[0].Key != "dec/x" {
		t.Fatalf("kind filter wrong: %+v", byKind)
	}
	byText, _ := m.Search(ctx, contracts.Query{Text: "nats"})
	if len(byText) != 2 {
		t.Fatalf("text filter expected 2, got %d", len(byText))
	}
	byTag, _ := m.Search(ctx, contracts.Query{Tags: []string{"go"}})
	if len(byTag) != 1 || byTag[0].Key != "a/index" {
		t.Fatalf("tag filter wrong: %+v", byTag)
	}
	lim, _ := m.Search(ctx, contracts.Query{Text: "nats", Limit: 1})
	if len(lim) != 1 {
		t.Fatalf("limit not honored: %d", len(lim))
	}
}

func TestLinksCreatesEdgeVisibleToRecall(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject})
	_ = m.Record(ctx, contracts.Node{Key: "b", Kind: contracts.KindRepo})

	if err := m.Links(ctx, "a", "b", "contains"); err != nil {
		t.Fatalf("Links: %v", err)
	}
	sg, _ := m.Recall(ctx, "a", 1)
	found := false
	for _, n := range sg.Nodes {
		if n.Key == "b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("edge a→b not visible via Recall: %+v", sg)
	}
}

func TestLinksIsIdempotent(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject})
	_ = m.Links(ctx, "a", "b", "contains")
	_ = m.Links(ctx, "a", "b", "contains")
	n, _ := m.load("a")
	count := 0
	for _, l := range n.Links {
		if l.To == "b" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate edge created: %d", count)
	}
}

func TestLinksKeepsExistingRelOnTarget(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject})
	_ = m.Links(ctx, "a", "b", "depends-on")
	_ = m.Links(ctx, "a", "b", "contains") // same target — idempotent, no rewrite
	n, _ := m.load("a")
	if len(n.Links) != 1 || n.Links[0].To != "b" || n.Links[0].Rel != "depends-on" {
		t.Fatalf("want one b/depends-on edge unchanged: %+v", n.Links)
	}
}

func TestCancelledContextIsHonored(t *testing.T) {
	m := newTestMem(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject}); err == nil {
		t.Fatalf("Record should respect a cancelled context")
	}
	if _, err := m.Search(ctx, contracts.Query{}); err == nil {
		t.Fatalf("Search should respect a cancelled context")
	}
	if err := m.Links(ctx, "a", "b", "contains"); err == nil {
		t.Fatalf("Links should respect a cancelled context")
	}
}

func TestSearchIgnoresSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "real", Kind: contracts.KindProject, Body: "inside"})

	secret := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(secret, []byte("---\ntype: user\n---\nTOP SECRET\n"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leak.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	res, err := m.Search(ctx, contracts.Query{Text: "secret"})
	if err != nil {
		t.Fatalf("Search errored on a symlinked vault: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("Search read through a symlink escaping the vault: %+v", res)
	}
}
