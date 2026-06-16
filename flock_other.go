//go:build !unix

package obsidian

// On platforms without flock the cross-process lock is a no-op: writes stay
// atomic (temp file + rename) so a vault never corrupts, but two concurrent
// processes can still race a read-modify-write. The daemon is single-process on
// these targets, so this is acceptable.
func lockFD(fd uintptr) error   { return nil }
func unlockFD(fd uintptr) error { return nil }
