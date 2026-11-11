package warmtuple

import (
	"errors"
	"net/netip"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

type FlowProfile struct {
	Config  *config.Config
	Matcher *sni.SuffixSet
}

type flowKey struct{ local, remote netip.AddrPort }

type Flows struct {
	mu      sync.RWMutex
	entries map[flowKey]*FlowProfile
}

func NewFlows() *Flows { return &Flows{entries: make(map[flowKey]*FlowProfile)} }

func (f *Flows) Register(local, remote netip.AddrPort, cfg *config.Config, set *config.SetConfig, host string) (func(), error) {
	if !local.IsValid() || !remote.IsValid() || !local.Addr().Is4() || !remote.Addr().Is4() || cfg == nil || set == nil {
		return nil, errors.New("invalid warm flow")
	}
	profileConfig := *cfg
	profileSet := *set
	profileSet.TCP.WarmTuple.Enabled = false
	profileSet.Targets = config.TargetsConfig{
		IPs: []string{remote.Addr().String()}, IpsToMatch: []string{remote.Addr().String()},
		SNIDomains: []string{host}, DomainsToMatch: []string{host}, IPPriority: true,
	}
	profileConfig.Sets = []*config.SetConfig{&profileSet}
	p := &FlowProfile{Config: &profileConfig, Matcher: sni.NewSuffixSet(profileConfig.Sets)}
	key := flowKey{local, remote}
	f.mu.Lock()
	if f.entries[key] != nil || len(f.entries) >= 4096 {
		f.mu.Unlock()
		return nil, errors.New("warm flow is already registered or registry is full")
	}
	f.entries[key] = p
	f.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { f.mu.Lock(); delete(f.entries, key); f.mu.Unlock() }) }, nil
}

func (f *Flows) Lookup(local, remote netip.AddrPort) *FlowProfile {
	if f == nil {
		return nil
	}
	f.mu.RLock()
	p := f.entries[flowKey{local, remote}]
	f.mu.RUnlock()
	return p
}

func (f *Flows) Active() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.entries)
}
