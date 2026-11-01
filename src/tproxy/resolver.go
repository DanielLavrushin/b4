package tproxy

import (
	"net"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/sni"
)

type NameSource interface {
	ObservedNames(client, dst net.IP) dns.NameMatches
	LearnedName(dst net.IP, setID string) string
	SetHasDomain(setID, host string) bool
	WantNames(wanted bool)
}

type SetMatcher interface {
	PassOnSet(host string, dst net.IP, dport uint16, srcMAC string, applies sni.SetFilter, takes func(set *config.SetConfig, byName bool) bool) (*config.SetConfig, bool)
}

type Resolver struct {
	matcher atomic.Pointer[sni.SuffixSet]
	names   *dns.NameCache
}

var _ SetMatcher = (*Resolver)(nil)

func NewResolver(names *dns.NameCache) *Resolver {
	return &Resolver{names: names}
}

func (r *Resolver) Set(m *sni.SuffixSet) {
	r.matcher.Store(m)
}

func (r *Resolver) ObservedNames(client, dst net.IP) dns.NameMatches {
	return r.names.Lookup(client, dst)
}

func (r *Resolver) LearnedName(dst net.IP, setID string) string {
	m := r.matcher.Load()
	if m == nil || dst == nil {
		return ""
	}
	matched, set, domain := m.MatchLearnedIP(dst)
	if !matched || set == nil || set.Id != setID {
		return ""
	}
	return domain
}

func (r *Resolver) SetHasDomain(setID, host string) bool {
	return r.matcher.Load().SetHasDomain(setID, host)
}

func (r *Resolver) PassOnSet(host string, dst net.IP, dport uint16, srcMAC string, applies sni.SetFilter, takes func(set *config.SetConfig, byName bool) bool) (*config.SetConfig, bool) {
	m := r.matcher.Load()
	if m == nil {
		return nil, false
	}
	if host != "" {
		if named, set := m.MatchSNIWithSource(host, srcMAC, applies); named && !addressHoldsAgainst(m, set, dst, dport, srcMAC, applies) && takes(set, true) {
			return set, true
		}
	}
	if dst == nil {
		return nil, false
	}
	eligible := func(set *config.SetConfig) bool { return applies(set) && passOnEligible(set, false) }
	if listed, set := m.MatchIPWithSource(dst, srcMAC, eligible); listed && takes(set, false) {
		return set, false
	}
	return nil, false
}

func addressHoldsAgainst(m *sni.SuffixSet, nameSet *config.SetConfig, dst net.IP, dport uint16, srcMAC string, keep sni.SetFilter) bool {
	if dst == nil {
		return false
	}
	listed, addrSet, prefix := m.MatchIPWithSourceDetail(dst, srcMAC, keep)
	return listed && m.AddressHolds(addrSet, prefix, nameSet, srcMAC) &&
		addrSet.MatchesTCPDPort(dport) && addrSet.MatchesIPVersion(ipVersionOf(dst))
}

func ipVersionOf(ip net.IP) uint8 {
	if ip.To4() != nil {
		return 4
	}
	return 6
}

func (r *Resolver) WantNames(wanted bool) {
	r.names.SetWanted(wanted)
}
