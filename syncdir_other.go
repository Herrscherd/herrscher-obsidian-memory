//go:build !unix

package obsidian

func (m *Memory) syncDir() error {
	return nil
}
