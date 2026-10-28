//go:build unix

package tasklist

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
)

// lockDir keeps lock files out of the user's repos.
// Tests point it at a temp dir.
var lockDir = func() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "plan", "locks"), nil
}

// lock takes an exclusive lock for a file. The lock is on a separate
// file because the atomic rename replaces the target's inode, and a
// lock held on the old inode would protect nothing.
func lock(path string) (unlock func(), err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir, err := lockDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(abs))
	name := filepath.Join(dir, hex.EncodeToString(sum[:8])+".lock")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		// Closing the descriptor releases the lock.
		f.Close()
	}, nil
}
