package config

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

type AsnInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Prefixes []string `json:"prefixes"`
}

type AsnStore struct {
	path string
	data map[string]*AsnInfo
	mu   sync.RWMutex
}

func NewAsnStore(configPath string) *AsnStore {
	dir := filepath.Dir(configPath)
	path := filepath.Join(dir, "asn_cache.json")

	s := &AsnStore{
		path: path,
		data: make(map[string]*AsnInfo),
	}
	s.load()
	return s
}

func (s *AsnStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := json.Unmarshal(data, &s.data); err != nil {
		log.Errorf("Failed to parse asn_cache.json: %v", err)
	}
}

func (s *AsnStore) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.RUnlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func (s *AsnStore) GetAll() map[string]*AsnInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*AsnInfo, len(s.data))
	for k, v := range s.data {
		copied := *v
		copied.Prefixes = append([]string{}, v.Prefixes...)
		result[k] = &copied
	}
	return result
}

func (s *AsnStore) Set(info *AsnInfo) error {
	s.mu.Lock()
	s.data[info.ID] = info
	s.mu.Unlock()
	return s.save()
}

func (s *AsnStore) Delete(asnID string) error {
	s.mu.Lock()
	delete(s.data, asnID)
	s.mu.Unlock()
	return s.save()
}

func (s *AsnStore) FindByIP(ipStr string) *AsnInfo {
	cleanIP := ipStr
	if idx := strings.Index(cleanIP, ":"); idx != -1 {
		if !strings.Contains(cleanIP, "::") && strings.Count(cleanIP, ":") == 1 {
			cleanIP = cleanIP[:idx]
		}
	}
	cleanIP = strings.Trim(cleanIP, "[]")

	ip := net.ParseIP(cleanIP)
	if ip == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, asn := range s.data {
		for _, prefix := range asn.Prefixes {
			_, cidr, err := net.ParseCIDR(prefix)
			if err != nil {
				continue
			}
			if cidr.Contains(ip) {
				copied := *asn
				copied.Prefixes = append([]string{}, asn.Prefixes...)
				return &copied
			}
		}
	}
	return nil
}
