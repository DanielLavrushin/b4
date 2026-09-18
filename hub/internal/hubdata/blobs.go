package hubdata

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

const SweepMinAge = time.Hour

var blobHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var ErrBadBlobHash = errors.New("blob hash must be 64 lower-case hex characters")

type Blobs struct {
	Dir string
}

func ValidBlobHash(hash string) bool {
	return blobHashPattern.MatchString(hash)
}

func (b Blobs) Path(hash string) (string, bool) {
	if !ValidBlobHash(hash) {
		return "", false
	}
	return filepath.Join(b.Dir, hash), true
}

func (b Blobs) Put(data []byte) (string, error) {
	hash := hubwire.BlobHash(data)
	path, _ := b.Path(hash)
	if _, err := os.Stat(path); err == nil {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
		return hash, nil
	}
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return "", err
	}
	if err := WriteFileAtomic(path, data, 0o644); err != nil {
		return "", err
	}
	return hash, nil
}

func (b Blobs) Read(hash string) ([]byte, error) {
	path, ok := b.Path(hash)
	if !ok {
		return nil, ErrBadBlobHash
	}
	return os.ReadFile(path)
}

func (b Blobs) Remove(hash string) error {
	path, ok := b.Path(hash)
	if !ok {
		return ErrBadBlobHash
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (b Blobs) Sweep(referenced map[string]struct{}, minAge time.Duration, now time.Time) ([]string, error) {
	entries, err := os.ReadDir(b.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0)
	for _, entry := range entries {
		hash := entry.Name()
		if entry.IsDir() || !ValidBlobHash(hash) {
			continue
		}
		if _, ok := referenced[hash]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) < minAge {
			continue
		}
		if err := b.Remove(hash); err != nil {
			return removed, err
		}
		removed = append(removed, hash)
	}
	sort.Strings(removed)
	return removed, nil
}

func (b Blobs) Exists(hash string) bool {
	path, ok := b.Path(hash)
	if !ok {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
