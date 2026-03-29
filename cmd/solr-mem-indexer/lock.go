package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// FileLock provides a filesystem-based advisory lock to prevent concurrent indexing.
type FileLock struct {
	path string
	file *os.File
}

// NewFileLock creates a lock using a file in the given directory.
func NewFileLock(dir string) *FileLock {
	return &FileLock{
		path: filepath.Join(dir, ".solr-mem-indexer.lock"),
	}
}

// Lock acquires an exclusive lock. Returns an error if another process holds it.
func (l *FileLock) Lock() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("another indexer is already running (lock: %s)", l.path)
	}

	// Write our PID for debugging
	f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())

	l.file = f
	return nil
}

// Unlock releases the lock.
func (l *FileLock) Unlock() {
	if l.file != nil {
		syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
		l.file.Close()
		os.Remove(l.path)
		l.file = nil
	}
}
