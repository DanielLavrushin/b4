package utils

import (
	"os"
	"path/filepath"
)

func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	discard := func(err error) error {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		return discard(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return discard(err)
	}
	if err := tmp.Sync(); err != nil {
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
