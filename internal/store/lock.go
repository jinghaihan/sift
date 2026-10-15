package store

import (
	"os"
	"path/filepath"
)

// Keep the lock file in place: unlinking it would allow a new writer to lock
// another inode while a waiting writer still holds the original one.
func lockRecord(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { unlockFile(f); f.Close() }, nil
}
