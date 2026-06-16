// Package obsidian implements the contracts.Memory port over an Obsidian-style
// markdown vault: one node per .md file, frontmatter for Meta, [[wikilinks]] for
// Links. The vault is a git-versioned folder; Obsidian is the human UI over it.
package obsidian

import (
	"path/filepath"
	"strings"
)

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
