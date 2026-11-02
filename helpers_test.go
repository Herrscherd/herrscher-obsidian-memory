package obsidian

import (
	"context"
	"io"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
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

func mustNew(tb testing.TB, root string) *Memory {
	tb.Helper()
	m, err := New(root)
	must(tb, err)
	return m
}

func loadNote(tb testing.TB, m *Memory, key string) (contracts.Node, error) {
	tb.Helper()
	sg, err := m.Recall(context.Background(), key, 0)
	return sg.Root, err
}
