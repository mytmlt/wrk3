package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const composeLockFile = ".wrk3-compose.lock"

// composeLockPollInterval is how long LockCompose waits between non-blocking
// lock attempts while waiting for another process to release the lock.
const composeLockPollInterval = 20 * time.Millisecond

// ComposeLock is an exclusive inter-process lock on a worktree's compose
// operations, backed by a lock file. Acquire with LockCompose and release
// with UnlockCompose. The zero value is not usable; UnlockCompose on nil is
// a no-op.
type ComposeLock struct {
	f *os.File
}

// LockCompose acquires an exclusive file lock on .wrk3-compose.lock inside
// worktreePath. The lock serialises compose operations (up and down) so
// concurrent processes do not race on the same worktree. Callers must call
// UnlockCompose when done, even on error paths.
//
// The lock file is intentionally never deleted: removing it would break
// mutual exclusion between processes waiting on the same inode.
//
// LockCompose honours ctx cancellation: if the context is already cancelled
// it fails fast, and if the context is cancelled or its deadline expires
// while waiting for the lock, the call returns ctx.Err() and no lock is
// returned.
func LockCompose(ctx context.Context, worktreePath string) (*ComposeLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("lock compose: %w", err)
	}
	lockPath := filepath.Join(worktreePath, composeLockFile)
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lock compose: open lock file: %w", err)
	}
	for {
		if err := tryLockFile(f); err == nil {
			return &ComposeLock{f: f}, nil
		} else if !isLockContention(err) {
			_ = f.Close()
			return nil, fmt.Errorf("lock compose: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("lock compose: %w", ctx.Err())
		case <-time.After(composeLockPollInterval):
		}
	}
}

// UnlockCompose releases the lock acquired by LockCompose and closes the
// underlying file. It is safe to call on a nil lock (no-op).
func UnlockCompose(l *ComposeLock) error {
	if l == nil || l.f == nil {
		return nil
	}
	var errs []error
	if err := unlockFile(l.f); err != nil {
		errs = append(errs, fmt.Errorf("unlock compose: %w", err))
	}
	if err := l.f.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close compose lock: %w", err))
	}
	l.f = nil
	return errors.Join(errs...)
}
