package seats

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/lockfile"
)

// LockFile serializes JSON-file updates across processes. This protects the
// registry/outbox files, not the database or its engine. The lock inode is
// stable across atomic replacement of the data file.
func LockFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // workspace file selected by caller
	if err != nil {
		return nil, err
	}
	if err := lockfile.FlockExclusiveNonBlock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("identity files busy (%s): %w; retry after the current writer", path, err)
	}
	return func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "identity files: unlock: %v\n", err)
		}
	}, nil
}

func RegistryLock(dir string) (func(), error) {
	path := filepath.Join(dir, File)
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return LockFile(path + ".lock")
}

// Update locks BEFORE reading, so two writers cannot erase each other's
// alias/endpoint changes. Readers see either complete atomic snapshot.
func Update(dir string, change func(Registry) error) error {
	release, err := RegistryLock(dir)
	if err != nil {
		return err
	}
	defer release()
	r, err := Load(dir)
	if err != nil {
		return err
	}
	if err := change(r); err != nil {
		return err
	}
	return Save(dir, r)
}
