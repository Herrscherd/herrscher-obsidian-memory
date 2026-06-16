//go:build !unix

package obsidian

// Without flock the cross-process lock is a no-op (writes stay atomic via
// temp file + rename; the daemon is single-process on these targets).
func lockFD(uintptr) error   { return nil }
func unlockFD(uintptr) error { return nil }
