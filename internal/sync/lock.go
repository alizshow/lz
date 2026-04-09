package sync

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type lockCloser struct {
	f *os.File
}

func (l *lockCloser) Close() error {
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	return l.f.Close()
}

// AcquireLock takes an exclusive, non-blocking flock on ~/.lz/sync.lock.
// Returns a Closer that releases the lock. Fails fast if another sync is running.
func AcquireLock() (io.Closer, error) {
	dir := StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "sync.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another sync is already running")
	}
	return &lockCloser{f: f}, nil
}
