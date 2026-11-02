package obsidian_test

import (
	"io"
	"testing"
)

func closeOnCleanup(t testing.TB, c io.Closer) {
	t.Helper()
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
}
