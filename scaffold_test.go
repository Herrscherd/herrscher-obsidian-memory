package obsidian

import (
	"context"
	"os"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestInitScaffoldsHierarchy(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{
		Org:     "herrscher",
		Project: "herrscher",
		Repos:   []string{"contracts", "gateway"},
		Servers: []string{"vps-1"},
	}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("Init: %v", err)
	}

	proj, err := m.load("herrscher/herrscher/index")
	if err != nil {
		t.Fatalf("project node missing: %v", err)
	}
	if proj.Kind != contracts.KindProject {
		t.Fatalf("project kind wrong: %s", proj.Kind)
	}
	if _, err := m.load("herrscher/herrscher/repos/contracts"); err != nil {
		t.Fatalf("repo node missing: %v", err)
	}
	if _, err := m.load("herrscher/herrscher/servers/vps-1"); err != nil {
		t.Fatalf("server node missing: %v", err)
	}
	if _, err := m.load("herrscher/index"); err != nil {
		t.Fatalf("org node missing: %v", err)
	}
	if _, err := m.load("herrscher/herrscher/architecture"); err != nil {
		t.Fatalf("architecture doc missing: %v", err)
	}
}

func TestInitIsIdempotentAndNeverOverwrites(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{Project: "solo", Repos: []string{"solo"}}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	path := keyToPath(m.root, "projets/solo/index")
	if err := os.WriteFile(path, []byte("---\ntype: project\n---\nHUMAN EDIT\n"), 0o644); err != nil {
		t.Fatalf("hand edit: %v", err)
	}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) == "" || !contains(string(got), "HUMAN EDIT") {
		t.Fatalf("Init overwrote a human-edited file: %q", string(got))
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
