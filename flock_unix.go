//go:build unix

package obsidian

import "syscall"

// lockFD takes an exclusive advisory lock on fd, blocking until granted.
func lockFD(fd uintptr) error { return syscall.Flock(int(fd), syscall.LOCK_EX) }

// unlockFD releases the advisory lock held on fd.
func unlockFD(fd uintptr) error { return syscall.Flock(int(fd), syscall.LOCK_UN) }
