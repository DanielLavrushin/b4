package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/daniellavrushin/b4/log"
)

const (
	AsnCacheFileName  = "asn_cache.json"
	AsnSourceRIPEstat = "ripestat"

	asnCacheFileMode os.FileMode = 0644
)

type AsnInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Prefixes  []string `json:"prefixes"`
	UpdatedAt int64    `json:"updated_at,omitempty"`
	Source    string   `json:"source,omitempty"`
}

func (a *AsnInfo) clone() *AsnInfo {
	if a == nil {
		return nil
	}
	c := *a
	c.Prefixes = append(make([]string, 0, len(a.Prefixes)), a.Prefixes...)
	return &c
}

func (a *AsnInfo) Counts() AsnCounts {
	if a == nil {
		return AsnCounts{}
	}
	return CountPrefixes(a.Prefixes)
}

type asnIndex struct {
	owner  map[netip.Prefix]uint32
	shared map[netip.Prefix][]uint32
	bits4  []int
	bits6  []int
}

type AsnStore struct {
	path   string
	saveMu sync.Mutex
	mu     sync.RWMutex
	data   map[string]*AsnInfo
	index  asnIndex
}

func NewAsnStore(configPath string) *AsnStore {
	s := &AsnStore{data: make(map[string]*AsnInfo)}
	if configPath != "" {
		s.path = filepath.Join(filepath.Dir(configPath), AsnCacheFileName)
		s.load()
	}
	s.index = buildAsnIndex(s.data)
	return s
}

func (s *AsnStore) Path() string {
	return s.path
}

func (s *AsnStore) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("Could not read the ASN cache %s, starting with an empty one: %v", s.path, err)
		}
		return
	}
	var parsed map[string]*AsnInfo
	if err := json.Unmarshal(raw, &parsed); err != nil {
		kept, moveErr := moveCorruptAside(s.path)
		if moveErr != nil {
			log.Errorf("The ASN cache %s is not valid JSON (%v) and could not be moved aside (%v); starting with an empty ASN cache", s.path, err, moveErr)
			return
		}
		log.Errorf("The ASN cache %s is not valid JSON (%v); it was moved to %s and b4 starts with an empty ASN cache, so the ASNs that sets reference are fetched again on the next refresh", s.path, err, kept)
		return
	}
	for key, info := range parsed {
		entry, ok := sanitizeAsnInfo(info, key)
		if !ok {
			log.Warnf("Dropping ASN cache entry %q: it is not a public AS number", key)
			continue
		}
		if prev := s.data[entry.ID]; prev != nil {
			entry = mergeAsnInfo(prev, entry)
		}
		s.data[entry.ID] = entry
	}
}

func sanitizeAsnInfo(info *AsnInfo, key string) (*AsnInfo, bool) {
	if info == nil {
		return nil, false
	}
	id, ok := NormalizeASN(info.ID)
	if !ok && key != "" {
		id, ok = NormalizeASN(key)
	}
	if !ok {
		return nil, false
	}
	updated := info.UpdatedAt
	if updated < 0 {
		updated = 0
	}
	return &AsnInfo{
		ID:        id,
		Name:      strings.TrimSpace(info.Name),
		Prefixes:  SanitizeASNPrefixes(info.Prefixes),
		UpdatedAt: updated,
		Source:    strings.TrimSpace(info.Source),
	}, true
}

func mergeAsnInfo(a, b *AsnInfo) *AsnInfo {
	switch {
	case a.UpdatedAt > b.UpdatedAt:
		return a
	case b.UpdatedAt > a.UpdatedAt:
		return b
	}
	merged := b.clone()
	if merged.Name == "" {
		merged.Name = a.Name
	}
	if merged.Source == "" {
		merged.Source = a.Source
	}
	merged.Prefixes = SanitizeASNPrefixes(append(append([]string{}, a.Prefixes...), b.Prefixes...))
	return merged
}

func buildAsnIndex(data map[string]*AsnInfo) asnIndex {
	idx := asnIndex{owner: make(map[netip.Prefix]uint32)}
	ids := make([]uint32, 0, len(data))
	for id := range data {
		ids = append(ids, asnNumber(id))
	}
	slices.Sort(ids)
	var has4 [33]bool
	var has6 [129]bool
	for _, n := range ids {
		info := data[asnKey(n)]
		if info == nil {
			continue
		}
		for _, raw := range info.Prefixes {
			p, err := netip.ParsePrefix(raw)
			if err != nil {
				continue
			}
			p = p.Masked()
			if first, taken := idx.owner[p]; taken {
				if first == n {
					continue
				}
				if idx.shared == nil {
					idx.shared = make(map[netip.Prefix][]uint32)
				}
				if len(idx.shared[p]) == 0 {
					idx.shared[p] = []uint32{first}
				}
				idx.shared[p] = append(idx.shared[p], n)
				continue
			}
			idx.owner[p] = n
			if p.Addr().Is4() {
				has4[p.Bits()] = true
			} else {
				has6[p.Bits()] = true
			}
		}
	}
	for b := 32; b >= 0; b-- {
		if has4[b] {
			idx.bits4 = append(idx.bits4, b)
		}
	}
	for b := 128; b >= 0; b-- {
		if has6[b] {
			idx.bits6 = append(idx.bits6, b)
		}
	}
	return idx
}

func (idx *asnIndex) lookup(addr netip.Addr) (netip.Prefix, []uint32) {
	bits := idx.bits6
	if addr.Is4() {
		bits = idx.bits4
	}
	for _, b := range bits {
		p, err := addr.Prefix(b)
		if err != nil {
			continue
		}
		first, ok := idx.owner[p]
		if !ok {
			continue
		}
		if all := idx.shared[p]; len(all) > 0 {
			return p, all
		}
		return p, []uint32{first}
	}
	return netip.Prefix{}, nil
}

func (s *AsnStore) Get(id string) *AsnInfo {
	norm, ok := NormalizeASN(id)
	if !ok {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[norm].clone()
}

func (s *AsnStore) GetAll() map[string]*AsnInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*AsnInfo, len(s.data))
	for k, v := range s.data {
		result[k] = v.clone()
	}
	return result
}

func (s *AsnStore) Put(info *AsnInfo) error {
	if info == nil {
		return errors.New("no ASN entry to store")
	}
	entry, ok := sanitizeAsnInfo(info, "")
	if !ok {
		return fmt.Errorf("%q is not a public AS number", info.ID)
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	s.data[entry.ID] = entry
	s.index = buildAsnIndex(s.data)
	s.mu.Unlock()
	return s.persistLocked()
}

func (s *AsnStore) Delete(id string) error {
	norm, ok := NormalizeASN(id)
	if !ok {
		return fmt.Errorf("%q is not a public AS number", id)
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	if _, exists := s.data[norm]; !exists {
		s.mu.Unlock()
		return nil
	}
	delete(s.data, norm)
	s.index = buildAsnIndex(s.data)
	s.mu.Unlock()
	return s.persistLocked()
}

func (s *AsnStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	data, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	return writeFileAtomic(s.path, data, asnCacheFileMode)
}

func parseLookupAddr(raw string) (netip.Addr, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return netip.Addr{}, false
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().WithZone("").Unmap(), true
	}
	addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.WithZone("").Unmap(), true
}

func (s *AsnStore) LookupIP(ip string) (string, []*AsnInfo) {
	addr, ok := parseLookupAddr(ip)
	if !ok {
		return "", nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix, owners := s.index.lookup(addr)
	if len(owners) == 0 {
		return "", nil
	}
	out := make([]*AsnInfo, 0, len(owners))
	for _, n := range owners {
		if info := s.data[asnKey(n)]; info != nil {
			out = append(out, info.clone())
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	return prefix.String(), out
}

func (s *AsnStore) FindByIP(ip string) *AsnInfo {
	_, matches := s.LookupIP(ip)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

func (s *AsnStore) Expand(ids []string, ipVersion string) (prefixes []string, unresolved []string) {
	want4 := ipVersion != "6"
	want6 := ipVersion != "4"
	seenID := make(map[string]struct{}, len(ids))
	var seenPrefix map[string]struct{}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, raw := range ids {
		id, ok := NormalizeASN(raw)
		if !ok {
			unresolved = append(unresolved, strings.TrimSpace(raw))
			continue
		}
		if _, dup := seenID[id]; dup {
			continue
		}
		seenID[id] = struct{}{}
		info := s.data[id]
		if info == nil || len(info.Prefixes) == 0 {
			unresolved = append(unresolved, id)
			continue
		}
		if len(seenID) > 1 && seenPrefix == nil {
			seenPrefix = make(map[string]struct{}, len(prefixes)+len(info.Prefixes))
			for _, p := range prefixes {
				seenPrefix[p] = struct{}{}
			}
		}
		for _, p := range info.Prefixes {
			isV6 := strings.IndexByte(p, ':') >= 0
			if (isV6 && !want6) || (!isV6 && !want4) {
				continue
			}
			if seenPrefix != nil {
				if _, dup := seenPrefix[p]; dup {
					continue
				}
				seenPrefix[p] = struct{}{}
			}
			prefixes = append(prefixes, p)
		}
	}
	return prefixes, unresolved
}

var (
	asnStore       atomic.Pointer[AsnStore]
	asnRefreshWake = make(chan struct{}, 1)
)

func InitAsnStore(configPath string) *AsnStore {
	s := NewAsnStore(configPath)
	asnStore.Store(s)
	return s
}

func Asns() *AsnStore {
	if s := asnStore.Load(); s != nil {
		return s
	}
	asnStore.CompareAndSwap(nil, NewAsnStore(""))
	return asnStore.Load()
}

func ExpandASNs(ids []string, ipVersion string) (prefixes []string, unresolved []string) {
	if len(ids) == 0 {
		return nil, nil
	}
	return Asns().Expand(ids, ipVersion)
}

func RequestASNRefresh() {
	select {
	case asnRefreshWake <- struct{}{}:
	default:
	}
}

func ASNRefreshRequests() <-chan struct{} {
	return asnRefreshWake
}
