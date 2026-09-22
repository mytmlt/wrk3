//go:build unix

package runner

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile attempts a non-blocking exclusive lock on f. It returns nil on
// success, or an error for which isLockContention reports whether another
// process holds the lock.
func tryLockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// isLockContention reports whether err means the lock is held by someone
// else (as opposed to a hard failure).
func isLockContention(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK)
}

// unlockFile releases a lock previously acquired by tryLockFile.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
