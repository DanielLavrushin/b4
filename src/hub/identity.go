package hub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
)

const identityFileName = "identity.json"

type identityFile struct {
	Seed      string `json:"seed"`
	CreatedAt string `json:"created_at"`
}

func (s *Service) identityPath() string {
	return filepath.Join(s.Dir(), identityFileName)
}

func readIdentityFile(path string) (*hubwire.Identity, time.Time, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var f identityFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, time.Time{}, fmt.Errorf("identity file does not decode: %w", err)
	}
	seed, err := hubwire.DecodeSeed(f.Seed)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("identity file: %w", err)
	}
	id, err := hubwire.IdentityFromSeed(seed)
	if err != nil {
		return nil, time.Time{}, err
	}
	created, _ := time.Parse(time.RFC3339, f.CreatedAt)
	return id, created, nil
}

func writeIdentityFile(path string, id *hubwire.Identity, created time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(identityFile{Seed: hubwire.EncodeSeed(id.Seed()), CreatedAt: created.UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return writeFileAtomic(path, raw, 0600)
}

func (s *Service) Identity() (*hubwire.Identity, time.Time, error) {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	if s.identity != nil {
		return s.identity, s.identityCreated, nil
	}
	path := s.identityPath()
	id, created, err := readIdentityFile(path)
	if err == nil {
		s.identity, s.identityCreated = id, created
		return id, created, nil
	}
	if !os.IsNotExist(err) {
		return nil, time.Time{}, err
	}
	id, err = hubwire.NewIdentity()
	if err != nil {
		return nil, time.Time{}, err
	}
	created = s.now().UTC()
	if err := writeIdentityFile(path, id, created); err != nil {
		return nil, time.Time{}, err
	}
	s.identity, s.identityCreated = id, created
	log.Infof("hub: created identity %s", id.KeyID())
	return id, created, nil
}

func (s *Service) RestoreIdentity(code string) (*hubwire.Identity, error) {
	id, err := hubwire.IdentityFromRecoveryCode(code)
	if err != nil {
		return nil, err
	}
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	created := s.now().UTC()
	if err := writeIdentityFile(s.identityPath(), id, created); err != nil {
		return nil, err
	}
	s.identity, s.identityCreated = id, created
	log.Infof("hub: identity restored as %s", id.KeyID())
	return id, nil
}
