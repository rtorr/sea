package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	lockFileName   = ".lock"
	lockTimeout    = 30 * time.Second
	lockRetryDelay = 100 * time.Millisecond
)

// acquireLock attempts to acquire a file-based lock in the cache directory.
// It retries for up to 30 seconds with 100ms intervals.
func (c *Cache) acquireLock() (string, error) {
	lockPath := filepath.Join(c.Layout.Root, lockFileName)
	deadline := time.Now().Add(lockTimeout)

	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			// Write our PID for diagnostics
			fmt.Fprintf(f, "%d", os.Getpid())
			f.Close()
			return lockPath, nil
		}

		if !os.IsExist(err) {
			return "", fmt.Errorf("acquiring cache lock: %w", err)
		}

		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for cache lock %s (another sea process may be running)", lockPath)
		}

		time.Sleep(lockRetryDelay)
	}
}

// releaseLock removes the lock file.
func (c *Cache) releaseLock(lockPath string) {
	os.Remove(lockPath)
}
