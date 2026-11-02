package obsidian

import (
	"fmt"
	"path/filepath"
	"strings"
)

const wikilinkMeta = "[]|\r\n"

func validKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidKey)
	}
	if filepath.IsAbs(key) {
		return fmt.Errorf("%w: absolute key %q", ErrInvalidKey, key)
	}
	if i := strings.IndexAny(key, wikilinkMeta); i >= 0 {
		return fmt.Errorf("%w: key %q contains %q, which a wikilink cannot carry", ErrInvalidKey, key, key[i:i+1])
	}
	for _, p := range strings.Split(key, "/") {
		if p == "" || p == "." || p == ".." {
			return fmt.Errorf("%w: unsafe key %q", ErrInvalidKey, key)
		}
	}
	return nil
}

func keyToPath(root, key string) string {
	return filepath.Join(root, keyToRel(key))
}

func keyToRel(key string) string {
	parts := strings.Split(key, "/")
	parts[len(parts)-1] += ".md"
	return filepath.Join(parts...)
}

func pathToKey(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || !strings.HasSuffix(rel, ".md") {
		return ""
	}
	rel = strings.TrimSuffix(rel, ".md")
	return filepath.ToSlash(rel)
}
