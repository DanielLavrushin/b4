package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

const SecretsFileName = "ai_secrets.json"

type SecretStore struct {
	path string
	mu   sync.RWMutex
	data map[string]string
}

func NewSecretStore(configPath string) *SecretStore {
	dir := filepath.Dir(configPath)
	if dir == "" || dir == "." {
		dir = "."
	}
	s := &SecretStore{
		path: filepath.Join(dir, SecretsFileName),
		data: map[string]string{},
	}
	s.load()
	return s
}

func (s *SecretStore) Path() string {
	return s.path
}

func (s *SecretStore) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Errorf("ai: failed to read %s: %v", s.path, err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := json.Unmarshal(raw, &s.data); err != nil {
		log.Errorf("ai: failed to parse %s: %v", s.path, err)
		s.data = map[string]string{}
	}
}

func (s *SecretStore) persist(data map[string]string) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		log.Debugf("ai: failed to restrict permissions on %s: %v", tmp, err)
	}
	return os.Rename(tmp, s.path)
}

func (s *SecretStore) Get(ref string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[ref]
}

func (s *SecretStore) Set(ref, key string) error {
	if ref == "" {
		return nil
	}
	s.mu.Lock()
	next := make(map[string]string, len(s.data)+1)
	for k, v := range s.data {
		next[k] = v
	}
	if key == "" {
		delete(next, ref)
	} else {
		next[ref] = key
	}
	s.mu.Unlock()

	if err := s.persist(next); err != nil {
		return err
	}

	s.mu.Lock()
	s.data = next
	s.mu.Unlock()
	return nil
}

func (s *SecretStore) Delete(ref string) error {
	return s.Set(ref, "")
}

func (s *SecretStore) Refs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs := make([]string, 0, len(s.data))
	for k := range s.data {
		refs = append(refs, k)
	}
	return refs
}

func (s *SecretStore) Has(ref string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[ref]
	return ok
}
