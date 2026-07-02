package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/tlsgen"
)

func (c *Config) SaveToFile(path string) error {
	if path == "" {
		log.Tracef("config path is not defined")
		return nil
	}

	c.Version = CurrentConfigVersion

	data, err := MarshalSparse(stripCLIOverrides(c))
	if err != nil {
		return log.Errorf("failed to marshal config: %v", err)
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return log.Errorf("failed to create config directory: %v", err)
		}
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return log.Errorf("failed to create config file: %v", err)
	}
	defer file.Close()

	_, err = file.Write(data)
	if err != nil {
		return log.Errorf("failed to write config file: %v", err)
	}
	return nil
}

func (c *Config) LoadFromFile(path string) error {
	if path == "" {
		log.Tracef("config path is not defined")
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return log.Errorf("failed to stat config file: %v", err)
	}

	if info.IsDir() {
		return log.Errorf("config path is a directory, not a file: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return log.Errorf("failed to read config file: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return log.Errorf("failed to parse config file: %v", err)
	}

	rawSets := raw["sets"]
	delete(raw, "sets")

	withoutSets, _ := json.Marshal(raw)
	if err := json.Unmarshal(withoutSets, c); err != nil {
		return log.Errorf("failed to parse config file: %v", err)
	}

	if rawSets != nil {
		var setArray []json.RawMessage
		if err := json.Unmarshal(rawSets, &setArray); err != nil {
			return log.Errorf("failed to parse sets: %v", err)
		}
		c.Sets = make([]*SetConfig, 0, len(setArray))
		for _, rawSet := range setArray {
			set := NewSetConfig()
			if err := json.Unmarshal(rawSet, &set); err != nil {
				return log.Errorf("failed to parse set: %v", err)
			}
			c.Sets = append(c.Sets, &set)
		}
	}

	return nil
}

func (cfg *Config) ApplyLogLevel(level string) {
	switch level {
	case "debug":
		cfg.System.Logging.Level = log.LevelDebug
	case "trace":
		cfg.System.Logging.Level = log.LevelTrace
	case "info":
		cfg.System.Logging.Level = log.LevelInfo
	case "error":
		cfg.System.Logging.Level = log.LevelError
	case "silent":
		cfg.System.Logging.Level = -1
	default:
		cfg.System.Logging.Level = log.LevelInfo
	}
}

func (c *Config) MainInjectedMark() uint {
	if c.Queue.Mark == 0 {
		return DefaultConfig.Queue.Mark
	}
	return c.Queue.Mark
}

func (c *Config) DiscoveryFlowMark() uint {
	if c.System.Checker.DiscoveryFlowMark != 0 {
		return c.System.Checker.DiscoveryFlowMark
	}
	return c.MainInjectedMark() + 1
}

func (c *Config) DiscoveryInjectedMark() uint {
	if c.System.Checker.DiscoveryInjectedMark != 0 {
		return c.System.Checker.DiscoveryInjectedMark
	}
	return c.MainInjectedMark() + 2
}

func (c *Config) LogString() string {
	return ""
}

// LoadTargets returns all targets (domains and IPs) from all sets grouped by set name
func (c *Config) LoadTargets() ([]*SetConfig, int, int, error) {
	result := make([]*SetConfig, 0, len(c.Sets))
	totalDomains := 0
	totalIps := 0

	// Process all sets
	for _, set := range c.Sets {

		if !set.Enabled {
			continue
		}

		domains, ips, err := c.GetTargetsForSet(set)
		if err != nil {
			return nil, -1, -1, fmt.Errorf("failed to load domains for set '%s': %w", set.Name, err)
		}
		if len(domains) > 0 {
			totalDomains += len(domains)
		}
		if len(ips) > 0 {
			totalIps += len(ips)
		}
		result = append(result, set)
	}

	return result, totalDomains, totalIps, nil
}

func (c *Config) GetTargetsForSet(set *SetConfig) ([]string, []string, error) {
	return c.GetTargetsForSetWithCache(set, nil, nil)
}

func (c *Config) GetTargetsForSetWithCache(set *SetConfig, geositeDomains, geoipIPs map[string][]string) ([]string, []string, error) {
	domains := []string{}
	ips := []string{}

	if len(set.Targets.GeoSiteCategories) > 0 && c.System.Geo.GeoSitePath != "" {
		if geositeDomains != nil {
			// Use cached data
			for _, cat := range set.Targets.GeoSiteCategories {
				if cached, ok := geositeDomains[cat]; ok {
					domains = append(domains, cached...)
				}
			}
		} else {
			// Fallback to disk (slow path)
			geoDomains, err := geodat.LoadDomainsFromCategories(
				c.System.Geo.GeoSitePath,
				set.Targets.GeoSiteCategories,
			)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load geosite domains for set '%s': %w", set.Name, err)
			}
			domains = append(domains, geoDomains...)
		}
	}

	if len(set.Targets.SNIDomains) > 0 {
		domains = append(domains, set.Targets.SNIDomains...)
	}
	set.Targets.DomainsToMatch = domains

	if len(set.Targets.GeoIpCategories) > 0 && c.System.Geo.GeoIpPath != "" {
		if geoipIPs != nil {
			for _, cat := range set.Targets.GeoIpCategories {
				if cached, ok := geoipIPs[cat]; ok {
					ips = append(ips, cached...)
				}
			}
		} else {
			geoIps, err := geodat.LoadIpsFromCategories(
				c.System.Geo.GeoIpPath,
				set.Targets.GeoIpCategories,
			)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load geoip for set '%s': %w", set.Name, err)
			}
			ips = append(ips, geoIps...)
		}
	}

	if len(set.Targets.IPs) > 0 {
		ips = append(ips, set.Targets.IPs...)
	}

	set.Targets.IpsToMatch = ips
	return domains, ips, nil
}

func (c *Config) GetSetById(id string) *SetConfig {
	for _, set := range c.Sets {
		if set.Id == id {
			return set
		}
	}
	return nil
}

func (set *SetConfig) ResetToDefaults() {
	defaultSet := DefaultSetConfig

	id := set.Id
	name := set.Name
	targets := set.Targets

	*set = defaultSet

	set.Id = id
	set.Name = name
	set.Targets = targets

	set.TCP.Win.Values = make([]int, len(defaultSet.TCP.Win.Values))
	copy(set.TCP.Win.Values, defaultSet.TCP.Win.Values)

	set.Faking.SNIMutation.FakeSNIs = make([]string, len(defaultSet.Faking.SNIMutation.FakeSNIs))
	copy(set.Faking.SNIMutation.FakeSNIs, defaultSet.Faking.SNIMutation.FakeSNIs)

	set.Fragmentation.SeqOverlapPattern = make([]string, len(defaultSet.Fragmentation.SeqOverlapPattern))
	copy(set.Fragmentation.SeqOverlapPattern, defaultSet.Fragmentation.SeqOverlapPattern)

	set.Faking.TLSMod = make([]string, len(defaultSet.Faking.TLSMod))
	copy(set.Faking.TLSMod, defaultSet.Faking.TLSMod)

	set.Routing.SourceInterfaces = make([]string, len(defaultSet.Routing.SourceInterfaces))
	copy(set.Routing.SourceInterfaces, defaultSet.Routing.SourceInterfaces)

}

func (t *TargetsConfig) AppendIP(ip []string) error {
	for _, newIP := range ip {
		exists := false
		for _, existingIP := range t.IPs {
			if existingIP == newIP {
				exists = true
				break
			}
		}
		if !exists {
			t.IPs = append(t.IPs, newIP)
		}
	}

	for _, newIP := range ip {
		exists := false
		for _, existingIP := range t.IpsToMatch {
			if existingIP == newIP {
				exists = true
				break
			}
		}
		if !exists {
			t.IpsToMatch = append(t.IpsToMatch, newIP)
		}
	}

	return nil
}
func (t *TargetsConfig) AppendSNI(sni string) error {

	for _, existingDomain := range t.SNIDomains {
		if existingDomain == sni {
			return log.Errorf("SNI '%s' already exists in the set", sni)
		}
	}
	t.SNIDomains = append(t.SNIDomains, sni)

	for _, existingDomain := range t.DomainsToMatch {
		if existingDomain == sni {
			return log.Errorf("SNI '%s' already exists in the set", sni)
		}
	}
	t.DomainsToMatch = append(t.DomainsToMatch, sni)
	return nil
}

func (cfg *Config) CollectUDPPorts() []string {
	portSet := make(map[string]bool)
	portSet["443"] = true

	for _, set := range cfg.Sets {
		if !set.Enabled || set.UDP.DPortFilter == "" {
			continue
		}
		for _, p := range strings.Split(set.UDP.DPortFilter, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				portSet[p] = true
			}
		}
	}

	ports := make([]string, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	sort.Strings(ports)
	ports = mergeAndNormalizePorts(ports)
	return ports
}

func (cfg *Config) CollectTCPPorts() []string {
	portSet := make(map[string]bool)
	portSet["443"] = true

	for _, set := range cfg.Sets {
		if !set.Enabled || set.TCP.DPortFilter == "" {
			continue
		}
		for _, p := range strings.Split(set.TCP.DPortFilter, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				portSet[p] = true
			}
		}
	}

	ports := make([]string, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	sort.Strings(ports)
	ports = mergeAndNormalizePorts(ports)
	return ports
}

// BuildTCPPortMap builds a fast lookup map of all configured TCP ports.
// Call this after CollectTCPPorts to pre-compute the set for packet handler use.
func (cfg *Config) BuildTCPPortMap() {
	cfg.tcpPortMap = make(map[uint16]bool)
	for _, p := range cfg.CollectTCPPorts() {
		p = strings.TrimSpace(p)
		if strings.Contains(p, "-") {
			parts := strings.Split(p, "-")
			if len(parts) == 2 {
				start, _ := strconv.Atoi(parts[0])
				end, _ := strconv.Atoi(parts[1])
				for i := start; i <= end; i++ {
					cfg.tcpPortMap[uint16(i)] = true
				}
			}
		} else {
			port, _ := strconv.Atoi(p)
			if port > 0 {
				cfg.tcpPortMap[uint16(port)] = true
			}
		}
	}
}

type PortRange struct {
	Min uint16
	Max uint16
}

// TLSVersionCode converts a TLS version filter string ("1.2", "1.3") to wire format.
// Returns 0 if the filter is empty (match any).
func TLSVersionCode(filter string) uint16 {
	switch filter {
	case "1.0":
		return 0x0301
	case "1.1":
		return 0x0302
	case "1.2":
		return 0x0303
	case "1.3":
		return 0x0304
	default:
		return 0
	}
}

// TLSVersionString converts a TLS wire version to a human-readable string.
func TLSVersionString(ver uint16) string {
	switch ver {
	case 0x0301:
		return "1.0"
	case 0x0302:
		return "1.1"
	case 0x0303:
		return "1.2"
	case 0x0304:
		return "1.3"
	default:
		return ""
	}
}

// MatchesTLSVersion checks if the client's max TLS version matches this set's filter.
// Returns true if no filter is configured or if tlsVersion is 0 (unknown).
func (set *SetConfig) MatchesTLSVersion(tlsVersion uint16) bool {
	if set.Targets.TLSVersion == "" {
		return true
	}
	filterVer := TLSVersionCode(set.Targets.TLSVersion)
	if filterVer == 0 {
		return false // invalid filter value — don't silently match everything
	}
	return tlsVersion == 0 || tlsVersion == filterVer
}

// MatchesIPVersion checks if the packet's IP version matches this set's filter.
// Returns true if no filter is configured or if version is 0 (unknown).
func (set *SetConfig) MatchesIPVersion(version uint8) bool {
	switch set.Targets.IPVersion {
	case "":
		return true
	case "4":
		return version == 0 || version == 4
	case "6":
		return version == 0 || version == 6
	default:
		return false // invalid filter value — don't silently match everything
	}
}

func (set *SetConfig) HasIPOrDomainTargets() bool {
	return len(set.Targets.IpsToMatch) > 0 || len(set.Targets.DomainsToMatch) > 0
}

func (set *SetConfig) MatchesTCPDPort(port uint16) bool {
	if len(set.TCPPortRanges) == 0 {
		return true
	}
	for _, r := range set.TCPPortRanges {
		if port >= r.Min && port <= r.Max {
			return true
		}
	}
	return false
}

func (set *SetConfig) MatchesUDPDPort(port uint16) bool {
	if len(set.UDPPortRanges) == 0 {
		return true
	}
	for _, r := range set.UDPPortRanges {
		if port >= r.Min && port <= r.Max {
			return true
		}
	}
	return false
}

func (cfg *Config) BuildSetPortRanges() {
	for _, set := range cfg.Sets {
		set.TCPPortRanges = nil
		set.UDPPortRanges = nil

		if set.TCP.DPortFilter != "" {
			for _, part := range strings.Split(set.TCP.DPortFilter, ",") {
				if pr, ok := parsePortRangeStr(part); ok {
					set.TCPPortRanges = append(set.TCPPortRanges, pr)
				}
			}
		}
		if set.UDP.DPortFilter != "" {
			for _, part := range strings.Split(set.UDP.DPortFilter, ",") {
				if pr, ok := parsePortRangeStr(part); ok {
					set.UDPPortRanges = append(set.UDPPortRanges, pr)
				}
			}
		}
	}
}

func parsePortRangeStr(part string) (PortRange, bool) {
	part = strings.TrimSpace(part)
	if part == "" {
		return PortRange{}, false
	}
	if strings.Contains(part, "-") {
		bounds := strings.SplitN(part, "-", 2)
		if len(bounds) == 2 {
			min, err1 := strconv.Atoi(bounds[0])
			max, err2 := strconv.Atoi(bounds[1])
			if err1 == nil && err2 == nil && min >= 1 && max >= 1 && min <= max && min <= 65535 && max <= 65535 {
				return PortRange{Min: uint16(min), Max: uint16(max)}, true
			}
		}
	} else {
		port, err := strconv.Atoi(part)
		if err == nil && port >= 1 && port <= 65535 {
			return PortRange{Min: uint16(port), Max: uint16(port)}, true
		}
	}
	return PortRange{}, false
}

// IsTCPPort checks if a port is in the configured TCP port set.
func (cfg *Config) IsTCPPort(port uint16) bool {
	if cfg.tcpPortMap == nil {
		return port == 443
	}
	return cfg.tcpPortMap[port]
}

func (cfg *Config) CollectDeviceMSSClamps() map[int][]string {
	result := make(map[int][]string)
	for _, d := range cfg.Queue.Devices.Devices {
		mac := strings.ToUpper(strings.TrimSpace(d.MAC))
		if mac == "" || d.MSSClamp <= 0 {
			continue
		}
		result[d.MSSClamp] = append(result[d.MSSClamp], mac)
	}
	return result
}

func (cfg *Config) CollectSetMSSClamps() []SetMSSClampEntry {
	var entries []SetMSSClampEntry
	for i, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.MSSClamp.Enabled || set.MSSClamp.Size <= 0 {
			continue
		}
		entry := SetMSSClampEntry{SetID: set.Id, SetIdx: i, Size: set.MSSClamp.Size}
		seen4 := make(map[string]struct{})
		seen6 := make(map[string]struct{})
		for _, ipStr := range set.Targets.IpsToMatch {
			ipStr = strings.TrimSpace(ipStr)
			if ipStr == "" {
				continue
			}
			if strings.Contains(ipStr, ":") {
				if _, ok := seen6[ipStr]; !ok {
					seen6[ipStr] = struct{}{}
					entry.IPv6 = append(entry.IPv6, ipStr)
				}
			} else {
				if _, ok := seen4[ipStr]; !ok {
					seen4[ipStr] = struct{}{}
					entry.IPv4 = append(entry.IPv4, ipStr)
				}
			}
		}
		if !set.Targets.SourceDevicesExclude {
			seenMAC := make(map[string]struct{})
			for _, m := range set.Targets.SourceDevices {
				m = strings.ToUpper(strings.TrimSpace(m))
				if m == "" {
					continue
				}
				if _, ok := seenMAC[m]; !ok {
					seenMAC[m] = struct{}{}
					entry.MACs = append(entry.MACs, m)
				}
			}
		}
		if len(entry.IPv4) == 0 && len(entry.IPv6) == 0 && len(entry.MACs) == 0 {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func (dc *DevicesConfig) SelectedMACs() []string {
	var macs []string
	seen := make(map[string]struct{})
	for _, d := range dc.Devices {
		if d.Selected && !d.IsManual {
			mac := strings.ToUpper(strings.TrimSpace(d.MAC))
			if mac == "" {
				continue
			}
			if _, ok := seen[mac]; ok {
				continue
			}
			seen[mac] = struct{}{}
			macs = append(macs, mac)
		}
	}
	return macs
}

func (dc *DevicesConfig) FindByMAC(mac string) *Device {
	mac = strings.ToUpper(strings.TrimSpace(mac))
	for i := range dc.Devices {
		if strings.ToUpper(dc.Devices[i].MAC) == mac {
			return &dc.Devices[i]
		}
	}
	return nil
}

func (dc *DevicesConfig) ManualEntries() []Device {
	var result []Device
	for _, d := range dc.Devices {
		if d.IsManual {
			result = append(result, d)
		}
	}
	return result
}

func (cfg *Config) HasGlobalMSSClamp() (bool, int) {
	if cfg.Queue.MSSClamp.Enabled && cfg.Queue.MSSClamp.Size > 0 {
		return true, cfg.Queue.MSSClamp.Size
	}
	return false, 0
}

// MSSClampFingerprint returns a string representation of the MSS clamp configuration for comparison.
func (cfg *Config) MSSClampFingerprint() string {
	parts := []string{}

	global, globalSize := cfg.HasGlobalMSSClamp()
	if global {
		parts = append(parts, fmt.Sprintf("global:%d", globalSize))
	}

	deviceClamps := cfg.CollectDeviceMSSClamps()
	for size, macs := range deviceClamps {
		sort.Strings(macs)
		parts = append(parts, fmt.Sprintf("dev:%d:%s", size, strings.Join(macs, ",")))
	}

	for _, e := range cfg.CollectSetMSSClamps() {
		ipv4 := append([]string(nil), e.IPv4...)
		ipv6 := append([]string(nil), e.IPv6...)
		macs := append([]string(nil), e.MACs...)
		sort.Strings(ipv4)
		sort.Strings(ipv6)
		sort.Strings(macs)
		parts = append(parts, fmt.Sprintf("set:%s:%d:v4=%s:v6=%s:mac=%s",
			e.SetID, e.Size,
			strings.Join(ipv4, ","),
			strings.Join(ipv6, ","),
			strings.Join(macs, ",")))
	}

	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// CollectDuplicateIPs returns IPv4 and IPv6 IPs/CIDRs from sets with duplication enabled.
// Used for firewall rules that queue packets without connbytes limit.
func (cfg *Config) CollectDuplicateIPs() (ipv4 []string, ipv6 []string) {
	seen4 := make(map[string]struct{})
	seen6 := make(map[string]struct{})
	for _, set := range cfg.Sets {
		if !set.Enabled || !set.TCP.Duplicate.Enabled {
			continue
		}
		for _, ipStr := range set.Targets.IpsToMatch {
			ipStr = strings.TrimSpace(ipStr)
			if ipStr == "" {
				continue
			}
			if strings.Contains(ipStr, ":") {
				if _, ok := seen6[ipStr]; !ok {
					seen6[ipStr] = struct{}{}
					ipv6 = append(ipv6, ipStr)
				}
			} else {
				if _, ok := seen4[ipStr]; !ok {
					seen4[ipStr] = struct{}{}
					ipv4 = append(ipv4, ipStr)
				}
			}
		}
	}
	return
}

func (c *Config) Clone() *Config {
	data, _ := json.Marshal(c)
	var clone Config
	_ = json.Unmarshal(data, &clone)
	clone.ConfigPath = c.ConfigPath

	for _, set := range clone.Sets {
		for _, origSet := range c.Sets {
			if set.Id == origSet.Id {
				set.Targets.DomainsToMatch = make([]string, len(origSet.Targets.DomainsToMatch))
				copy(set.Targets.DomainsToMatch, origSet.Targets.DomainsToMatch)

				set.Targets.IpsToMatch = make([]string, len(origSet.Targets.IpsToMatch))
				copy(set.Targets.IpsToMatch, origSet.Targets.IpsToMatch)
				break
			}
		}
	}

	clone.Validate()
	return &clone
}

func safeCapturePath(configDir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("absolute path not allowed")
	}
	configAbs, err := filepath.Abs(configDir)
	if err != nil {
		return "", err
	}
	capturesAbs := filepath.Join(configAbs, "captures")
	candidate := filepath.Clean(filepath.Join(configAbs, name))
	rel, err := filepath.Rel(capturesAbs, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes captures directory")
	}
	if !strings.HasSuffix(strings.ToLower(candidate), ".bin") {
		return "", fmt.Errorf("only .bin files are allowed")
	}
	return candidate, nil
}

func (c *Config) LoadCapturePayloads() {
	capturesDir := ""
	if c.ConfigPath != "" {
		capturesDir = filepath.Dir(c.ConfigPath)
	}

	for _, set := range c.Sets {
		if !set.Enabled {
			continue
		}
		switch set.Faking.SNIType {
		case FakePayloadDefault1:
			set.Faking.PayloadData = FakeSNI1
		case FakePayloadDefault2:
			set.Faking.PayloadData = FakeSNI2
		case FakePayloadCustom:
			set.Faking.PayloadData = []byte(set.Faking.CustomPayload)
		case FakePayloadCapture:
			if capturesDir == "" || set.Faking.PayloadFile == "" {
				set.Faking.PayloadData = nil
				continue
			}
			capturePath, err := safeCapturePath(capturesDir, set.Faking.PayloadFile)
			if err != nil {
				log.Errorf("Rejected capture payload %q: %v", set.Faking.PayloadFile, err)
				set.Faking.PayloadData = nil
				continue
			}
			data, err := os.ReadFile(capturePath)
			if err != nil {
				log.Errorf("Failed to load capture file %s: %v", set.Faking.PayloadFile, err)
				set.Faking.PayloadData = nil
				continue
			}
			set.Faking.PayloadData = data
			log.Tracef("Loaded capture payload %s (%d bytes)", set.Faking.PayloadFile, len(data))
		case FakePayloadDomain:
			if set.Faking.PayloadDomain == "" {
				set.Faking.PayloadData = nil
				continue
			}
			data, err := tlsgen.GenerateTLSClientHello(set.Faking.PayloadDomain)
			if err != nil {
				log.Errorf("Failed to generate domain payload for %s: %v", set.Faking.PayloadDomain, err)
				set.Faking.PayloadData = nil
				continue
			}
			set.Faking.PayloadData = data
			log.Tracef("Generated domain payload for %s (%d bytes)", set.Faking.PayloadDomain, len(data))
		default:
			set.Faking.PayloadData = nil
		}

		set.UDP.FakePayloadData = nil
		switch set.UDP.FakePayloadFile {
		case "", FakePayloadAutoQUIC:
		case FakePayloadPreset1:
			set.UDP.FakePayloadData = FakeQUIC1
		case FakePayloadPreset2:
			set.UDP.FakePayloadData = FakeQUIC2
		default:
			if capturesDir != "" {
				payloadPath, err := safeCapturePath(capturesDir, set.UDP.FakePayloadFile)
				if err != nil {
					log.Errorf("Rejected UDP fake payload %q: %v", set.UDP.FakePayloadFile, err)
					break
				}
				data, err := os.ReadFile(payloadPath)
				if err != nil {
					log.Errorf("Failed to load UDP fake payload %s: %v", set.UDP.FakePayloadFile, err)
				} else {
					set.UDP.FakePayloadData = data
					log.Tracef("Loaded UDP fake payload %s (%d bytes)", set.UDP.FakePayloadFile, len(data))
				}
			}
		}
	}
}

func mergeAndNormalizePorts(ports []string) []string {
	type portRange struct{ start, end int }
	var ranges []portRange

	for _, p := range ports {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.ReplaceAll(p, ":", "-")
		if strings.Contains(p, "-") {
			parts := strings.Split(p, "-")
			if len(parts) == 2 {
				start, _ := strconv.Atoi(parts[0])
				end, _ := strconv.Atoi(parts[1])
				if start > 0 && end > 0 && start <= end {
					ranges = append(ranges, portRange{start, end})
				}
			}
		} else {
			port, _ := strconv.Atoi(p)
			if port > 0 {
				ranges = append(ranges, portRange{port, port})
			}
		}
	}

	if len(ranges) == 0 {
		return nil
	}

	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })

	merged := []portRange{ranges[0]}
	for i := 1; i < len(ranges); i++ {
		last := &merged[len(merged)-1]
		cur := ranges[i]
		if cur.start <= last.end+1 {
			if cur.end > last.end {
				last.end = cur.end
			}
		} else {
			merged = append(merged, cur)
		}
	}

	result := make([]string, len(merged))
	for i, r := range merged {
		if r.start == r.end {
			result[i] = strconv.Itoa(r.start)
		} else {
			result[i] = fmt.Sprintf("%d-%d", r.start, r.end)
		}
	}
	return result
}

func (c *Config) sanitizeEscalation() {
	byID := make(map[string]*SetConfig, len(c.Sets))
	for _, s := range c.Sets {
		if s.Id != "" {
			byID[s.Id] = s
		}
	}

	for _, s := range c.Sets {
		if s.Escalate.To == "" {
			continue
		}
		if s.Escalate.To == s.Id {
			log.Warnf("Set %q (id=%s): escalate.to references self, clearing", s.Name, s.Id)
			s.Escalate.To = ""
			continue
		}
		target, ok := byID[s.Escalate.To]
		if !ok {
			log.Warnf("Set %q (id=%s): escalate.to %q not found, clearing", s.Name, s.Id, s.Escalate.To)
			s.Escalate.To = ""
			continue
		}
		if !target.Enabled {
			log.Warnf("Set %q (id=%s): escalate.to %q (id=%s) is disabled, clearing", s.Name, s.Id, target.Name, target.Id)
			s.Escalate.To = ""
			continue
		}
	}

	for _, s := range c.Sets {
		if s.Escalate.To == "" {
			continue
		}
		seen := map[string]bool{s.Id: true}
		cur := s
		for cur.Escalate.To != "" {
			if seen[cur.Escalate.To] {
				log.Warnf("Set %q (id=%s): escalate.to chain has a cycle at %q (id=%s), breaking", s.Name, s.Id, cur.Name, cur.Id)
				cur.Escalate.To = ""
				break
			}
			seen[cur.Escalate.To] = true
			cur = byID[cur.Escalate.To]
			if cur == nil {
				break
			}
		}
	}
}

// sanitizeIfaceName strips any characters not valid in a Linux interface name.
func sanitizeIfaceName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
