package nfq

import (
	"errors"
	"net/netip"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
)

func (p *Pool) collectedEntriesLocked() []metrics.CollectedEntry {
	items := make([]metrics.CollectedEntry, 0, len(p.collected))
	for ip, candidate := range p.collected {
		name := candidate.setID
		if p.collectedBase != nil {
			if set := p.collectedBase.GetSetById(candidate.setID); set != nil {
				name = set.Name
			}
		}
		items = append(items, metrics.CollectedEntry{IP: ip.String(), Host: candidate.host, Set: name, Since: candidate.at.UnixMilli()})
	}
	slices.SortFunc(items, func(a, b metrics.CollectedEntry) int {
		if a.Since > b.Since {
			return -1
		}
		if a.Since < b.Since {
			return 1
		}
		return strings.Compare(a.IP, b.IP)
	})
	return items
}

func (p *Pool) GetCollectedIPs() []metrics.CollectedEntry {
	if p == nil {
		return []metrics.CollectedEntry{}
	}
	p.configMu.Lock()
	defer p.configMu.Unlock()
	return p.collectedEntriesLocked()
}

func (p *Pool) updateCollectedMetricsLocked() {
	metrics.GetMetricsCollector().UpdateCollectedIPs(p.collectedEntriesLocked())
}

func (p *Pool) ClearCollectedIPs(raw string) (int, error) {
	var address netip.Addr
	if raw != "" {
		var err error
		address, err = netip.ParseAddr(raw)
		if err != nil || address.Zone() != "" {
			return 0, errors.New("invalid collected IP address")
		}
		address = address.Unmap()
	}
	if p == nil {
		return 0, nil
	}
	unlock := config.LockWrites()
	defer unlock()
	p.configMu.Lock()
	defer p.configMu.Unlock()
	removed := make(map[netip.Addr]collectedCandidate)
	for ip, candidate := range p.collected {
		if !address.IsValid() || ip == address {
			removed[ip] = candidate
			delete(p.collected, ip)
		}
	}
	if len(removed) == 0 {
		return 0, nil
	}
	if p.collectedCtx == nil || !p.publishCollectedViewLocked(false) {
		for ip, candidate := range removed {
			p.collected[ip] = candidate
		}
		p.collectedDirty = true
		return 0, errors.New("collected target removal could not be applied")
	}
	if p.state != nil && p.state.destState != nil {
		t := p.state.destState
		t.watchMu.Lock()
		for key, h := range t.watches {
			if addr, ok := netip.AddrFromSlice(h.dst); ok && h.collect {
				if _, exists := removed[addr.Unmap()]; exists {
					t.unwatchLocked(key)
				}
			}
		}
		t.watchMu.Unlock()
	}
	p.updateCollectedMetricsLocked()
	return len(removed), nil
}
