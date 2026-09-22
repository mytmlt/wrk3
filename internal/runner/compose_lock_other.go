//go:build !(unix || windows)

package runner

import (
	"errors"
	"os"
)

var errComposeLockUnsupported = errors.New("compose file locking not supported on this platform")

// tryLockFile always fails on platforms without a lock implementation.
func tryLockFile(_ *os.File) error {
	return errComposeLockUnsupported
}

// isLockContention always reports false: the stub never contends.
func isLockContention(_ error) bool {
	return false
}

// unlockFile is a no-op on platforms without a lock implementation.
func unlockFile(_ *os.File) error {
	return nil
}
