package hubdata

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
)

const (
	DBFile     = "hub.db"
	KeyFile    = "hub.key"
	SecretFile = "secret"
	BlobsDir   = "blobs"
	PublicDir  = "public"
	GeoDir     = "geo"
	SecretSize = 32
)

var (
	ErrNoIdentity      = errors.New("hub identity not found, run b4hub keygen first")
	ErrIdentityPresent = errors.New("hub identity already exists")
)

type Layout struct {
	Root string
}

func (l Layout) DBPath() string     { return filepath.Join(l.Root, DBFile) }
func (l Layout) KeyPath() string    { return filepath.Join(l.Root, KeyFile) }
func (l Layout) SecretPath() string { return filepath.Join(l.Root, SecretFile) }
func (l Layout) Blobs() Blobs       { return Blobs{Dir: filepath.Join(l.Root, BlobsDir)} }
func (l Layout) Public() string     { return filepath.Join(l.Root, PublicDir) }
func (l Layout) Geo() string        { return filepath.Join(l.Root, GeoDir) }

func (l Layout) EnsureDirs() error {
	for _, dir := range []string{l.Root, l.Blobs().Dir, l.Public(), l.Geo()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (l Layout) LoadOrCreateSecret() ([]byte, error) {
	raw, err := os.ReadFile(l.SecretPath())
	if err == nil {
		if len(raw) != SecretSize {
			return nil, fmt.Errorf("%s holds %d bytes, want %d", l.SecretPath(), len(raw), SecretSize)
		}
		return raw, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	secret := make([]byte, SecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := WriteFileAtomic(l.SecretPath(), secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

func (l Layout) LoadIdentity() (*hubwire.Identity, error) {
	raw, err := os.ReadFile(l.KeyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoIdentity
	}
	if err != nil {
		return nil, err
	}
	seed, err := hubwire.DecodeSeed(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", l.KeyPath(), err)
	}
	return hubwire.IdentityFromSeed(seed)
}

func (l Layout) WriteIdentity(id *hubwire.Identity, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(l.KeyPath()); err == nil {
			return ErrIdentityPresent
		}
	}
	return WriteFileAtomic(l.KeyPath(), []byte(hubwire.EncodeSeed(id.Seed())+"\n"), 0o600)
}

func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	discard := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return discard(err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return discard(err)
	}
	if err := tmp.Sync(); err != nil {
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
