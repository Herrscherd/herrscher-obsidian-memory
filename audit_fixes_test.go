package obsidian

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestSearchResultsDoNotAliasParseCache(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)
	if err := m.Record(ctx, contracts.Node{Key: "a/one", Kind: contracts.KindDecision, Title: "One", Body: "nats here"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cold", "warm"} {
		got, err := m.Search(ctx, contracts.Query{Text: "nats"})
		if err != nil {
			t.Fatalf("%s search: %v", name, err)
		}
		if len(got) != 1 {
			t.Fatalf("%s: want 1 node, got %d", name, len(got))
		}
		got[0].Meta[contracts.MetaState] = contracts.StateArchived
		got[0].Links = append(got[0].Links, contracts.Link{To: "a/two"})
	}
	after, err := m.Search(ctx, contracts.Query{Text: "nats"})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("cache poisoned: want 1 node, got %d", len(after))
	}
	if s := after[0].Meta[contracts.MetaState]; s != "" {
		t.Fatalf("cached node mutated: state = %q", s)
	}
	if len(after[0].Links) != 0 {
		t.Fatalf("cached node links mutated: %v", after[0].Links)
	}
}

func TestRecordDoesNotMutateCallerMeta(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time {
		clock = clock.Add(time.Hour)
		return clock
	}
	shared := map[string]string{"domain": "acme"}
	for _, key := range []string{"a/one", "a/two"} {
		if err := m.Record(ctx, contracts.Node{Key: key, Kind: contracts.KindDecision, Meta: shared}); err != nil {
			t.Fatalf("record %s: %v", key, err)
		}
	}
	if len(shared) != 1 {
		t.Fatalf("caller meta mutated: %v", shared)
	}
	one, err := m.load("a/one")
	if err != nil {
		t.Fatal(err)
	}
	two, err := m.load("a/two")
	if err != nil {
		t.Fatal(err)
	}
	if one.Meta["capturedAt"] == two.Meta["capturedAt"] {
		t.Fatalf("capturedAt leaked between records: %q", one.Meta["capturedAt"])
	}
}

func TestRecallSurfacesNonNotExistNeighborError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	ctx := context.Background()
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Record(ctx, contracts.Node{Key: "a/one", Kind: contracts.KindDecision, Links: []contracts.Link{{To: "a/two", Rel: "sees"}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Record(ctx, contracts.Node{Key: "a/two", Kind: contracts.KindDecision}); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dir, "a", "two.md")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(unreadable, 0o600)
	if _, err := m.Recall(ctx, "a/one", 1); err == nil {
		t.Fatal("Recall swallowed an unreadable neighbor")
	}

	if err := os.Remove(unreadable); err != nil {
		t.Fatal(err)
	}
	sg, err := m.Recall(ctx, "a/one", 1)
	if err != nil {
		t.Fatalf("dangling link must stay skippable: %v", err)
	}
	if len(sg.Nodes) != 0 {
		t.Fatalf("want no neighbors, got %d", len(sg.Nodes))
	}
}

func TestSearchRankedFallsBackToRecencyOnPartialTermMatch(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	cases := []struct {
		key        string
		capturedAt string
	}{
		{"a/oldest", "2026-01-01T00:00:00Z"},
		{"b/newest", "2026-05-30T00:00:00Z"},
		{"c/middle", "2026-03-01T00:00:00Z"},
	}
	for _, c := range cases {
		n := contracts.Node{Key: c.key, Kind: contracts.KindDecision, Body: "neublock migration", Meta: map[string]string{"capturedAt": c.capturedAt}}
		if err := m.Record(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	got, err := m.Search(ctx, contracts.Query{Text: "neubl", Ranked: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b/newest", "c/middle", "a/oldest"}
	if len(got) != len(want) {
		t.Fatalf("want %d nodes, got %d", len(want), len(got))
	}
	for i, k := range want {
		if got[i].Key != k {
			t.Fatalf("position %d: want %q, got %q", i, k, got[i].Key)
		}
	}
}

func TestSearchRankedKeepsTextHitsAboveFallback(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	if err := m.Record(ctx, contracts.Node{Key: "a/partial", Kind: contracts.KindDecision, Body: "neublock", Meta: map[string]string{"capturedAt": "2026-05-31T00:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Record(ctx, contracts.Node{Key: "b/exact", Kind: contracts.KindDecision, Body: "neubl", Meta: map[string]string{"capturedAt": "2020-01-01T00:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Search(ctx, contracts.Query{Text: "neubl", Ranked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "b/exact" {
		t.Fatalf("token hit must rank first, got %v", keysOf(got))
	}
}

func keysOf(ns []contracts.Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Key)
	}
	return out
}

func TestWritesFailWhenLockCannotBeTaken(t *testing.T) {
	m := newTestMem(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	peer, err := New(m.root.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	release, err := peer.flock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	err = m.Record(ctx, contracts.Node{Key: "a/one", Kind: contracts.KindDecision})
	if err == nil {
		t.Fatal("Record proceeded without the cross-process lock")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestVaultAndNotesArePrivate(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "vault")
	m, err := EnsureVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Record(ctx, contracts.Node{Key: "a/one", Kind: contracts.KindDecision}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		want os.FileMode
	}{
		{dir, 0o700},
		{filepath.Join(dir, lockName), 0o600},
		{filepath.Join(dir, "a"), 0o700},
		{filepath.Join(dir, "a", "one.md"), 0o600},
		{filepath.Join(dir, ".obsidian"), 0o700},
		{filepath.Join(dir, ".obsidian", "app.json"), 0o600},
	}
	for _, c := range cases {
		info, err := os.Stat(c.path)
		if err != nil {
			t.Fatalf("stat %s: %v", c.path, err)
		}
		if got := info.Mode().Perm(); got != c.want {
			t.Errorf("%s: mode %o, want %o", c.path, got, c.want)
		}
	}
}

func TestFrontmatterKeyWithColonCannotBreakOut(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]string
	}{
		{"shadows type", map[string]string{"type:x": "v"}},
		{"shadows title", map[string]string{"title:x": "v"}},
		{"plain colon key", map[string]string{"url:host": "example.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := contracts.Node{Key: "a/one", Kind: contracts.KindDecision, Title: "Kept", Meta: c.meta}
			back := unmarshalNode("a/one", []byte(marshalNode(n)))
			if back.Kind != contracts.KindDecision {
				t.Errorf("Kind hijacked: %q", back.Kind)
			}
			if back.Title != "Kept" {
				t.Errorf("Title hijacked: %q", back.Title)
			}
			for k, v := range back.Meta {
				if strings.Contains(v, ": ") {
					t.Errorf("meta %q holds a smuggled key: %q", k, v)
				}
			}
		})
	}
}

func TestSearchPrunesCacheEntriesForVanishedFiles(t *testing.T) {
	ctx := context.Background()
	m := newTestMem(t)
	if err := m.Record(ctx, contracts.Node{Key: "a/one", Kind: contracts.KindDecision, Body: "nats"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Search(ctx, contracts.Query{Text: "nats"}); err != nil {
		t.Fatal(err)
	}
	if len(m.parseCache) != 1 {
		t.Fatalf("want 1 cached node, got %d", len(m.parseCache))
	}
	if err := os.Remove(filepath.Join(m.root.Name(), "a", "one.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Search(ctx, contracts.Query{Text: "nats"}); err != nil {
		t.Fatal(err)
	}
	if len(m.parseCache) != 0 {
		t.Fatalf("stale cache entries kept: %v", m.parseCache)
	}
}

func TestLocateReturnsAbsoluteFileURIFromRelativeRoot(t *testing.T) {
	ctx := context.Background()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	defer os.Chdir(wd)
	m, err := New("memory")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Record(ctx, contracts.Node{Key: "acme/fact", Kind: contracts.KindDecision}); err != nil {
		t.Fatal(err)
	}
	loc, err := m.Locate(ctx, "acme/fact")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loc.File, "file:///") {
		t.Fatalf("want an absolute file URI, got %q", loc.File)
	}
}
