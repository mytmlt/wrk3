package runner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLockComposeBlocksUntilUnlock(t *testing.T) {
	dir := t.TempDir()

	// Acquire first lock (main goroutine).
	f1, err := LockCompose(context.Background(), dir)
	if err != nil {
		t.Fatalf("first LockCompose: %v", err)
	}
	defer func() { _ = UnlockCompose(f1) }()

	var acquired atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		f2, err := LockCompose(context.Background(), dir)
		if err != nil {
			t.Logf("second LockCompose (unexpected): %v", err)
			return
		}
		acquired.Store(true)
		_ = UnlockCompose(f2)
	}()

	// Give the goroutine time to attempt the lock.
	time.Sleep(200 * time.Millisecond)
	if acquired.Load() {
		t.Fatal("second lock acquired before first was unlocked")
	}

	// Release first lock.
	_ = UnlockCompose(f1)

	// Wait for the goroutine to acquire and release.
	wg.Wait()
	if !acquired.Load() {
		t.Fatal("second lock was not acquired after first was released")
	}
}

func TestLockComposeContextCancellation(t *testing.T) {
	dir := t.TempDir()

	// Hold the lock on this goroutine so the second attempt blocks.
	f1, err := LockCompose(context.Background(), dir)
	if err != nil {
		t.Fatalf("first LockCompose: %v", err)
	}
	defer func() { _ = UnlockCompose(f1) }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = LockCompose(ctx, dir)
	if err == nil {
		t.Fatal("LockCompose should return error on context cancellation")
	}
	if !os.IsTimeout(err) && ctx.Err() == nil {
		t.Fatalf("LockCompose error = %v, want timeout/context error", err)
	}
}

func TestLockComposeLockFilePersists(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, composeLockFile)

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("lock file should not exist before LockCompose")
	}

	f, err := LockCompose(context.Background(), dir)
	if err != nil {
		t.Fatalf("LockCompose: %v", err)
	}

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should exist after LockCompose: %v", err)
	}

	if err := UnlockCompose(f); err != nil {
		t.Fatalf("UnlockCompose: %v", err)
	}

	// The lock file must survive unlock: deleting it would hand a new inode
	// to the next locker while a contender still waits on the old one,
	// silently breaking mutual exclusion between processes.
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should persist after UnlockCompose: %v", err)
	}
}

func TestLockComposeCancelledContextFailsFast(t *testing.T) {
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := LockCompose(ctx, dir); err == nil {
		t.Fatal("LockCompose with cancelled context should return error")
	}
}

func TestUnlockComposeNilIsNoop(t *testing.T) {
	if err := UnlockCompose(nil); err != nil {
		t.Fatalf("UnlockCompose(nil): %v", err)
	}
}
