package discovery

import (
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

// tlsFilterVersion converts the discovery TLS version setting to a config TLS filter value.
func (ds *DiscoverySuite) tlsFilterVersion() string {
	switch ds.tlsVersion {
	case "tls12":
		return "1.2"
	case "tls13":
		return "1.3"
	default:
		return ""
	}
}

// ipFilterVersion converts the discovery IP version setting to a config IP filter value.
func (ds *DiscoverySuite) ipFilterVersion() string {
	switch ds.ipVersion {
	case "ipv4":
		return "4"
	case "ipv6":
		return "6"
	default:
		return ""
	}
}

func (ds *DiscoverySuite) buildTestConfig(preset ConfigPreset) *config.Config {
	testSet := config.NewSetConfig()
	testSet.Name = preset.Name
	testSet.TCP = preset.Config.TCP
	testSet.UDP = preset.Config.UDP
	testSet.Fragmentation = preset.Config.Fragmentation
	testSet.Faking = preset.Config.Faking
	testSet.Hub = preset.Config.Hub
	testSet.DNS = ds.discoveredDNS

	config.ApplySetDefaults(&testSet)

	if testSet.TCP.Win.Mode == "" {
		testSet.TCP.Win.Mode = config.ConfigOff
	}
	if testSet.TCP.Desync.Mode == "" {
		testSet.TCP.Desync.Mode = config.ConfigOff
	}

	if testSet.Faking.SNIMutation.Mode == "" {
		testSet.Faking.SNIMutation.Mode = config.ConfigOff
	}
	if testSet.Faking.SNIMutation.FakeSNIs == nil {
		testSet.Faking.SNIMutation.FakeSNIs = []string{}
	}

	if preset.Name == presetNoBypass {
		testSet.Enabled = false
		testSet.DNS = config.DNSConfig{}
	} else {
		testSet.Enabled = true
		testSet.Targets.SNIDomains = []string{ds.Domain}
		testSet.Targets.DomainsToMatch = []string{ds.Domain}
		testSet.Targets.TLSVersion = ds.tlsFilterVersion()
		testSet.Targets.IPVersion = ds.ipFilterVersion()

		geoip, geosite := GetCDNCategories(ds.Domain)
		if len(geoip) > 0 || len(geosite) > 0 {
			geoip, geosite = ds.installedGeoCategories(geoip, geosite)
			if len(geoip) > 0 {
				testSet.Targets.GeoIpCategories = geoip
			}
			if len(geosite) > 0 {
				testSet.Targets.GeoSiteCategories = geosite
			}

			if len(geoip) > 0 || len(geosite) > 0 {
				tempCfg := &config.Config{System: ds.cfg.System}
				domains, ips, err := tempCfg.GetTargetsForSet(&testSet)
				if err != nil {
					log.DiscoveryLogf("Discovery: failed to load CDN categories: %v", err)
				} else {
					log.Tracef("Discovery: CDN %s - loaded %d domains, %d IPs", ds.Domain, len(domains), len(ips))
				}
			}
		} else {
			var ipsToAdd []string
			dnsResult := ds.dnsResults[ds.Domain]
			if dnsResult != nil {
				ipsToAdd = append(ipsToAdd, dnsResult.ExpectedIPs...)
				for _, probe := range dnsResult.ProbeResults {
					if probe.ResolvedIP != "" && !dnsResult.isGateway(probe.ResolvedIP) {
						found := false
						for _, ip := range ipsToAdd {
							if ip == probe.ResolvedIP {
								found = true
								break
							}
						}
						if !found {
							ipsToAdd = append(ipsToAdd, probe.ResolvedIP)
						}
					}
				}
			}

			if cidrIPs := asCIDRs(ipsToAdd); len(cidrIPs) > 0 {
				testSet.Targets.IPs = cidrIPs
				testSet.Targets.IpsToMatch = cidrIPs
				log.Tracef("Discovery: added %d IPs to test config: %v", len(cidrIPs), cidrIPs)
			}
		}
	}

	return &config.Config{
		ConfigPath: ds.cfg.ConfigPath,
		Queue:      ds.cfg.Queue,
		System:     ds.cfg.System,
		Sets:       []*config.SetConfig{&testSet},
	}
}

// buildTestConfigMulti creates a test config targeting ALL domains simultaneously.
func (ds *DiscoverySuite) buildTestConfigMulti(preset ConfigPreset) *config.Config {
	testSet := config.NewSetConfig()
	testSet.Name = preset.Name
	testSet.TCP = preset.Config.TCP
	testSet.UDP = preset.Config.UDP
	testSet.Fragmentation = preset.Config.Fragmentation
	testSet.Faking = preset.Config.Faking
	testSet.Hub = preset.Config.Hub
	testSet.DNS = ds.discoveredDNS

	config.ApplySetDefaults(&testSet)

	if testSet.TCP.Win.Mode == "" {
		testSet.TCP.Win.Mode = config.ConfigOff
	}
	if testSet.TCP.Desync.Mode == "" {
		testSet.TCP.Desync.Mode = config.ConfigOff
	}

	if testSet.Faking.SNIMutation.Mode == "" {
		testSet.Faking.SNIMutation.Mode = config.ConfigOff
	}
	if testSet.Faking.SNIMutation.FakeSNIs == nil {
		testSet.Faking.SNIMutation.FakeSNIs = []string{}
	}

	if preset.Name == presetNoBypass {
		testSet.Enabled = false
		testSet.DNS = config.DNSConfig{}
	} else {
		testSet.Enabled = true

		var allDomains []string
		var allIPs []string

		for _, di := range ds.Domains {
			allDomains = append(allDomains, di.Domain)

			geoip, geosite := GetCDNCategories(di.Domain)
			if len(geoip) > 0 || len(geosite) > 0 {
				geoip, geosite = ds.installedGeoCategories(geoip, geosite)
				testSet.Targets.GeoIpCategories = appendUnique(testSet.Targets.GeoIpCategories, geoip...)
				testSet.Targets.GeoSiteCategories = appendUnique(testSet.Targets.GeoSiteCategories, geosite...)
			}

			// Collect IPs from per-domain DNS results
			if dnsResult, ok := ds.dnsResults[di.Domain]; ok && dnsResult != nil {
				for _, ip := range dnsResult.ExpectedIPs {
					allIPs = appendUnique(allIPs, ip)
				}
				for _, probe := range dnsResult.ProbeResults {
					if probe.ResolvedIP != "" && !dnsResult.isGateway(probe.ResolvedIP) {
						allIPs = appendUnique(allIPs, probe.ResolvedIP)
					}
				}
			}
		}

		testSet.Targets.SNIDomains = allDomains
		testSet.Targets.DomainsToMatch = allDomains
		testSet.Targets.TLSVersion = ds.tlsFilterVersion()
		testSet.Targets.IPVersion = ds.ipFilterVersion()
		testSet.DNS.Pins = ds.pinsFor(allDomains)

		if cidrIPs := asCIDRs(allIPs); len(cidrIPs) > 0 {
			testSet.Targets.IPs = cidrIPs
			testSet.Targets.IpsToMatch = cidrIPs
		}

		if len(testSet.Targets.GeoIpCategories) > 0 || len(testSet.Targets.GeoSiteCategories) > 0 {
			tempCfg := &config.Config{System: ds.cfg.System}
			domains, ips, err := tempCfg.GetTargetsForSet(&testSet)
			if err != nil {
				log.DiscoveryLogf("Discovery: failed to load CDN categories: %v", err)
			} else {
				log.Tracef("Discovery: CDN - loaded %d domains, %d IPs", len(domains), len(ips))
			}
		}
	}

	return &config.Config{
		ConfigPath: ds.cfg.ConfigPath,
		Queue:      ds.cfg.Queue,
		System:     ds.cfg.System,
		Sets:       []*config.SetConfig{&testSet},
	}
}

func (ds *DiscoverySuite) scopeSetToDomains(set *config.SetConfig, domains []string) *config.SetConfig {
	if set == nil || len(domains) == 0 {
		return set
	}
	scoped := *set
	scoped.Targets.SNIDomains = append([]string(nil), domains...)
	scoped.Targets.DomainsToMatch = append([]string(nil), domains...)
	scoped.Targets.GeoIpCategories, scoped.Targets.GeoSiteCategories = ds.geoCategoriesFor(domains)
	scoped.Targets.IPs = ds.targetIPsFor(domains)
	scoped.Targets.IpsToMatch = scoped.Targets.IPs
	scoped.DNS.Pins = ds.pinsFor(domains)
	if scoped.DNS.Enabled && !ds.anyDNSPoisoned(domains) {
		scoped.DNS = config.DNSConfig{Pins: scoped.DNS.Pins}
	}
	if ds.anyAddressBlocked(domains) || len(scoped.DNS.Pins) > 0 {
		ibd := config.DefaultSetConfig.TCP.IPBlockDetect
		ibd.Enabled = true
		ibd.SynDetect = true
		ibd.HealDNS = true
		ibd.CacheBlockedIPs = true
		scoped.TCP.IPBlockDetect = ibd
	}
	return &scoped
}

func (ds *DiscoverySuite) geoCategoriesFor(domains []string) ([]string, []string) {
	var geoips, geosites []string
	for _, domain := range domains {
		geoip, geosite := GetCDNCategories(domain)
		if len(geoip) == 0 && len(geosite) == 0 {
			continue
		}
		geoip, geosite = ds.installedGeoCategories(geoip, geosite)
		geoips = appendUnique(geoips, geoip...)
		geosites = appendUnique(geosites, geosite...)
	}
	return geoips, geosites
}

func (ds *DiscoverySuite) targetIPsFor(domains []string) []string {
	var ips []string
	for _, domain := range domains {
		result := ds.dnsResults[domain]
		if result == nil {
			continue
		}
		ips = appendUnique(ips, result.ExpectedIPs...)
		for _, probe := range result.ProbeResults {
			if probe.ResolvedIP != "" && !result.isGateway(probe.ResolvedIP) {
				ips = appendUnique(ips, probe.ResolvedIP)
			}
		}
	}
	return asCIDRs(ips)
}

func asCIDRs(ips []string) []string {
	if len(ips) == 0 {
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		switch {
		case strings.Contains(ip, "/"):
			out = append(out, ip)
		case strings.Contains(ip, ":"):
			out = append(out, ip+"/128")
		default:
			out = append(out, ip+"/32")
		}
	}
	return out
}

func (ds *DiscoverySuite) anyDNSPoisoned(domains []string) bool {
	for _, domain := range domains {
		if r := ds.dnsResults[domain]; r != nil && r.IsPoisoned {
			return true
		}
	}
	return false
}
