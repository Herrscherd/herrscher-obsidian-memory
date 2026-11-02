package obsidian

import (
	"io"
	"testing"
)

func must(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}

func closeOnCleanup(tb testing.TB, c io.Closer) {
	tb.Helper()
	tb.Cleanup(func() {
		if err := c.Close(); err != nil {
			tb.Error(err)
		}
	})
}

func mustNew(tb testing.TB, root string) *ObsidianMemory {
	tb.Helper()
	m, err := New(root)
	must(tb, err)
	return m
}
