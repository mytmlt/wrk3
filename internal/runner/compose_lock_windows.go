//go:build windows

package runner

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// composeLockRangeLow is the size of the byte range locked by tryLockFile.
// Byte-range locks are independent of file size, so locking one byte at
// offset zero works even on an empty lock file.
const composeLockRangeLow = 1

// tryLockFile attempts a non-blocking exclusive lock on f. It returns nil on
// success, or an error for which isLockContention reports whether another
// process holds the lock.
func tryLockFile(f *os.File) error {
	ol := windows.Overlapped{}
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, composeLockRangeLow, 0, &ol,
	)
}

// isLockContention reports whether err means the lock is held by someone
// else (as opposed to a hard failure).
func isLockContention(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == windows.ERROR_LOCK_VIOLATION
}

// unlockFile releases a lock previously acquired by tryLockFile.
func unlockFile(f *os.File) error {
	ol := windows.Overlapped{}
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0, composeLockRangeLow, 0, &ol,
	)
}
