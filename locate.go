package obsidian

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var _ contracts.Locator = (*ObsidianMemory)(nil)

// Locate renvoie les URIs ouvrables de la note du Key. La mémoire résout
// elle-même son chemin (aucun caller ne devine le stockage). Erreur si le Key
// est invalide ou si la note n'existe pas.
func (m *ObsidianMemory) Locate(ctx context.Context, key string) (contracts.Location, error) {
	if err := ctx.Err(); err != nil {
		return contracts.Location{}, err
	}
	if err := validKey(key); err != nil {
		return contracts.Location{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return contracts.Location{}, err
	}
	loc, err := m.locateUnlocked(key)
	return loc, errors.Join(err, release())
}

func (m *ObsidianMemory) locateUnlocked(key string) (contracts.Location, error) {
	rel := keyToRel(key)
	file, err := m.root.Open(rel)
	if err != nil {
		return contracts.Location{}, fmt.Errorf("obsidian: locate %q: %w", key, err)
	}
	if err = file.Close(); err != nil {
		return contracts.Location{}, fmt.Errorf("obsidian: locate %q: %w", key, err)
	}
	abs := filepath.Join(m.root.Name(), rel)
	vault := filepath.Base(m.root.Name())
	obs := fmt.Sprintf("obsidian://open?vault=%s&file=%s",
		url.QueryEscape(vault), url.QueryEscape(key))
	return contracts.Location{
		Obsidian: obs,
		File:     (&url.URL{Scheme: "file", Path: abs}).String(),
	}, nil
}
