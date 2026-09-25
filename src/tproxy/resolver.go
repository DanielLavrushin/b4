package tproxy

import (
	"net"
	"sync/atomic"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/sni"
)

type NameSource interface {
	ObservedNames(client, dst net.IP) dns.NameMatches
	LearnedName(dst net.IP, setID string) string
	SetHasDomain(setID, host string) bool
	WantNames(wanted bool)
}

type Resolver struct {
	matcher atomic.Pointer[sni.SuffixSet]
	names   *dns.NameCache
}

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

func (r *Resolver) WantNames(wanted bool) {
	r.names.SetWanted(wanted)
}
