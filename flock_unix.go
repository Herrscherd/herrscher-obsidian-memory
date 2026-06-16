//go:build unix

package obsidian

import "syscall"

// lockFD tries to take an exclusive advisory lock on fd without blocking. It
// returns syscall.EWOULDBLOCK when another process holds the lock, so the caller
// can poll under a context deadline rather than block uninterruptibly.
func lockFD(fd uintptr) error { return syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB) }

// unlockFD releases the advisory lock held on fd.
func unlockFD(fd uintptr) error { return syscall.Flock(int(fd), syscall.LOCK_UN) }
