//go:build !unix

package obsidian

func lockFD(uintptr) error   { return nil }
func unlockFD(uintptr) error { return nil }
