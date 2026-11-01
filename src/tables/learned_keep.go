package tables

import (
	"net"

	"github.com/daniellavrushin/b4/config"
)

var LearnedAddressKeepFunc func(cfg *config.Config, set *config.SetConfig, ip net.IP) bool

func keepLearned(cfg *config.Config, set *config.SetConfig, ip net.IP) bool {
	keep := LearnedAddressKeepFunc
	return keep == nil || cfg == nil || set == nil || ip == nil || keep(cfg, set, ip)
}

func keepLearnedIPs(cfg *config.Config, set *config.SetConfig, ips []net.IP) []net.IP {
	keep := LearnedAddressKeepFunc
	if keep == nil || cfg == nil || set == nil {
		return ips
	}
	for i, ip := range ips {
		if ip == nil || keep(cfg, set, ip) {
			continue
		}
		kept := append(make([]net.IP, 0, len(ips)-1), ips[:i]...)
		for _, rest := range ips[i+1:] {
			if rest == nil || keep(cfg, set, rest) {
				kept = append(kept, rest)
			}
		}
		return kept
	}
	return ips
}
