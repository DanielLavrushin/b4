package hub

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
)

const (
	manifestFileName   = "manifest.json"
	catalogueLimit     = 8 << 20
	catalogueJSONLimit = 32 << 20
)

var (
	catalogueFilePattern = regexp.MustCompile(`^catalogue-[0-9]+-[0-9]+\.json\.gz$`)
	ErrCatalogueName     = errors.New("catalogue file name is not of the form catalogue-<epoch>-<seq>.json.gz")
)

type store struct {
	dir string
}

func (s *Service) store() store {
	return store{dir: s.Dir()}
}

func (st store) manifestPath() string {
	return filepath.Join(st.dir, manifestFileName)
}

func (st store) cataloguePath(file string) (string, error) {
	if !catalogueFilePattern.MatchString(file) {
		return "", ErrCatalogueName
	}
	return filepath.Join(st.dir, file), nil
}

func (st store) loadManifest() (*hubwire.Manifest, error) {
	raw, err := os.ReadFile(st.manifestPath())
	if err != nil {
		return nil, err
	}
	var m hubwire.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest does not decode: %w", err)
	}
	return &m, nil
}

func (st store) loadCatalogue(ref hubwire.FileRef) (*hubwire.Catalogue, error) {
	path, err := st.cataloguePath(ref.File)
	if err != nil {
		return nil, err
	}
	gz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := verifyFileRef(gz, ref); err != nil {
		return nil, err
	}
	return decodeCatalogue(gz)
}

func (st store) save(m *hubwire.Manifest, gz []byte) error {
	path, err := st.cataloguePath(m.Catalogue.File)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(st.dir, 0700); err != nil {
		return err
	}
	if err := writeFileAtomic(path, gz, 0600); err != nil {
		return err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(st.manifestPath(), raw, 0600); err != nil {
		return err
	}
	st.prune(m.Catalogue.File)
	return nil
}

func (st store) prune(keep string) {
	entries, err := os.ReadDir(st.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if name == keep || !catalogueFilePattern.MatchString(name) {
			continue
		}
		_ = os.Remove(filepath.Join(st.dir, name))
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
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

func verifyFileRef(data []byte, ref hubwire.FileRef) error {
	if ref.Size > 0 && int64(len(data)) != ref.Size {
		return fmt.Errorf("%s is %d bytes, the manifest says %d", ref.File, len(data), ref.Size)
	}
	if !strings.EqualFold(hubwire.BlobHash(data), ref.SHA256) {
		return fmt.Errorf("%s does not match the sha256 in the manifest", ref.File)
	}
	return nil
}

func decodeCatalogue(gz []byte) (*hubwire.Catalogue, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("catalogue is not gzip: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(io.LimitReader(zr, catalogueJSONLimit+1))
	if err != nil {
		return nil, fmt.Errorf("catalogue does not decompress: %w", err)
	}
	if len(raw) > catalogueJSONLimit {
		return nil, fmt.Errorf("catalogue expands past the %d byte limit", catalogueJSONLimit)
	}
	var cat hubwire.Catalogue
	if err := json.Unmarshal(raw, &cat); err != nil {
		return nil, fmt.Errorf("catalogue does not decode: %w", err)
	}
	if cat.Sets == nil {
		cat.Sets = []hubwire.CatalogueSet{}
	}
	return &cat, nil
}

func EncodeCatalogue(cat *hubwire.Catalogue) ([]byte, error) {
	raw, err := json.Marshal(cat)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
