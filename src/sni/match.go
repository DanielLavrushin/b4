package sni

import (
	"container/list"
	"net"
	"regexp"
	"sort"
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
	sets          map[string]*config.SetConfig
	multiSets     map[string][]*config.SetConfig
	regexes       []*regexWithSet
	regexCache    sync.Map
	ipRanger      cidranger.Ranger
	udpPortRanges []portRange
	tcpPortRanges []portRange

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
	totalDomains := 0
	for _, set := range sets {
		if set.Enabled {
			totalDomains += len(set.Targets.DomainsToMatch)
		}
	}

	s := &SuffixSet{
		sets:      make(map[string]*config.SetConfig, totalDomains),
		multiSets: make(map[string][]*config.SetConfig, totalDomains),
		regexes:   make([]*regexWithSet, 0),
		ipRanger:  cidranger.NewPCTrieRanger(),

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

			d = strings.TrimRight(d, ".")
			s.multiSets[d] = append(s.multiSets[d], set)
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
				entry := &ipRange{ipNet: ipNet, set: set}
				_ = s.ipRanger.Insert(entry)
			}
		}

		if set.UDP.DPortFilter != "" {
			for _, part := range strings.Split(set.UDP.DPortFilter, ",") {
				if pr, ok := parsePortRange(part, set); ok {
					s.udpPortRanges = append(s.udpPortRanges, pr)
				}
			}
		}

		if set.TCP.DPortFilter != "" {
			for _, part := range strings.Split(set.TCP.DPortFilter, ",") {
				if pr, ok := parsePortRange(part, set); ok {
					s.tcpPortRanges = append(s.tcpPortRanges, pr)
				}
			}
		}
	}

	return s
}

func parsePortRange(part string, set *config.SetConfig) (portRange, bool) {
	part = strings.TrimSpace(part)
	if part == "" {
		return portRange{}, false
	}
	if strings.Contains(part, "-") {
		bounds := strings.SplitN(part, "-", 2)
		if len(bounds) == 2 {
			min, err1 := strconv.Atoi(bounds[0])
			max, err2 := strconv.Atoi(bounds[1])
			if err1 == nil && err2 == nil && min >= 0 && max >= 0 && min <= max {
				return portRange{min: min, max: max, set: set}, true
			}
		}
	} else {
		port, err := strconv.Atoi(part)
		if err == nil && port >= 0 {
			return portRange{min: port, max: port, set: set}, true
		}
	}
	return portRange{}, false
}

// MatchUDPPort matches a UDP port against sets that have NO IP or domain targets (global port-only sets).
func (s *SuffixSet) MatchUDPPort(dport uint16) (bool, *config.SetConfig) {
	if s == nil || len(s.udpPortRanges) == 0 {
		return false, nil
	}

	port := int(dport)

	for _, r := range s.udpPortRanges {
		if r.set.HasIPOrDomainTargets() {
			continue
		}
		if port >= r.min && port <= r.max {
			return true, r.set
		}
	}

	return false, nil
}

// MatchTCPPort matches a TCP port against sets that have NO IP or domain targets (global port-only sets).
func (s *SuffixSet) MatchTCPPort(dport uint16) (bool, *config.SetConfig) {
	if s == nil || len(s.tcpPortRanges) == 0 {
		return false, nil
	}

	port := int(dport)

	for _, r := range s.tcpPortRanges {
		if r.set.HasIPOrDomainTargets() {
			continue
		}
		if port >= r.min && port <= r.max {
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

	// Sort by prefix length descending so most specific match wins
	// (e.g., /32 beats /16 when IP falls in both ranges)
	if len(entries) > 1 {
		sortEntriesBySpecificity(entries)
	}

	matchedEntry := entries[0].(*ipRange)
	s.cacheIPResult(ipStr, true, matchedEntry.set)

	return true, matchedEntry.set
}

func (s *SuffixSet) cacheIPResult(ipStr string, matched bool, set *config.SetConfig) {
	s.ipCacheMu.Lock()
	defer s.ipCacheMu.Unlock()

	if existing, ok := s.ipCache[ipStr]; ok {
		s.ipCacheLRU.MoveToFront(existing.element)
		return
	}

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

	s.domainCacheMu.Lock()
	defer s.domainCacheMu.Unlock()

	if existing, ok := s.domainCache[host]; ok {
		s.domainCacheLRU.MoveToFront(existing.element)
		return existing.matched, existing.set
	}

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
	if set.Targets.DomainOnly {
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
	if entry.set != nil && entry.set.Targets.DomainOnly {
		return false, nil, ""
	}

	s.learnedIPCacheMu.Lock()
	defer s.learnedIPCacheMu.Unlock()

	entry, exists = s.learnedIPCache[ipStr]
	if !exists {
		return false, nil, ""
	}
	if entry.set != nil && entry.set.Targets.DomainOnly {
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

func (s *SuffixSet) TransferLearnedIPs(old *SuffixSet) {
	if s == nil || old == nil {
		return
	}

	old.learnedIPCacheMu.RLock()
	entries := make([]struct {
		ip     string
		domain string
		set    *config.SetConfig
	}, 0, len(old.learnedIPCache))
	now := time.Now()
	for ipStr, entry := range old.learnedIPCache {
		if now.Sub(entry.learnedAt) <= old.learnedIPTTL {
			entries = append(entries, struct {
				ip     string
				domain string
				set    *config.SetConfig
			}{ip: ipStr, domain: entry.domain, set: entry.set})
		}
	}
	old.learnedIPCacheMu.RUnlock()

	for _, e := range entries {
		if matched, newSet := s.MatchSNI(e.domain); matched {
			ip := net.ParseIP(e.ip)
			if ip != nil {
				s.LearnIPToDomain(ip, e.domain, newSet)
			}
		}
	}
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

func setMatchesSource(set *config.SetConfig, srcMAC string) bool {
	if len(set.Targets.SourceDevices) == 0 {
		return true
	}
	exclude := set.Targets.SourceDevicesExclude
	if srcMAC == "" {
		return exclude
	}
	for _, mac := range set.Targets.SourceDevices {
		if strings.EqualFold(mac, srcMAC) {
			return !exclude
		}
	}
	return exclude
}

func (s *SuffixSet) MatchSNIWithSource(host string, srcMAC string) (bool, *config.SetConfig) {
	return s.MatchSNIWithSourceTLS(host, srcMAC, 0, 0)
}

// MatchSNIWithSourceTLS matches a domain with source MAC, TLS version and IP version filtering.
// tlsVersion is the client's max TLS version in wire format (0x0303=1.2, 0x0304=1.3).
// ipVersion is the packet's IP version (4 or 6). Pass 0 for either to skip that filter.
func (s *SuffixSet) MatchSNIWithSourceTLS(host string, srcMAC string, tlsVersion uint16, ipVersion uint8) (bool, *config.SetConfig) {
	if s == nil || (len(s.sets) == 0 && len(s.regexes) == 0) || host == "" {
		return false, nil
	}

	lower := strings.ToLower(host)

	if matched, set := s.matchDomainWithSourceTLS(lower, srcMAC, tlsVersion, ipVersion); matched {
		return true, set
	}

	if len(s.regexes) > 0 {
		return s.matchRegexWithSourceTLS(lower, srcMAC, tlsVersion, ipVersion)
	}

	return false, nil
}

func (s *SuffixSet) matchDomainWithSourceTLS(host string, srcMAC string, tlsVersion uint16, ipVersion uint8) (bool, *config.SetConfig) {
	candidates := s.findDomainCandidates(host)

	if len(candidates) == 0 {
		return false, nil
	}

	return selectSetBySourceAndTLS(candidates, srcMAC, tlsVersion, ipVersion)
}

func (s *SuffixSet) findDomainCandidates(host string) []*config.SetConfig {
	if sets, ok := s.multiSets[host]; ok {
		return sets
	}

	remaining := host
	for {
		idx := strings.IndexByte(remaining, '.')
		if idx == -1 {
			break
		}
		remaining = remaining[idx+1:]
		if sets, ok := s.multiSets[remaining]; ok {
			return sets
		}
	}
	return nil
}

func (s *SuffixSet) matchRegexWithSourceTLS(host string, srcMAC string, tlsVersion uint16, ipVersion uint8) (bool, *config.SetConfig) {
	var candidates []*config.SetConfig
	for _, rws := range s.regexes {
		if rws.regex.MatchString(host) {
			candidates = append(candidates, rws.set)
		}
	}
	if len(candidates) == 0 {
		return false, nil
	}
	return selectSetBySourceAndTLS(candidates, srcMAC, tlsVersion, ipVersion)
}

func (s *SuffixSet) MatchIPWithSource(ip net.IP, srcMAC string) (bool, *config.SetConfig) {
	if s == nil || s.ipRanger == nil || ip == nil {
		return false, nil
	}

	entries, err := s.ipRanger.ContainingNetworks(ip)
	if err != nil || len(entries) == 0 {
		return false, nil
	}

	// Sort by prefix length descending so most specific match wins
	sortEntriesBySpecificity(entries)

	var candidates []*config.SetConfig
	for _, e := range entries {
		candidates = append(candidates, e.(*ipRange).set)
	}

	return selectSetBySource(candidates, srcMAC, ipVersionOf(ip))
}

func ipVersionOf(ip net.IP) uint8 {
	if ip == nil {
		return 0
	}
	if ip.To4() != nil {
		return 4
	}
	return 6
}

// sortEntriesBySpecificity sorts CIDR entries by prefix length descending,
// so more specific matches (e.g., /32) come before broader ones (e.g., /16).
func sortEntriesBySpecificity(entries []cidranger.RangerEntry) {
	sort.Slice(entries, func(i, j int) bool {
		ni := entries[i].Network()
		nj := entries[j].Network()
		onesI, _ := ni.Mask.Size()
		onesJ, _ := nj.Mask.Size()
		return onesI > onesJ
	})
}

func (s *SuffixSet) MatchLearnedIPWithSource(ip net.IP, srcMAC string) (bool, *config.SetConfig, string) {
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
	if entry.set != nil && entry.set.Targets.DomainOnly {
		return false, nil, ""
	}

	s.learnedIPCacheMu.Lock()
	defer s.learnedIPCacheMu.Unlock()

	entry, exists = s.learnedIPCache[ipStr]
	if !exists {
		return false, nil, ""
	}
	if entry.set != nil && entry.set.Targets.DomainOnly {
		return false, nil, ""
	}

	if time.Since(entry.learnedAt) > s.learnedIPTTL {
		if currentEntry, stillExists := s.learnedIPCache[ipStr]; stillExists && currentEntry == entry {
			delete(s.learnedIPCache, ipStr)
			s.learnedIPCacheLRU.Remove(entry.element)
		}
		return false, nil, ""
	}

	if !setMatchesSource(entry.set, srcMAC) {
		if matched, altSet := s.MatchSNIWithSourceTLS(entry.domain, srcMAC, 0, ipVersionOf(ip)); matched {
			entry.learnedAt = time.Now()
			s.learnedIPCacheLRU.MoveToFront(entry.element)
			return true, altSet, entry.domain
		}
		return false, nil, ""
	}

	entry.learnedAt = time.Now()
	s.learnedIPCacheLRU.MoveToFront(entry.element)
	return true, entry.set, entry.domain
}

func selectSetBySource(candidates []*config.SetConfig, srcMAC string, ipVersion uint8) (bool, *config.SetConfig) {
	return selectSetBySourceAndTLS(candidates, srcMAC, 0, ipVersion)
}

func selectSetBySourceAndTLS(candidates []*config.SetConfig, srcMAC string, tlsVersion uint16, ipVersion uint8) (bool, *config.SetConfig) {
	if len(candidates) == 0 {
		return false, nil
	}

	for _, set := range candidates {
		if len(set.Targets.SourceDevices) > 0 && setMatchesSource(set, srcMAC) && set.MatchesTLSVersion(tlsVersion) && set.MatchesIPVersion(ipVersion) {
			return true, set
		}
	}

	for _, set := range candidates {
		if len(set.Targets.SourceDevices) == 0 && set.MatchesTLSVersion(tlsVersion) && set.MatchesIPVersion(ipVersion) {
			return true, set
		}
	}

	if tlsVersion != 0 || ipVersion != 0 {
		return selectSetBySourceAndTLS(candidates, srcMAC, 0, 0)
	}

	return false, nil
}
