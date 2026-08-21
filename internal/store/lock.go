package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	lockFileName = ".lock"
	// lockPollInterval is how often a waiter retries acquiring the lock.
	lockPollInterval = 5 * time.Millisecond
	// lockWaitTimeout bounds how long a writer waits for another process.
	lockWaitTimeout = 30 * time.Second
	// lockStaleAfter is when a lock left behind by a killed process is broken.
	lockStaleAfter = 2 * time.Minute
)

// lockDir serializes read-modify-write cycles across processes, since every CLI
// invocation is its own process and an in-process mutex cannot protect them.
// The lock is a directory-wide advisory lock file created with O_EXCL; a stale
// file from a killed process is broken once it is older than lockStaleAfter.
func lockDir(dir string) (func(), error) {
	path := filepath.Join(dir, lockFileName)
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		handle, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = handle.WriteString(strconv.Itoa(os.Getpid()))
			_ = handle.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire store lock %s: %w", path, err)
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("acquire store lock %s: still held after %s", path, lockWaitTimeout)
		}
		time.Sleep(lockPollInterval)
	}
}
