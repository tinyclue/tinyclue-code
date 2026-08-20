package task

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"syscall"
	"time"
)

// lockOptions controls the retry behaviour of fileLock.
type lockOptions struct {
	Retries    int
	MinTimeout time.Duration
	MaxTimeout time.Duration
}

var defaultLockOpts = lockOptions{
	Retries:    30,
	MinTimeout: 5 * time.Millisecond,
	MaxTimeout: 100 * time.Millisecond,
}

// fileLock wraps an OS-level exclusive file lock (flock LOCK_EX).
// Release must be called to unlock.
type fileLock struct {
	f *os.File
}

// lockFile opens (or creates) the file at path and acquires an exclusive
// advisory lock with retry and exponential backoff.
func lockFile(path string, opts *lockOptions) (*fileLock, error) {
	if opts == nil {
		opts = &defaultLockOpts
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	var lastErr error
	for attempt := 0; attempt < opts.Retries; attempt++ {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &fileLock{f: f}, nil
		}
		lastErr = err
		// Exponential backoff with jitter, capped at MaxTimeout.
		base := float64(opts.MinTimeout) * math.Pow(2, float64(attempt))
		if base > float64(opts.MaxTimeout) {
			base = float64(opts.MaxTimeout)
		}
		jitter := time.Duration(rand.Int63n(int64(base / 4)))
		sleep := time.Duration(base) + jitter
		time.Sleep(sleep)
	}

	f.Close()
	return nil, fmt.Errorf("acquire lock on %s: %w after %d retries", path, lastErr, opts.Retries)
}

// Release unlocks and closes the lock file.
func (l *fileLock) Release() error {
	// Ignore error from Unlock — best effort.
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// ---------------------------------------------------------------------------
// Transaction helper: acquire + deferred release
// ---------------------------------------------------------------------------

// withLock executes fn while holding an exclusive lock on path.
func withLock(path string, opts *lockOptions, fn func() error) error {
	lock, err := lockFile(path, opts)
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Release()
	}()
	return fn()
}

// ---------------------------------------------------------------------------
// EnsureDir creates the directory if it does not exist (concurrent-safe).
// ---------------------------------------------------------------------------

var mkdirMu sync.Mutex

func ensureDir(dir string) error {
	mkdirMu.Lock()
	defer mkdirMu.Unlock()
	return os.MkdirAll(dir, 0755)
}
