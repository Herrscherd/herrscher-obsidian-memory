package obsidian

import (
	"io"
	"testing"
)

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustValue[T any](t testing.TB, v T, err error) T {
	t.Helper()
	must(t, err)
	return v
}

func closeOnCleanup(t testing.TB, c io.Closer) {
	t.Helper()
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
}

func mustNew(t testing.TB, root string) *ObsidianMemory {
	t.Helper()
	m, err := New(root)
	must(t, err)
	return m
}
