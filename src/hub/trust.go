package hub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
)

const (
	mirrorsFileName     = "mirrors.json"
	revokedKeysFileName = "revoked_keys.json"
	maxManifestMirrors  = 32
)

func (s *Service) mirrorsPath() string {
	return filepath.Join(s.Dir(), mirrorsFileName)
}

func (s *Service) revokedKeysPath() string {
	return filepath.Join(s.Dir(), revokedKeysFileName)
}

func readStringList(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("hub: %s ignored: %v", filepath.Base(path), err)
		}
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		log.Warnf("hub: %s ignored: %v", filepath.Base(path), err)
		return nil
	}
	return list
}

func (s *Service) writeStringList(path string, list []string) {
	if err := s.ensureDir(); err != nil {
		log.Warnf("hub: could not store %s: %v", filepath.Base(path), err)
		return
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return
	}
	if err := writeFileAtomic(path, raw, 0600); err != nil {
		log.Warnf("hub: could not store %s: %v", filepath.Base(path), err)
	}
}

func (s *Service) loadTrust() {
	mirrors := s.manifestMirrors(readStringList(s.mirrorsPath()))
	revoked := canonicalKeys(readStringList(s.revokedKeysPath()))
	s.trustMu.Lock()
	s.mirrors = mirrors
	s.revoked = revoked
	s.trustMu.Unlock()
}

func (s *Service) manifestMirrors(raw []string) []string {
	seen := map[string]bool{}
	for _, builtin := range s.builtin {
		if base := NormalizeBaseURL(builtin); base != "" {
			seen[base] = true
		}
	}
	out := make([]string, 0, len(raw))
	for _, candidate := range raw {
		base := NormalizeBaseURL(candidate)
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		out = append(out, base)
		if len(out) == maxManifestMirrors {
			break
		}
	}
	return out
}

func canonicalKeys(raw []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, encoded := range raw {
		pub, err := hubwire.DecodeKey(encoded)
		if err != nil {
			continue
		}
		key := hubwire.EncodeKey(pub)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Service) KnownMirrors() []string {
	s.trustMu.RLock()
	defer s.trustMu.RUnlock()
	return append([]string{}, s.mirrors...)
}

func (s *Service) RevokedKeys() []string {
	s.trustMu.RLock()
	defer s.trustMu.RUnlock()
	return append([]string{}, s.revoked...)
}

func (s *Service) withoutRevoked(keys []string) []string {
	s.trustMu.RLock()
	revoked := s.revoked
	s.trustMu.RUnlock()
	if len(revoked) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, encoded := range keys {
		if keyRevoked(encoded, revoked) {
			continue
		}
		out = append(out, encoded)
	}
	return out
}

func keyRevoked(encoded string, revoked []string) bool {
	pub, err := hubwire.DecodeKey(encoded)
	if err != nil {
		return false
	}
	key := hubwire.EncodeKey(pub)
	for _, r := range revoked {
		if r == key {
			return true
		}
	}
	return false
}

func (s *Service) adoptManifest(m *hubwire.Manifest) {
	mirrors := s.manifestMirrors(m.Mirrors)
	fresh := make([]string, 0, len(m.RevokedKeys))
	for _, key := range canonicalKeys(m.RevokedKeys) {
		if key != m.KeyID {
			fresh = append(fresh, key)
		}
	}
	s.trustMu.Lock()
	mirrorsChanged := !equalStrings(mirrors, s.mirrors)
	revoked := canonicalKeys(append(append([]string{}, s.revoked...), fresh...))
	revokedChanged := !equalStrings(revoked, s.revoked)
	s.mirrors = mirrors
	s.revoked = revoked
	s.trustMu.Unlock()
	if mirrorsChanged {
		s.writeStringList(s.mirrorsPath(), mirrors)
		log.Infof("hub: manifest lists %d mirrors", len(mirrors))
	}
	if revokedChanged {
		s.writeStringList(s.revokedKeysPath(), revoked)
		log.Warnf("hub: manifest revoked hub keys %v", fresh)
	}
}
