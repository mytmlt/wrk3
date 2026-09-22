//go:build unix

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const composeLockFile = ".wrk3-compose.lock"

// LockCompose acquires an exclusive file lock on .wrk3-compose.lock inside
// worktreePath. The lock serialises compose operations (up and down) so
// concurrent processes do not race on the same worktree.  Returns the open
// file handle; callers must call UnlockCompose when done, even on error.
// The returned handle is safe to defer-close with UnlockCompose.
//
// LockCompose honours ctx cancellation: if the context is cancelled or its
// deadline expires before the lock is acquired, the call returns ctx.Err()
// and no file handle is returned.
func LockCompose(ctx context.Context, worktreePath string) (*os.File, error) {
	lockPath := filepath.Join(worktreePath, composeLockFile)
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lock compose: open lock file: %w", err)
	}

	locked := make(chan struct{}, 1)
	go func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		locked <- struct{}{}
	}()

	select {
	case <-locked:
		return f, nil
	case <-ctx.Done():
		_ = f.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("lock compose: %w", ctx.Err())
	}
}

// UnlockCompose releases the lock acquired by LockCompose, closes the file,
// and removes the lock file. It is safe to call on a nil file (no-op).
func UnlockCompose(f *os.File) error {
	if f == nil {
		return nil
	}
	lockPath := f.Name()
	var errs []error
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		errs = append(errs, fmt.Errorf("flock unlock: %w", err))
	}
	if err := f.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close lock: %w", err))
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("remove lock: %w", err))
	}
	return errors.Join(errs...)
}