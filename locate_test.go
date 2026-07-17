package obsidian

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestLocateExistingNode(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Record(context.Background(), contracts.Node{Key: "acme/proj/fact", Kind: contracts.KindDecision, Title: "F"}); err != nil {
		t.Fatal(err)
	}
	loc, err := m.Locate(context.Background(), "acme/proj/fact")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loc.File, "file://") || !strings.HasSuffix(loc.File, "/acme/proj/fact.md") {
		t.Fatalf("File = %q", loc.File)
	}
	if !strings.Contains(loc.Obsidian, "obsidian://open?vault=") || !strings.Contains(loc.Obsidian, "file=acme%2Fproj%2Ffact") {
		t.Fatalf("Obsidian = %q", loc.Obsidian)
	}
}

func TestLocateMissingNode(t *testing.T) {
	m, _ := New(t.TempDir())
	defer m.Close()
	if _, err := m.Locate(context.Background(), "nope"); err == nil {
		t.Fatal("expected error for missing node")
	}
}

func TestLocateUnsafeKey(t *testing.T) {
	m, _ := New(t.TempDir())
	defer m.Close()
	if _, err := m.Locate(context.Background(), "../escape"); err == nil {
		t.Fatal("expected error for unsafe key")
	}
}

func TestLocateEncodesSpacesInPath(t *testing.T) {
	dir := t.TempDir() + "/My Vault"
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Record(context.Background(), contracts.Node{Key: "a/b", Kind: contracts.KindDecision}); err != nil {
		t.Fatal(err)
	}
	loc, err := m.Locate(context.Background(), "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(loc.File, " ") {
		t.Fatalf("file URI must be encoded, got %q", loc.File)
	}
	if !strings.HasPrefix(loc.File, "file://") || !strings.HasSuffix(loc.File, "/a/b.md") {
		t.Fatalf("file URI = %q", loc.File)
	}
}
