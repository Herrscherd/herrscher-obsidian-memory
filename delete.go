package obsidian

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var _ contracts.Deleter = (*ObsidianMemory)(nil)

// Delete retire la note du Key. Idempotent : un Key absent renvoie nil. Prend le
// même verrouillage in-process + cross-process que les écritures et invalide le
// parseCache pour le fichier retiré.
func (m *ObsidianMemory) Delete(ctx context.Context, key string) error {
	if err := validKey(key); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release := m.flock(ctx)
	defer release()

	rel := keyToRel(key)
	if err := m.root.Remove(rel); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // idempotent
		}
		return fmt.Errorf("obsidian: delete %q: %w", key, err)
	}
	delete(m.parseCache, rel)
	return nil
}
