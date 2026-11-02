package obsidian_test

import (
	"io"
	"testing"
)

func closeOnCleanup(tb testing.TB, c io.Closer) {
	tb.Helper()
	tb.Cleanup(func() {
		if err := c.Close(); err != nil {
			tb.Error(err)
		}
	})
}
