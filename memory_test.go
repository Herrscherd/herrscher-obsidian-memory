package obsidian

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func newTestMem(t *testing.T) *Memory {
	t.Helper()
	m, err := New(t.TempDir())
	must(t, err)
	closeOnCleanup(t, m)
	return m
}

func TestUnlinkRemovesEdgeAndPreservesProse(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)

	code := "    x  =  1  # two-spaced code must survive"
	body := "See [[proj/b|decided-in]] for context and keep [[proj/c|see-also]] too.\n" +
		code + "\n" +
		"[[proj/b|lead]] starts this line.\n" +
		"\n## Liens\n- [[proj/b|merged-into]]\n"
	if err := m.Record(ctx, contracts.Node{Key: "proj/a", Kind: contracts.KindProject, Title: "A", Body: body}); err != nil {
		t.Fatal(err)
	}

	if err := m.Unlink(ctx, "proj/a", "proj/b"); err != nil {
		t.Fatal(err)
	}

	sg, err := m.Recall(ctx, "proj/a", 0)
	if err != nil {
		t.Fatal(err)
	}
	foundC := false
	for _, l := range sg.Root.Links {
		if l.To == "proj/b" {
			t.Fatalf("edge to proj/b still present after Unlink: %+v", sg.Root.Links)
		}
		if l.To == "proj/c" {
			foundC = true
		}
	}
	if !foundC {
		t.Fatalf("Unlink dropped the unrelated edge to proj/c: %+v", sg.Root.Links)
	}
	if !strings.Contains(sg.Root.Body, "for context and keep") {
		t.Fatalf("Unlink mangled surrounding prose: %q", sg.Root.Body)
	}
	if strings.Contains(sg.Root.Body, "[[proj/b") {
		t.Fatalf("Unlink left a [[proj/b...]] token in the body: %q", sg.Root.Body)
	}
	if !strings.Contains(sg.Root.Body, code) {
		t.Fatalf("Unlink mutated an unrelated line; want %q intact in %q", code, sg.Root.Body)
	}
	if !strings.Contains(sg.Root.Body, "\nstarts this line.") {
		t.Fatalf("Unlink left an orphan leading space on the line-leading token: %q", sg.Root.Body)
	}

	if err := m.Unlink(ctx, "proj/a", "proj/b"); err != nil {
		t.Fatalf("second Unlink should be a no-op, got %v", err)
	}
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
	must(t, m.Record(ctx, contracts.Node{Key: "p/index", Kind: contracts.KindProject, Title: "Old"}))
	must(t, m.Record(ctx, contracts.Node{Key: "p/index", Kind: contracts.KindProject, Title: "New"}))
	sg, err := m.Recall(ctx, "p/index", 0)
	must(t, err)
	if sg.Root.Title != "New" {
		t.Fatalf("upsert did not update in place: %+v", sg.Root)
	}
}

func TestRecallFollowsLinksToDepth(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	must(t, m.Record(ctx, contracts.Node{
		Key: "a", Kind: contracts.KindProject,
		Links: []contracts.Link{{To: "b", Rel: "depends-on"}},
	}))
	must(t, m.Record(ctx, contracts.Node{
		Key: "b", Kind: contracts.KindRepo,
		Links: []contracts.Link{{To: "c", Rel: relContains}},
	}))
	must(t, m.Record(ctx, contracts.Node{Key: "c", Kind: contracts.KindRepo}))

	sg, err := m.Recall(ctx, "a", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	keys := make(map[string]bool)
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
	must(t, m.Record(ctx, contracts.Node{
		Key: "a/index", Kind: contracts.KindProject,
		Title: "Alpha", Body: "uses NATS", Meta: map[string]string{"tags": "platform, go"},
	}))
	must(t, m.Record(ctx, contracts.Node{
		Key: "dec/x", Kind: contracts.KindDecision,
		Title: "Choose NATS", Body: "transport choice",
	}))

	byKind, err := m.Search(ctx, contracts.Query{Kinds: []contracts.NodeKind{contracts.KindDecision}})

	must(t, err)
	if len(byKind) != 1 || byKind[0].Key != "dec/x" {
		t.Fatalf("kind filter wrong: %+v", byKind)
	}
	byText, err := m.Search(ctx, contracts.Query{Text: "nats"})
	must(t, err)
	if len(byText) != 2 {
		t.Fatalf("text filter expected 2, got %d", len(byText))
	}
	byTag, err := m.Search(ctx, contracts.Query{Tags: []string{"go"}})
	must(t, err)
	if len(byTag) != 1 || byTag[0].Key != "a/index" {
		t.Fatalf("tag filter wrong: %+v", byTag)
	}
	lim, err := m.Search(ctx, contracts.Query{Text: "nats", Limit: 1})
	must(t, err)
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

	got, err := m.Search(ctx, contracts.Query{Text: "widget"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if n.Kind == contracts.KindTranscript {
			t.Fatalf("default Search returned a raw transcript node %q; must be hidden", n.Key)
		}
	}

	got, err = m.Search(ctx, contracts.Query{Text: "widget", IncludeArchived: true})

	must(t, err)
	for _, n := range got {
		if n.Kind == contracts.KindTranscript {
			t.Fatalf("IncludeArchived Search leaked a raw node %q; sweep/merge/promote must not see raw", n.Key)
		}
	}

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
	must(t, m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject}))
	must(t, m.Record(ctx, contracts.Node{Key: "b", Kind: contracts.KindRepo}))

	if err := m.Links(ctx, "a", "b", relContains); err != nil {
		t.Fatalf("Links: %v", err)
	}
	sg, err := m.Recall(ctx, "a", 1)
	must(t, err)
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
	must(t, m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject}))
	must(t, m.Links(ctx, "a", "b", relContains))
	must(t, m.Links(ctx, "a", "b", relContains))
	n, err := m.load("a")
	must(t, err)
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
	must(t, m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject}))
	must(t, m.Links(ctx, "a", "b", "depends-on"))
	must(t, m.Links(ctx, "a", "b", relContains))
	n, err := m.load("a")
	must(t, err)
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
	if err := m.Links(ctx, "a", "b", relContains); err == nil {
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
	if err = m.Record(ctx, contracts.Node{Key: "a/b", Kind: contracts.KindRepo, Title: "T"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "a", "b.md.tmp")); !os.IsNotExist(err) {
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
	if _, err = os.Stat(filepath.Join(dir, lockName)); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
	must(t, m.Record(context.Background(), contracts.Node{Key: "x", Kind: contracts.KindProject, Body: "lock"}))
	res, err := m.Search(context.Background(), contracts.Query{Text: "lock"})
	must(t, err)
	for _, n := range res {
		if strings.Contains(n.Key, lockName) {
			t.Fatalf("Search surfaced the lock file: %+v", res)
		}
	}
}

func TestConcurrentLinksAllLand(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	must(t, m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindProject}))
	var wg sync.WaitGroup
	errs := make([]error, 16)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = m.Links(ctx, "a", fmt.Sprintf("b%d", i), relContains)
		}(i)
	}
	wg.Wait()
	must(t, errors.Join(errs...))
	n, err := m.load("a")
	must(t, err)
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
	must(t, m.Record(ctx, contracts.Node{Key: "real", Kind: contracts.KindProject, Body: "inside"}))

	secret := filepath.Join(t.TempDir(), "secret.md")
	if err = os.WriteFile(secret, []byte("---\ntype: user\n---\nTOP SECRET\n"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err = os.Symlink(secret, filepath.Join(dir, "leak.md")); err != nil {
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
	if err := m.Record(ctx, contracts.Node{
		Key: "projets/x/index", Kind: contracts.KindProject,
		Title: "X", Meta: map[string]string{"domain": "dev"},
	}); err != nil {
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
	m.now = func() time.Time { return t1 }
	if err := m.Record(ctx, contracts.Node{Key: "facts/x", Kind: contracts.KindDecision, Title: "v2"}); err != nil {
		t.Fatal(err)
	}
	sg, err := m.Recall(ctx, "facts/x", 0)
	must(t, err)
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
	sg, err := m.Recall(ctx, "facts/x", 0)
	must(t, err)
	if got := sg.Root.Meta["capturedAt"]; got != supplied {
		t.Fatalf("caller-supplied capturedAt must be kept: want %q, got %q", supplied, got)
	}
}

func TestSearch_RankedOrdersByRelevance(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	must(t, m.Record(ctx, contracts.Node{Key: "facts/title", Kind: contracts.KindDecision, Title: "nats transport choice", Body: "chosen for decoupling"}))
	must(t, m.Record(ctx, contracts.Node{Key: "facts/body", Kind: contracts.KindDecision, Title: "logging note", Body: "we mention nats once here"}))
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
	must(t, m.Record(ctx, contracts.Node{Key: "facts/both", Kind: contracts.KindDecision, Title: "nats transport", Body: "nats transport nats"}))
	must(t, m.Record(ctx, contracts.Node{Key: "facts/one", Kind: contracts.KindSession, Title: "note", Body: "nats"}))
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
	must(t, m.Record(ctx, contracts.Node{Key: "a", Kind: contracts.KindDecision, Title: "nats", Body: "x"}))
	must(t, m.Record(ctx, contracts.Node{Key: "b", Kind: contracts.KindDecision, Title: "nats deep", Body: "deep dive"}))
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
	if err = m.Record(ctx, contracts.Node{Key: "n1", Body: "hello"}); err != nil {
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

	t1 := t0.Add(48 * time.Hour)
	m.now = func() time.Time { return t1 }
	if err = m.Record(ctx, contracts.Node{Key: "n1", Body: "hello again"}); err != nil {
		t.Fatal(err)
	}
	got, err = m.Recall(ctx, "n1", 0)
	must(t, err)
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
	if err = m.Record(context.Background(), contracts.Node{
		Key: "n2", Body: "x", Meta: map[string]string{contracts.MetaLastSeen: supplied},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(context.Background(), "n2", 0)
	must(t, err)
	if got.Root.Meta[contracts.MetaLastSeen] != supplied {
		t.Fatalf("supplied lastSeen not honored: %q", got.Root.Meta[contracts.MetaLastSeen])
	}
}

func TestArchivedExclusion(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		keys func(context.Context, *Memory) ([]string, error)
		want []string
	}{
		{
			name: "default Search hides the archived node",
			keys: func(ctx context.Context, m *Memory) ([]string, error) {
				hits, err := m.Search(ctx, contracts.Query{Text: "keyword"})
				return keysOfNodes(hits), err
			},
			want: []string{"root"},
		},
		{
			name: "IncludeArchived Search returns the archived node",
			keys: func(ctx context.Context, m *Memory) ([]string, error) {
				hits, err := m.Search(ctx, contracts.Query{Text: "keyword", IncludeArchived: true})
				return keysOfNodes(hits), err
			},
			want: []string{"old", "root"},
		},
		{
			name: "Recall from the root leaves the archived neighbor out",
			keys: func(ctx context.Context, m *Memory) ([]string, error) {
				sg, err := m.Recall(ctx, "root", 1)
				return keysOfNodes(sg.Nodes), err
			},
			want: []string{},
		},
		{
			name: "direct Recall returns the archived root",
			keys: func(ctx context.Context, m *Memory) ([]string, error) {
				sg, err := m.Recall(ctx, "old", 0)
				return []string{sg.Root.Key}, err
			},
			want: []string{"old"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := archivedFixture(ctx, t)
			keys, err := c.keys(ctx, m)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, c.want) {
				t.Fatalf("keys = %q, want %q", keys, c.want)
			}
		})
	}
}

func archivedFixture(ctx context.Context, t *testing.T) *Memory {
	t.Helper()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archived := map[string]string{contracts.MetaState: contracts.StateArchived}
	must(t, m.Record(ctx, contracts.Node{
		Key: "root", Body: "keyword root",
		Links: []contracts.Link{{To: "old", Rel: "rel"}},
	}))
	must(t, m.Record(ctx, contracts.Node{Key: "old", Body: "keyword old", Meta: archived}))
	return m
}

func TestRecallHidesTranscriptNeighbor(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = m.Record(ctx, contracts.Node{
		Key:   "root",
		Links: []contracts.Link{{To: "raw/s/e-1", Rel: "captured"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err = m.Record(ctx, contracts.Node{
		Key: "raw/s/e-1", Kind: contracts.KindTranscript,
		Body: "attacker-controlled transcript body",
	}); err != nil {
		t.Fatal(err)
	}
	sg, err := m.Recall(ctx, "root", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range sg.Nodes {
		if n.Kind == contracts.KindTranscript {
			t.Fatalf("raw transcript neighbor leaked into Recall: %q", n.Key)
		}
	}
}

func TestSearchSeesSameSizeRewriteWithRestoredMtime(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)
	must(t, m.Record(ctx, contracts.Node{Key: "a/one", Kind: contracts.KindDecision, Body: "alpha"}))
	path := filepath.Join(m.root.Name(), "a", "one.md")
	before, err := os.Stat(path)
	must(t, err)
	if _, err = m.Search(ctx, contracts.Query{Text: "alpha"}); err != nil {
		t.Fatal(err)
	}
	must(t, m.Record(ctx, contracts.Node{Key: "a/one", Kind: contracts.KindDecision, Body: "bravo"}))
	must(t, os.Chtimes(path, before.ModTime(), before.ModTime()))
	got, err := m.Search(ctx, contracts.Query{Text: "bravo"})
	must(t, err)
	if len(got) != 1 || got[0].Body != "bravo" {
		t.Fatalf("Search served the stale parse: %+v", got)
	}
}

func TestRecordLeavesNoTempFile(t *testing.T) {
	m := newTestMem(t)
	must(t, m.Record(context.Background(), contracts.Node{Key: "a/one", Kind: contracts.KindDecision, Body: "kept"}))
	if _, err := os.Stat(filepath.Join(m.root.Name(), "a", "one.md.tmp")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temp file left behind: %v", err)
	}
	n, err := m.load("a/one")
	must(t, err)
	if n.Body != "kept" {
		t.Fatalf("body: got %q", n.Body)
	}
}

func TestErrorsQuoteTheKey(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)
	must(t, os.WriteFile(filepath.Join(m.root.Name(), "blocker"), nil, privateFileMode))
	_, err := m.load("a/absent")
	requireQuotedKey(t, err, "a/absent")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing note: want fs.ErrNotExist, got %v", err)
	}
	err = m.Record(ctx, contracts.Node{Key: "a/one", Kind: contracts.KindDecision, Links: []contracts.Link{{To: "a/two", Rel: "x]y"}}})
	requireQuotedKey(t, err, "a/one")
	if !errors.Is(err, ErrInvalidLink) {
		t.Fatalf("bad relation: want ErrInvalidLink, got %v", err)
	}
	err = m.Record(ctx, contracts.Node{Key: "blocker/one", Kind: contracts.KindDecision})
	requireQuotedKey(t, err, "blocker/one")
}

func requireQuotedKey(t *testing.T, err error, key string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error naming %q", key)
	}
	if !strings.Contains(err.Error(), strconv.Quote(key)) {
		t.Fatalf("error does not quote the key %q: %v", key, err)
	}
}

func TestRecordRejectsKindThatBreaksFrontmatter(t *testing.T) {
	for _, kind := range []contracts.NodeKind{"decision\ntitle: hijacked", "decision\rtype: user"} {
		t.Run(strconv.Quote(string(kind)), func(t *testing.T) {
			m := newTestMem(t)
			err := m.Record(context.Background(), contracts.Node{Key: "a/one", Kind: kind, Title: "Kept"})
			if !errors.Is(err, ErrInvalidKind) {
				t.Fatalf("want ErrInvalidKind, got %v", err)
			}
			if _, err = m.load("a/one"); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("refused kind still wrote a note: %v", err)
			}
		})
	}
}

func TestExciseWikilinksKeepsOtherLinks(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"inline":      {"see [[a]] and [[to|rel]] now", "see [[a]] and now"},
		"bullet only": {"## Liens\n- [[to]]\n- [[b]]", "## Liens\n- [[b]]"},
		"alias only":  {"[[x]] then [[to|rel]]", "[[x]] then"},
		"prefix name": {"[[toast]] and [[to]]", "[[toast]] and"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := exciseWikilinks(c.body, "to"); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
