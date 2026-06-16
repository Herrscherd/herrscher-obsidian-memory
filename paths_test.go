package obsidian

import (
	"path/filepath"
	"testing"
)

func TestKeyToPathAndBack(t *testing.T) {
	root := "/tmp/vault"
	got := keyToPath(root, "herrscher/repos/contracts")
	want := filepath.Join(root, "herrscher", "repos", "contracts.md")
	if got != want {
		t.Fatalf("keyToPath = %q, want %q", got, want)
	}
	if k := pathToKey(root, want); k != "herrscher/repos/contracts" {
		t.Fatalf("pathToKey = %q, want %q", k, "herrscher/repos/contracts")
	}
}

func TestPathToKeyRejectsNonMarkdown(t *testing.T) {
	if k := pathToKey("/tmp/vault", "/tmp/vault/notes/x.txt"); k != "" {
		t.Fatalf("pathToKey on non-.md should be empty, got %q", k)
	}
}
