package metrics

import (
	"net"
	"net/netip"
	"strings"
	"sync"
)

type Device struct {
	MAC  string
	Addr netip.Addr
}

func DeviceOf(addr net.Addr) Device {
	a, ok := addr.(interface{ AddrPort() netip.AddrPort })
	if !ok {
		return Device{}
	}
	ip := a.AddrPort().Addr().Unmap().WithZone("")
	if !ip.IsValid() {
		return Device{}
	}
	return Device{MAC: loadMAC(ip.String()), Addr: ip}
}

type deviceKey struct {
	mac  string
	addr netip.Addr
}

func (k deviceKey) String() string {
	if k.mac != "" {
		return k.mac
	}
	return k.addr.String()
}

func (d Device) addrKey() (deviceKey, bool) {
	ip := d.Addr.Unmap().WithZone("")
	return deviceKey{addr: ip}, ip.IsValid()
}

func (d Device) key() (deviceKey, bool) {
	if d.MAC != "" {
		return deviceKey{mac: d.MAC}, true
	}
	return d.addrKey()
}

type deviceDomain struct {
	name  string
	count uint64
	seq   uint64
}

type deviceItem struct {
	count   uint64
	last    int64
	seq     uint64
	addr    netip.Addr
	domains [DeviceDomainsKept]deviceDomain
}

func (it *deviceItem) note(name string, n, seq uint64) {
	low := 0
	for i := range it.domains {
		slot := &it.domains[i]
		if slot.name == name {
			slot.count += n
			slot.seq = max(slot.seq, seq)
			return
		}
		if slot.name == "" {
			*slot = deviceDomain{name: strings.Clone(name), count: n, seq: seq}
			return
		}
		if l := &it.domains[low]; slot.count < l.count || (slot.count == l.count && slot.seq < l.seq) {
			low = i
		}
	}
	it.domains[low] = deviceDomain{name: strings.Clone(name), count: n, seq: seq}
}

func (it *deviceItem) absorb(o *deviceItem) {
	if o.seq > it.seq {
		it.addr = o.addr
	}
	it.count += o.count
	it.last = max(it.last, o.last)
	it.seq = max(it.seq, o.seq)
	for _, slot := range o.domains {
		if slot.name == "" {
			break
		}
		it.note(slot.name, slot.count, slot.seq)
	}
}

func (it *deviceItem) topDomains() []string {
	top := make([]topRow[string], 0, DeviceDomainsSent)
	for _, slot := range it.domains {
		if slot.name == "" {
			break
		}
		top = insertTop(top, topRow[string]{key: slot.name, count: slot.count, seq: slot.seq}, DeviceDomainsSent)
	}
	if len(top) == 0 {
		return nil
	}
	names := make([]string, len(top))
	for i, r := range top {
		names[i] = r.key
	}
	return names
}

type deviceState struct {
	mu    sync.Mutex
	rev   uint64
	seq   uint64
	items map[deviceKey]*deviceItem
}

func (s *deviceState) count(from Device, now int64) {
	k, ok := from.key()
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.entry(k, from)
	s.seq++
	it.count++
	it.last = now
	it.seq = s.seq
	if byAddr, ok := from.addrKey(); ok {
		it.addr = byAddr.addr
	}
	s.rev++
}

func (s *deviceState) entry(k deviceKey, from Device) *deviceItem {
	it := s.items[k]
	if k.mac != "" {
		if byAddr, ok := from.addrKey(); ok {
			it = s.adopt(k.mac, it, byAddr)
		}
	}
	if it != nil {
		return it
	}
	if s.items == nil {
		s.items = make(map[deviceKey]*deviceItem)
	}
	for len(s.items) >= DevicesKept {
		s.evict()
	}
	it = &deviceItem{}
	s.items[deviceKey{mac: strings.Clone(k.mac), addr: k.addr}] = it
	return it
}

func (s *deviceState) adopt(mac string, it *deviceItem, byAddr deviceKey) *deviceItem {
	prior, found := s.items[byAddr]
	if !found {
		return it
	}
	delete(s.items, byAddr)
	if it == nil {
		s.items[deviceKey{mac: strings.Clone(mac)}] = prior
		return prior
	}
	it.absorb(prior)
	return it
}

type learnedMAC struct {
	addr netip.Addr
	mac  string
}

func (s *deviceState) resolve() {
	s.mu.Lock()
	var addrs []netip.Addr
	for k := range s.items {
		if k.mac == "" {
			addrs = append(addrs, k.addr)
		}
	}
	s.mu.Unlock()
	var learned []learnedMAC
	for _, addr := range addrs {
		if mac := loadMAC(addr.String()); mac != "" {
			learned = append(learned, learnedMAC{addr: addr, mac: mac})
		}
	}
	if len(learned) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range learned {
		s.adopt(l.mac, s.items[deviceKey{mac: l.mac}], deviceKey{addr: l.addr})
	}
	s.rev++
}

func (s *deviceState) evict() {
	var victim deviceKey
	var worst *deviceItem
	for k, it := range s.items {
		if worst == nil || it.count < worst.count || (it.count == worst.count && it.seq < worst.seq) {
			victim, worst = k, it
		}
	}
	delete(s.items, victim)
}

func (s *deviceState) name(from Device, host string) {
	k, ok := from.key()
	if !ok {
		return
	}
	host = domainKey(host)
	if host == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.counted(k, from)
	if it == nil {
		return
	}
	s.seq++
	it.note(host, 1, s.seq)
	s.rev++
}

func (s *deviceState) counted(k deviceKey, from Device) *deviceItem {
	if it, ok := s.items[k]; ok {
		return it
	}
	if k.mac != "" {
		if byAddr, ok := from.addrKey(); ok {
			return s.adopt(k.mac, nil, byAddr)
		}
		return nil
	}
	if mac := loadMAC(k.addr.String()); mac != "" {
		return s.items[deviceKey{mac: mac}]
	}
	return nil
}

func (s *deviceState) reset() {
	s.mu.Lock()
	clear(s.items)
	s.rev++
	s.mu.Unlock()
}

func (s *deviceState) currentRev() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev
}

func (s *deviceState) list(off int64) *DeviceList {
	top := make([]topRow[deviceKey], 0, DevicesSent)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, it := range s.items {
		top = insertTop(top, topRow[deviceKey]{key: k, count: it.count, last: it.last, seq: it.seq}, DevicesSent)
	}
	items := make([]DeviceEntry, len(top))
	for i, r := range top {
		it := s.items[r.key]
		items[i] = DeviceEntry{Key: r.key.String(), Count: r.count, Last: wallMs(r.last, off), Domains: it.topDomains()}
		if r.key.mac != "" && it.addr.IsValid() {
			items[i].IP = it.addr.String()
		}
	}
	return &DeviceList{Rev: s.rev, Items: items}
}
