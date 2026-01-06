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
	"github.com/daniellavrushin/b4/utils"
)

func (c *Config) SaveToFile(path string) error {
	if path == "" {
		log.Tracef("config path is not defined")
		return nil
	}

	c.Version = CurrentConfigVersion
	if len(c.Sets) == 0 {
		defaultCopy := NewSetConfig()
		c.Sets = []*SetConfig{&defaultCopy}
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return log.Errorf("failed to marshal config: %v", err)
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
	err = json.Unmarshal(data, c)

	if err != nil {
		return log.Errorf("failed to parse config file: %v", err)
	}

	if len(c.Sets) == 0 {
		defaultCopy := NewSetConfig()
		c.Sets = []*SetConfig{&defaultCopy}
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

func (c *Config) Validate() error {
	c.System.WebServer.IsEnabled = c.System.WebServer.Port > 0 && c.System.WebServer.Port <= 65535

	c.MainSet = nil
	for _, set := range c.Sets {
		if set.Id == MAIN_SET_ID {
			c.MainSet = set
			break
		}
	}

	if c.MainSet == nil {
		defaultCopy := NewSetConfig()
		c.MainSet = &defaultCopy
	}

	for _, set := range c.Sets {

		if len(set.Fragmentation.SeqOverlapPattern) > 0 {
			set.Fragmentation.SeqOverlapBytes = make([]byte, len(set.Fragmentation.SeqOverlapPattern))
			for i, s := range set.Fragmentation.SeqOverlapPattern {
				s = strings.TrimPrefix(s, "0x")
				b, _ := strconv.ParseUint(s, 16, 8)
				set.Fragmentation.SeqOverlapBytes[i] = byte(b)
			}
		}

		if set.Id == MAIN_SET_ID {
			continue
		}
		if set.TCP.ConnBytesLimit > c.MainSet.TCP.ConnBytesLimit {
			set.TCP.ConnBytesLimit = c.MainSet.TCP.ConnBytesLimit
		}
		if set.UDP.ConnBytesLimit > c.MainSet.UDP.ConnBytesLimit {
			set.UDP.ConnBytesLimit = c.MainSet.UDP.ConnBytesLimit
		}

	}

	if len(c.MainSet.Targets.GeoSiteCategories) > 0 && c.System.Geo.GeoSitePath == "" {
		return fmt.Errorf("--geosite must be specified when using --geo-categories")
	}

	if len(c.MainSet.Targets.GeoIpCategories) > 0 && c.System.Geo.GeoIpPath == "" {
		return fmt.Errorf("--geoip must be specified when using --geoip-categories")
	}

	if c.Queue.Threads < 1 {
		return fmt.Errorf("threads must be at least 1")
	}

	if c.Queue.StartNum < 0 || c.Queue.StartNum > 65535 {
		return fmt.Errorf("queue-num must be between 0 and 65535")
	}

	if len(c.Sets) >= 1 {
		for _, set := range c.Sets {
			if set.Id == "" {
				return fmt.Errorf("each set must have a unique non-empty ID")
			}

			if set.Id == MAIN_SET_ID {
				set.UDP.DPortFilter = utils.ValidatePorts(set.UDP.DPortFilter)
				continue
			}

			set.UDP.DPortFilter = utils.ValidatePorts(set.UDP.DPortFilter)
		}
	}

	c.LoadCapturePayloads()

	return nil
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

	set.Fragmentation.Combo.DecoySNIs = make([]string, len(defaultSet.Fragmentation.Combo.DecoySNIs))
	copy(set.Fragmentation.Combo.DecoySNIs, defaultSet.Fragmentation.Combo.DecoySNIs)

	set.Fragmentation.SeqOverlapPattern = make([]string, len(defaultSet.Fragmentation.SeqOverlapPattern))
	copy(set.Fragmentation.SeqOverlapPattern, defaultSet.Fragmentation.SeqOverlapPattern)

	set.Faking.TLSMod = make([]string, len(defaultSet.Faking.TLSMod))
	copy(set.Faking.TLSMod, defaultSet.Faking.TLSMod)

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

func (c *Config) LoadCapturePayloads() {
	if c.ConfigPath == "" {
		return
	}
	capturesDir := filepath.Join(filepath.Dir(c.ConfigPath))

	for _, set := range c.Sets {
		if set.Faking.SNIType == FakePayloadCapture && set.Faking.PayloadFile != "" {
			capturePath := filepath.Join(capturesDir, set.Faking.PayloadFile)
			data, err := os.ReadFile(capturePath)
			if err != nil {
				log.Errorf("Failed to load capture file %s: %v", set.Faking.PayloadFile, err)
				continue
			}
			set.Faking.PayloadData = data
			log.Tracef("Loaded capture payload %s (%d bytes)", set.Faking.PayloadFile, len(data))
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
