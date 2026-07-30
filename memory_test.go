package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestSearchHidesTranscriptUnlessIncludeRaw(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "projects/p/fact", Kind: contracts.KindProject, Title: "widget throughput", Body: "the widget runs at 5rps"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Record(ctx, contracts.Node{Key: "raw/s/1", Kind: contracts.KindTranscript, Title: "turn 1", Body: "user: how fast is the widget → assistant: 5rps"}); err != nil {
		t.Fatal(err)
	}

	// Default query: raw chunk hidden.
	got, err := m.Search(ctx, contracts.Query{Text: "widget"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if n.Kind == contracts.KindTranscript {
			t.Fatalf("default Search returned a raw transcript node %q; must be hidden", n.Key)
		}
	}

	// Sweep-shaped query (IncludeArchived true, IncludeRaw unset): still hidden.
	got, _ = m.Search(ctx, contracts.Query{Text: "widget", IncludeArchived: true})
	for _, n := range got {
		if n.Kind == contracts.KindTranscript {
			t.Fatalf("IncludeArchived Search leaked a raw node %q; sweep/merge/promote must not see raw", n.Key)
		}
	}

	// Opt-in: raw chunk visible.
	got, err = m.Search(ctx, contracts.Query{Text: "widget", IncludeRaw: true})
	if err != nil {
		t.Fatal(err)
	}
	var sawRaw bool
	for _, n := range got {
		if n.Key == "raw/s/1" {
			sawRaw = true
		}
	}
	if !sawRaw {
		t.Fatal("IncludeRaw Search did not return the raw transcript node")
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

func TestRecordIsAtomicNoTempLeftBehind(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "a/b", Kind: contracts.KindRepo, Title: "T"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "b.md.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind after Record: err=%v", err)
	}
	n, err := m.load("a/b")
	if err != nil || n.Title != "T" {
		t.Fatalf("node not committed cleanly: %+v err=%v", n, err)
	}
}

func TestVaultLockFileIsHiddenFromSearch(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, lockName)); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
	_ = m.Record(context.Background(), contracts.Node{Key: "x", Kind: contracts.KindProject, Body: "lock"})
	res, _ := m.Search(context.Background(), contracts.Query{Text: "lock"})
	for _, n := range res {
		if strings.Contains(n.Key, lockName) {
			t.Fatalf("Search surfaced the lock file: %+v", res)
		}
	}
}

func TestConcurrentLinksAllLand(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	_ = m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = m.Links(ctx, "a", fmt.Sprintf("b%d", i), "contains")
		}(i)
	}
	wg.Wait()
	n, _ := m.load("a")
	if len(n.Links) != 16 {
		t.Fatalf("want 16 edges after concurrent Links, got %d", len(n.Links))
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

func TestSearchMatchesDomainAsTag(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "projets/x/index", Kind: contracts.KindProject,
		Title: "X", Meta: map[string]string{"domain": "dev"}}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := m.Search(ctx, contracts.Query{Tags: []string{"dev"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Key != "projets/x/index" {
		t.Fatalf("domain tag search did not find node: %+v", got)
	}
}

func keysOfNodes(ns []contracts.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Key
	}
	return out
}

func TestRecord_StampsCapturedAtWhenAbsent(t *testing.T) {
	m := newTestMem(t)
	fixed := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "x"}); err != nil {
		t.Fatal(err)
	}
	sg, err := m.Recall(ctx, "facts/x", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := sg.Root.Meta["capturedAt"]; got != fixed.Format(time.RFC3339) {
		t.Fatalf("capturedAt: want %q, got %q", fixed.Format(time.RFC3339), got)
	}
}

func TestRecord_PreservesCapturedAtOnUpsert(t *testing.T) {
	m := newTestMem(t)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	m.now = func() time.Time { return t0 }
	if err := m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "v1"}); err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return t1 } // later re-record (upsert)
	if err := m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "v2"}); err != nil {
		t.Fatal(err)
	}
	sg, _ := m.Recall(ctx, "facts/x", 0)
	if got := sg.Root.Meta["capturedAt"]; got != t0.Format(time.RFC3339) {
		t.Fatalf("capturedAt must be preserved from first write %q, got %q", t0.Format(time.RFC3339), got)
	}
}

func TestRecord_KeepsCallerSuppliedCapturedAt(t *testing.T) {
	m := newTestMem(t)
	m.now = func() time.Time { return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	supplied := "2020-05-05T00:00:00Z"
	if err := m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "x", Meta: map[string]string{"capturedAt": supplied}}); err != nil {
		t.Fatal(err)
	}
	sg, _ := m.Recall(ctx, "facts/x", 0)
	if got := sg.Root.Meta["capturedAt"]; got != supplied {
		t.Fatalf("caller-supplied capturedAt must be kept: want %q, got %q", supplied, got)
	}
}

func TestSearch_RankedOrdersByRelevance(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	// Both contain the query substring "nats" (so both pass the matchesQuery gate);
	// only their placement differs, so ranking — not the gate — decides order.
	m.Record(ctx, contracts.Node{Key: "facts/title", Kind: contracts.KindDecision, Title: "nats transport choice", Body: "chosen for decoupling"})
	m.Record(ctx, contracts.Node{Key: "facts/body", Kind: contracts.KindDecision, Title: "logging note", Body: "we mention nats once here"})
	got, err := m.Search(ctx, contracts.Query{Text: "nats", Ranked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "facts/title" {
		t.Fatalf("ranked: title-hit node should lead; got %v", keysOfNodes(got))
	}
}

func TestSearch_RankedRespectsLimitAsTopK(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	// Both contain "nats"; facts/both scores higher (title hit + higher tf + durable
	// kind), so ranked+Limit:1 must return it alone.
	m.Record(ctx, contracts.Node{Key: "facts/both", Kind: contracts.KindDecision, Title: "nats transport", Body: "nats transport nats"})
	m.Record(ctx, contracts.Node{Key: "facts/one", Kind: contracts.KindSession, Title: "note", Body: "nats"})
	got, err := m.Search(ctx, contracts.Query{Text: "nats", Ranked: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "facts/both" {
		t.Fatalf("ranked+limit should return the single best; got %v", keysOfNodes(got))
	}
}

func TestSearch_UnrankedReturnsAllMatches(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindDecision, Title: "nats", Body: "x"})
	m.Record(ctx, contracts.Node{Key: "b", Kind: contracts.KindDecision, Title: "nats deep", Body: "nats nats"})
	// Ranked:false must preserve today's semantics: all matches, no top-K cut.
	got, err := m.Search(ctx, contracts.Query{Text: "nats"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("unranked should return all matches, got %d", len(got))
	}
}

func TestWriteNodeStampsAndBumpsLastSeen(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return t0 }

	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "n1", Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(ctx, "n1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root.Meta[contracts.MetaLastSeen] != t0.Format(time.RFC3339) {
		t.Fatalf("lastSeen = %q, want %q", got.Root.Meta[contracts.MetaLastSeen], t0.Format(time.RFC3339))
	}
	capturedAt := got.Root.Meta["capturedAt"]

	// Re-record at a later clock with no lastSeen supplied: lastSeen bumps,
	// capturedAt is preserved.
	t1 := t0.Add(48 * time.Hour)
	m.now = func() time.Time { return t1 }
	if err := m.Record(ctx, contracts.Node{Key: "n1", Body: "hello again"}); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Recall(ctx, "n1", 0)
	if got.Root.Meta[contracts.MetaLastSeen] != t1.Format(time.RFC3339) {
		t.Fatalf("lastSeen not bumped: %q", got.Root.Meta[contracts.MetaLastSeen])
	}
	if got.Root.Meta["capturedAt"] != capturedAt {
		t.Fatalf("capturedAt changed: %q want %q", got.Root.Meta["capturedAt"], capturedAt)
	}
}

func TestWriteNodeHonorsSuppliedLastSeen(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }
	supplied := "2020-01-02T03:04:05Z"
	if err := m.Record(context.Background(), contracts.Node{
		Key: "n2", Body: "x", Meta: map[string]string{contracts.MetaLastSeen: supplied},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Recall(context.Background(), "n2", 0)
	if got.Root.Meta[contracts.MetaLastSeen] != supplied {
		t.Fatalf("supplied lastSeen not honored: %q", got.Root.Meta[contracts.MetaLastSeen])
	}
}

func TestArchivedExclusion(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	archived := map[string]string{contracts.MetaState: contracts.StateArchived}

	if err := m.Record(ctx, contracts.Node{Key: "root", Body: "keyword root",
		Links: []contracts.Link{{To: "old", Rel: "rel"}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Record(ctx, contracts.Node{Key: "old", Body: "keyword old", Meta: archived}); err != nil {
		t.Fatal(err)
	}

	// Search hides archived by default...
	hits, err := m.Search(ctx, contracts.Query{Text: "keyword"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range hits {
		if n.Key == "old" {
			t.Fatalf("archived node returned by default Search")
		}
	}
	// ...but IncludeArchived reaches it.
	hits, _ = m.Search(ctx, contracts.Query{Text: "keyword", IncludeArchived: true})
	found := false
	for _, n := range hits {
		if n.Key == "old" {
			found = true
		}
	}
	if !found {
		t.Fatalf("archived node missing with IncludeArchived=true")
	}

	// Recall from active root: archived neighbor is hidden.
	sg, err := m.Recall(ctx, "root", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range sg.Nodes {
		if n.Key == "old" {
			t.Fatalf("archived neighbor leaked into Recall")
		}
	}

	// Recall of an archived key directly: root still returned.
	sg, err = m.Recall(ctx, "old", 0)
	if err != nil {
		t.Fatal(err)
	}
	if sg.Root.Key != "old" {
		t.Fatalf("archived root not returned by direct Recall: %q", sg.Root.Key)
	}
}
