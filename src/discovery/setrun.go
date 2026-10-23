package discovery

import (
	"maps"
	"slices"

	"github.com/daniellavrushin/b4/config"
)

func SetRunStrategy(set *config.SetConfig) *config.SetConfig {
	strategy := config.NewSetConfig()
	strategy.TCP = set.TCP
	strategy.TCP.Win.Values = slices.Clone(set.TCP.Win.Values)
	strategy.UDP = set.UDP
	strategy.UDP.FakePayloadData = slices.Clone(set.UDP.FakePayloadData)
	strategy.Fragmentation = set.Fragmentation
	strategy.Fragmentation.StrategyPool = slices.Clone(set.Fragmentation.StrategyPool)
	strategy.Fragmentation.SeqOverlapPattern = slices.Clone(set.Fragmentation.SeqOverlapPattern)
	strategy.Fragmentation.SeqOverlapBytes = slices.Clone(set.Fragmentation.SeqOverlapBytes)
	strategy.Faking = set.Faking
	strategy.Faking.PayloadData = slices.Clone(set.Faking.PayloadData)
	strategy.Faking.TLSMod = slices.Clone(set.Faking.TLSMod)
	strategy.Faking.SNIMutation.FakeSNIs = slices.Clone(set.Faking.SNIMutation.FakeSNIs)
	strategy.DNS = set.DNS
	strategy.DNS.Pins = maps.Clone(set.DNS.Pins)
	for domain, ips := range strategy.DNS.Pins {
		strategy.DNS.Pins[domain] = slices.Clone(ips)
	}
	return &strategy
}

func SetRunVersions(set *config.SetConfig, tlsVersion, ipVersion string) (string, string) {
	if tlsVersion == "" || tlsVersion == "auto" {
		switch set.Targets.TLSVersion {
		case "1.2":
			tlsVersion = "tls12"
		case "1.3":
			tlsVersion = "tls13"
		}
	}
	if ipVersion == "" || ipVersion == "auto" {
		switch set.Targets.IPVersion {
		case "4":
			ipVersion = "ipv4"
		case "6":
			ipVersion = "ipv6"
		}
	}
	return tlsVersion, ipVersion
}
