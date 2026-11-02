//go:build unix

package obsidian

import (
	"errors"
	"syscall"
	"testing"
)

func TestFlockReleaseReturnsUnlockError(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release, err := m.flock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.lockFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := release(); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("release on a closed lock file: got %v, want EBADF", err)
	}
}
