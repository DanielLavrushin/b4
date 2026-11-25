package nfq

import (
	"net"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/sni"
)

func (w *Worker) resolversNeed(cfg *config.Config, set *config.SetConfig, domain string, query []byte, srcMac string, keep ...sni.SetFilter) bool {
	qtype, _ := dns.QuestionType(query)
	zoneKeys := qtype == dnsTypeDS || qtype == dnsTypeDNSKEY
	if !zoneKeys && !cfg.IsDoHHost(domain) {
		return false
	}
	matcher := w.getMatcher()
	seen := map[string]bool{}
	for pending := set.DNS.DoHHosts(); len(pending) > 0; {
		host := pending[0]
		pending = pending[1:]
		if host == domain || zoneKeys && strings.HasSuffix(host, "."+domain) {
			return !lookedUpOutside()
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		next := w.redirectingSet(cfg, matcher, host, srcMac, keep...)
		if next == nil {
			continue
		}
		if hosts := next.DNS.DoHHosts(); !slices.Contains(hosts, host) {
			pending = append(pending, hosts...)
		}
	}
	return false
}

var (
	systemNameservers = netprobe.SystemNameservers
	ownNetworks       = net.InterfaceAddrs
)

func lookedUpOutside() bool {
	own, err := ownNetworks()
	return err == nil && !slices.ContainsFunc(systemNameservers(), func(server string) bool {
		ip := net.ParseIP(server)
		return !netprobe.PublicAddress(ip) || slices.ContainsFunc(own, func(a net.Addr) bool {
			n, ok := a.(*net.IPNet)
			return ok && n.Contains(ip)
		})
	})
}

func (w *Worker) serversToAsk(cfg *config.Config, set *config.SetConfig, servers []dns.Server, domain string, query []byte, srcMac string, asked net.IP, keep ...sni.SetFilter) []dns.Server {
	if !w.resolversNeed(cfg, set, domain, query, srcMac, keep...) {
		return servers
	}
	byAddress := slices.DeleteFunc(servers, func(srv dns.Server) bool { return serverAddress(srv) == nil })
	if len(byAddress) == 0 || !cfg.IsDoHHost(domain) || asked == nil || asked.IsUnspecified() || asked.IsLinkLocalUnicast() || len(dnsResolveInflight) >= maxDNSResolveInflight/2 {
		return nil
	}
	if slices.ContainsFunc(byAddress, func(srv dns.Server) bool { return srv.UDP.Equal(asked) }) {
		return byAddress
	}
	return append(byAddress, dns.Server{Source: asked.String(), UDP: asked, Mark: int(cfg.MainInjectedMark()), Timeout: cfg.DNSQueryTimeout(), Unlisted: true})
}

func (w *Worker) redirectingSet(cfg *config.Config, matcher *sni.SuffixSet, host, srcMac string, keep ...sni.SetFilter) *config.SetConfig {
	matched, set := matcher.MatchSNIWithSource(host, srcMac, keep...)
	if !matched {
		return nil
	}
	if esc := w.escalatedSetFor(cfg, set, host, srcMac, keep...); esc != nil {
		set = esc
	}
	if routingBlocks(set) || len(set.DNS.PinnedAddresses(host)) > 0 {
		return nil
	}
	return set
}
