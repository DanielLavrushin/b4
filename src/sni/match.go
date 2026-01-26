package sni

import (
	"container/list"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/yl2chen/cidranger"
)

type ipRange struct {
	ipNet *net.IPNet
	set   *config.SetConfig
}

type portRange struct {
	min int
	max int
	set *config.SetConfig
}

type SuffixSet struct {
	sets       map[string]*config.SetConfig
	regexes    []*regexWithSet
	regexCache sync.Map
	ipRanger   cidranger.Ranger
	portRanges []portRange

	ipCache      map[string]*cacheEntry
	ipCacheLRU   *list.List
	ipCacheMu    sync.RWMutex
	ipCacheLimit int

	domainCache      map[string]*cacheEntry
	domainCacheLRU   *list.List
	domainCacheMu    sync.RWMutex
	domainCacheLimit int

	learnedIPCache      map[string]*learnedIPEntry
	learnedIPCacheLRU   *list.List
	learnedIPCacheMu    sync.RWMutex
	learnedIPCacheLimit int
	learnedIPTTL        time.Duration

	regexCacheSize int32
}

type cacheEntry struct {
	matched bool
	set     *config.SetConfig
	element *list.Element
}

type learnedIPEntry struct {
	domain    string
	set       *config.SetConfig
	learnedAt time.Time
	element   *list.Element
}

type regexWithSet struct {
	regex *regexp.Regexp
	set   *config.SetConfig
}

func (e *ipRange) Network() net.IPNet {
	return *e.ipNet
}

func NewSuffixSet(sets []*config.SetConfig) *SuffixSet {
	s := &SuffixSet{
		sets:     make(map[string]*config.SetConfig),
		regexes:  make([]*regexWithSet, 0),
		ipRanger: cidranger.NewPCTrieRanger(),

		ipCache:      make(map[string]*cacheEntry),
		ipCacheLRU:   list.New(),
		ipCacheLimit: 2000,

		domainCache:      make(map[string]*cacheEntry),
		domainCacheLRU:   list.New(),
		domainCacheLimit: 2000,

		learnedIPCache:      make(map[string]*learnedIPEntry),
		learnedIPCacheLRU:   list.New(),
		learnedIPCacheLimit: 5000,
		learnedIPTTL:        10 * time.Minute,
	}

	seenRegexes := make(map[string]bool)

	for _, set := range sets {
		if !set.Enabled {
			continue
		}

		for _, d := range set.Targets.DomainsToMatch {
			d = strings.ToLower(strings.TrimSpace(d))
			if d == "" {
				continue
			}

			// Handle regex patterns
			if strings.HasPrefix(d, "regexp:") {
				pattern := strings.TrimPrefix(d, "regexp:")
				if seenRegexes[pattern] {
					continue
				}
				if re, err := regexp.Compile(pattern); err == nil {
					s.regexes = append(s.regexes, &regexWithSet{regex: re, set: set})
					seenRegexes[pattern] = true
				}
				continue
			}

			// Regular domain
			d = strings.TrimRight(d, ".")
			if _, exists := s.sets[d]; !exists {
				s.sets[d] = set
			}
		}

		for _, ipStr := range set.Targets.IpsToMatch {
			ipStr = strings.TrimSpace(ipStr)
			if ipStr == "" {
				continue
			}

			var ipNet *net.IPNet
			var err error

			if strings.Contains(ipStr, "/") {
				_, ipNet, err = net.ParseCIDR(ipStr)
			} else {
				ip := net.ParseIP(ipStr)
				if ip != nil {
					if ip.To4() != nil {
						_, ipNet, _ = net.ParseCIDR(ipStr + "/32")
					} else {
						_, ipNet, _ = net.ParseCIDR(ipStr + "/128")
					}
				}
			}

			if err == nil && ipNet != nil {
				// Store set config in the ranger entry
				entry := &ipRange{ipNet: ipNet, set: set}
				_ = s.ipRanger.Insert(entry)
			}
		}

		if set.UDP.DPortFilter != "" {
			ports := strings.Split(set.UDP.DPortFilter, ",")
			for _, part := range ports {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}

				if strings.Contains(part, "-") {
					bounds := strings.SplitN(part, "-", 2)
					if len(bounds) == 2 {
						min, err1 := strconv.Atoi(bounds[0])
						max, err2 := strconv.Atoi(bounds[1])
						if err1 == nil && err2 == nil {
							if min >= 0 && max >= 0 && min <= max {
								s.portRanges = append(s.portRanges, portRange{min: min, max: max, set: set})
							}
						}
					}
				} else {
					port, err := strconv.Atoi(part)
					if err == nil && port >= 0 {
						s.portRanges = append(s.portRanges, portRange{min: port, max: port, set: set})
					}
				}
			}
		}
	}

	return s
}

func (s *SuffixSet) MatchUDPPort(dport uint16) (bool, *config.SetConfig) {
	if s == nil || len(s.portRanges) == 0 {
		return false, nil
	}

	port := int(dport)

	for _, r := range s.portRanges {

		matched := port >= r.min && port <= r.max
		if matched {
			return true, r.set
		}
	}

	return false, nil
}

func (s *SuffixSet) MatchSNI(host string) (bool, *config.SetConfig) {
	if s == nil || (len(s.sets) == 0 && len(s.regexes) == 0) || host == "" {
		return false, nil
	}

	lower := strings.ToLower(host)

	// Check exact/suffix match first (fast)
	if matched, set := s.matchDomain(lower); matched {
		return true, set
	}

	if len(s.regexes) > 0 {
		return s.matchRegex(lower)
	}

	return false, nil
}

func (s *SuffixSet) MatchIP(ip net.IP) (bool, *config.SetConfig) {
	if s == nil || s.ipRanger == nil || ip == nil {
		return false, nil
	}

	ipStr := ip.String()

	s.ipCacheMu.RLock()
	_, found := s.ipCache[ipStr]
	s.ipCacheMu.RUnlock()

	if found {
		s.ipCacheMu.Lock()
		if entry, ok := s.ipCache[ipStr]; ok {
			s.ipCacheLRU.MoveToFront(entry.element)
			matched, set := entry.matched, entry.set
			s.ipCacheMu.Unlock()
			return matched, set
		}
		s.ipCacheMu.Unlock()
	}

	entries, err := s.ipRanger.ContainingNetworks(ip)
	if err != nil || len(entries) == 0 {
		s.cacheIPResult(ipStr, false, nil)
		return false, nil
	}

	matchedEntry := entries[0].(*ipRange)
	s.cacheIPResult(ipStr, true, matchedEntry.set)

	return true, matchedEntry.set
}

func (s *SuffixSet) cacheIPResult(ipStr string, matched bool, set *config.SetConfig) {
	s.ipCacheMu.Lock()
	defer s.ipCacheMu.Unlock()

	if len(s.ipCache) >= s.ipCacheLimit {
		oldest := s.ipCacheLRU.Back()
		if oldest != nil {
			delete(s.ipCache, oldest.Value.(string))
			s.ipCacheLRU.Remove(oldest)
		}
	}

	element := s.ipCacheLRU.PushFront(ipStr)
	s.ipCache[ipStr] = &cacheEntry{
		matched: matched,
		set:     set,
		element: element,
	}
}

func (s *SuffixSet) matchDomain(host string) (bool, *config.SetConfig) {
	// Quick read-only check
	s.domainCacheMu.RLock()
	_, found := s.domainCache[host]
	s.domainCacheMu.RUnlock()

	if found {
		s.domainCacheMu.Lock()
		if entry, ok := s.domainCache[host]; ok {
			s.domainCacheLRU.MoveToFront(entry.element)
			matched, set := entry.matched, entry.set
			s.domainCacheMu.Unlock()
			return matched, set
		}
		s.domainCacheMu.Unlock()
	}

	var matched bool
	var matchedSet *config.SetConfig

	if set, ok := s.sets[host]; ok {
		matched = true
		matchedSet = set
	} else {
		remaining := host
		for {
			idx := strings.IndexByte(remaining, '.')
			if idx == -1 {
				break
			}
			remaining = remaining[idx+1:]
			if set, ok := s.sets[remaining]; ok {
				matched = true
				matchedSet = set
				break
			}
		}
	}

	// Update cache
	s.domainCacheMu.Lock()
	defer s.domainCacheMu.Unlock()

	if len(s.domainCache) >= s.domainCacheLimit {
		oldest := s.domainCacheLRU.Back()
		if oldest != nil {
			delete(s.domainCache, oldest.Value.(string))
			s.domainCacheLRU.Remove(oldest)
		}
	}

	element := s.domainCacheLRU.PushFront(host)
	s.domainCache[host] = &cacheEntry{
		matched: matched,
		set:     matchedSet,
		element: element,
	}

	return matched, matchedSet
}

func (s *SuffixSet) matchRegex(host string) (bool, *config.SetConfig) {
	if cached, ok := s.regexCache.Load(host); ok {
		entry := cached.(cacheEntry)
		return entry.matched, entry.set
	}

	var matched bool
	var matchedSet *config.SetConfig
	for _, rws := range s.regexes {
		if rws.regex.MatchString(host) {
			matched = true
			matchedSet = rws.set
			break
		}
	}

	if atomic.LoadInt32(&s.regexCacheSize) < 2000 {
		s.regexCache.Store(host, cacheEntry{matched: matched, set: matchedSet})
		atomic.AddInt32(&s.regexCacheSize, 1)
	}

	return matched, matchedSet
}

func (s *SuffixSet) LearnIPToDomain(ip net.IP, domain string, set *config.SetConfig) {
	if s == nil || ip == nil || domain == "" || set == nil {
		return
	}

	ipStr := ip.String()

	s.learnedIPCacheMu.Lock()
	defer s.learnedIPCacheMu.Unlock()

	if entry, exists := s.learnedIPCache[ipStr]; exists {
		s.learnedIPCacheLRU.MoveToFront(entry.element)
		entry.domain = domain
		entry.set = set
		entry.learnedAt = time.Now()
		return
	}

	if len(s.learnedIPCache) >= s.learnedIPCacheLimit {
		oldest := s.learnedIPCacheLRU.Back()
		if oldest != nil {
			delete(s.learnedIPCache, oldest.Value.(string))
			s.learnedIPCacheLRU.Remove(oldest)
		}
	}

	element := s.learnedIPCacheLRU.PushFront(ipStr)
	s.learnedIPCache[ipStr] = &learnedIPEntry{
		domain:    domain,
		set:       set,
		learnedAt: time.Now(),
		element:   element,
	}
}

func (s *SuffixSet) MatchLearnedIP(ip net.IP) (bool, *config.SetConfig, string) {
	if s == nil || ip == nil {
		return false, nil, ""
	}

	ipStr := ip.String()

	s.learnedIPCacheMu.RLock()
	entry, exists := s.learnedIPCache[ipStr]
	s.learnedIPCacheMu.RUnlock()

	if !exists {
		return false, nil, ""
	}

	s.learnedIPCacheMu.Lock()
	defer s.learnedIPCacheMu.Unlock()

	entry, exists = s.learnedIPCache[ipStr]
	if !exists {
		return false, nil, ""
	}

	if time.Since(entry.learnedAt) > s.learnedIPTTL {
		if currentEntry, stillExists := s.learnedIPCache[ipStr]; stillExists && currentEntry == entry {
			delete(s.learnedIPCache, ipStr)
			s.learnedIPCacheLRU.Remove(entry.element)
		}
		return false, nil, ""
	}

	entry.learnedAt = time.Now()
	s.learnedIPCacheLRU.MoveToFront(entry.element)
	return true, entry.set, entry.domain
}

func (s *SuffixSet) GetCacheStats() map[string]interface{} {
	if s == nil {
		return nil
	}

	s.ipCacheMu.RLock()
	ipCacheSize := len(s.ipCache)
	s.ipCacheMu.RUnlock()

	s.domainCacheMu.RLock()
	domainCacheSize := len(s.domainCache)
	s.domainCacheMu.RUnlock()

	s.learnedIPCacheMu.RLock()
	learnedIPCacheSize := len(s.learnedIPCache)
	s.learnedIPCacheMu.RUnlock()

	regexCacheSize := atomic.LoadInt32(&s.regexCacheSize)

	return map[string]interface{}{
		"ip_cache_size":          ipCacheSize,
		"ip_cache_limit":         s.ipCacheLimit,
		"domain_cache_size":      domainCacheSize,
		"domain_cache_limit":     s.domainCacheLimit,
		"learned_ip_cache_size":  learnedIPCacheSize,
		"learned_ip_cache_limit": s.learnedIPCacheLimit,
		"regex_cache_size":       regexCacheSize,
		"regex_cache_limit":      10000,
	}
}

func (s *SuffixSet) PortMatchesSet(dport uint16, targetSet *config.SetConfig) bool {
	if s == nil || targetSet == nil {
		return false
	}
	port := int(dport)
	for _, r := range s.portRanges {
		if r.set == targetSet && port >= r.min && port <= r.max {
			return true
		}
	}
	return false
}

func (s *SuffixSet) MatchUDPPortOnly(dport uint16) (bool, *config.SetConfig) {
	if s == nil || len(s.portRanges) == 0 {
		return false, nil
	}
	port := int(dport)
	for _, r := range s.portRanges {
		if len(r.set.Targets.IpsToMatch) == 0 && port >= r.min && port <= r.max {
			return true, r.set
		}
	}
	return false, nil
}
