package tasklist

import (
	"bytes"
	"os"
	"path/filepath"
)

// Load reads and parses the file at path.
func Load(path string) (*Document, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(src), nil
}

// Update applies fn to a fresh read of the file and writes the result
// atomically. Updates made through this package hold a lock for the
// whole read-modify-write, so concurrent writers never lose a change.
// A write that changes nothing is skipped, so the file's modification
// time only moves when its content does.
func Update(path string, fn func(*Document) error) (*Document, error) {
	// Write through symlinks rather than replacing them.
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	unlock, err := lock(target)
	if err != nil {
		return nil, err
	}
	defer unlock()

	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	doc := Parse(src)
	if err := fn(doc); err != nil {
		return nil, err
	}
	out := doc.Bytes()
	if bytes.Equal(out, src) {
		return doc, nil
	}
	if err := writeAtomic(target, out, info.Mode().Perm()); err != nil {
		return nil, err
	}
	return doc, nil
}

// writeAtomic writes beside the target and renames over it, so readers
// see the old file or the new one, never a partial write.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	// Fails harmlessly once the rename has moved it.
	defer os.Remove(name)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
