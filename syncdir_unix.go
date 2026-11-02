//go:build unix

package obsidian

import "errors"

func (m *Memory) syncDir() error {
	dir, err := m.root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
