//go:build unix

package obsidian

import "syscall"

func lockFD(fd uintptr) error { return syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB) }

func unlockFD(fd uintptr) error { return syscall.Flock(int(fd), syscall.LOCK_UN) }
