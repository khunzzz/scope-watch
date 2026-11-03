// Package runlock excludes competing writers, including across processes.
package runlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

var ErrBusy = errors.New("another scope-watch run is already using this data directory")

// Acquire returns an OS-backed lock. The lock file is intentionally never unlinked.
func Acquire(dir string) (*flock.Flock, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	lock := flock.New(filepath.Join(dir, "scope-watch.lock"))
	ok, e := lock.TryLock()
	if e != nil {
		lock.Close()
		return nil, fmt.Errorf("lock data directory: %w", e)
	}
	if !ok {
		lock.Close()
		return nil, ErrBusy
	}
	return lock, nil
}
