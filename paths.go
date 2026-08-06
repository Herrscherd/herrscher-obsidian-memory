package obsidian

import (
	"fmt"
	"path/filepath"
	"strings"
)

// wikilinkMeta are the runes a key may not contain because a key is not only a
// path: it is also written into other notes as `[[key]]`, and read back out by a
// regexp. A key holding "|" would come back with its tail parsed as the link's
// relation, and one holding "]" would come back truncated — silently, and only on
// the next read. A newline would move the rest of the key onto its own line. None
// of these can be escaped: Obsidian's wikilink syntax has no escape.
const wikilinkMeta = "[]|\r\n"

// validKey rejects keys that would escape the vault root, are malformed (empty,
// absolute, or containing "." / ".." / empty path segments), or could not survive
// the round-trip through a wikilink.
func validKey(key string) error {
	if key == "" {
		return fmt.Errorf("obsidian: empty key")
	}
	if filepath.IsAbs(key) {
		return fmt.Errorf("obsidian: absolute key %q", key)
	}
	if i := strings.IndexAny(key, wikilinkMeta); i >= 0 {
		return fmt.Errorf("obsidian: key %q contains %q, which a wikilink cannot carry", key, key[i:i+1])
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
	return filepath.Join(root, keyToRel(key))
}

// keyToRel maps a key to its vault-relative .md path (for *os.Root operations).
func keyToRel(key string) string {
	parts := strings.Split(key, "/")
	parts[len(parts)-1] += ".md"
	return filepath.Join(parts...)
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
