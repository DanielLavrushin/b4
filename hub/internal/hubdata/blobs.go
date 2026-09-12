package hubdata

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/daniellavrushin/b4/hubwire"
)

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

func (b Blobs) Exists(hash string) bool {
	path, ok := b.Path(hash)
	if !ok {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
