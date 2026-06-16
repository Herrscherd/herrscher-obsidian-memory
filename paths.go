// Package obsidian implements the contracts.Memory port over an Obsidian-style
// markdown vault: one node per .md file, frontmatter for Meta, [[wikilinks]] for
// Links. The vault is a git-versioned folder; Obsidian is the human UI over it.
package obsidian

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validKey rejects keys that would escape the vault root or are malformed: empty,
// absolute, or containing "." / ".." / empty path segments.
func validKey(key string) error {
	if key == "" {
		return fmt.Errorf("obsidian: empty key")
	}
	if filepath.IsAbs(key) {
		return fmt.Errorf("obsidian: absolute key %q", key)
	}
	for _, p := range strings.Split(key, "/") {
		if p == "" || p == "." || p == ".." {
			return fmt.Errorf("obsidian: unsafe key %q", key)
		}
	}
	return nil
}

// keyToPath maps a vault-relative key ("a/b/c") to its on-disk .md file.
func keyToPath(root, key string) string {
	parts := strings.Split(key, "/")
	parts[len(parts)-1] += ".md"
	return filepath.Join(append([]string{root}, parts...)...)
}

// pathToKey is the inverse: an absolute .md path under root → its key, or "" if
// the path is not a .md file under root.
func pathToKey(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || !strings.HasSuffix(rel, ".md") {
		return ""
	}
	rel = strings.TrimSuffix(rel, ".md")
	return filepath.ToSlash(rel)
}
