package obsidian

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var _ contracts.Deleter = (*Memory)(nil)

// Delete retire la note du Key. Idempotent : un Key absent renvoie nil. Prend le
// même verrouillage in-process + cross-process que les écritures et invalide le
// parseCache pour le fichier retiré.
func (m *Memory) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validKey(key); err != nil {
		return err
	}
	release, err := m.begin(ctx)
	if err != nil {
		return err
	}
	err = m.deleteUnlocked(key)
	return errors.Join(err, release())
}

func (m *Memory) deleteUnlocked(key string) error {
	rel := keyToRel(key)
	if err := m.root.Remove(rel); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("obsidian: delete %q: %w", key, err)
	}
	m.forget(rel)
	return nil
}
