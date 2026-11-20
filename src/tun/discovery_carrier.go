package tun

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/tables"
)

const (
	carrierDeviceName         = "b4dsc0"
	carrierDeviceAltName      = "b4dsc1"
	carrierGuardPrio          = 80
	carrierFlowPrio           = 81
	carrierInjectedMainPrio   = 82
	carrierInjectedUplinkPrio = 83
	carrierTableFirst         = 60
	carrierTableFloor         = 32
	carrierExactMask          = "0xffffffff"
	carrierReservedMarkBits   = uint(engine.TunSteerMark | engine.ClientMark | engine.ReinjectMarkBit)
)

var (
	carrierReconcileEvery = 5 * time.Second
	carrierDeviceExists   = func(name string) bool {
		_, err := run("ip", "link", "show", "dev", name)
		return err == nil
	}
	openCarrierDevice = openTUN
	newCarrierSender  = func(mark int) (ipv4Sender, error) {
		s, err := sock.NewSenderWithMark(mark)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	disableCarrierIPv6 = func(name string) {
		if err := os.WriteFile("/proc/sys/net/ipv6/conf/"+name+"/disable_ipv6", []byte("1\n"), 0644); err != nil {
			log.Tracef("TUN: could not disable IPv6 on %s: %v", name, err)
		}
	}
)

type ipv4Sender interface {
	SendIPv4(packet []byte, destIP net.IP) error
	Close()
}

type DiscoveryCarrierRequest struct {
	FlowMark     uint
	InjectedMark uint
	Worker       *nfq.Worker
	Lost         func(error)
}

type carrierRule struct {
	prio     int
	mark     uint
	lookup   string
	suppress bool
}

func (c carrierRule) args(action string) []string {
	args := []string{"ip", "rule", action, "fwmark", fmt.Sprintf("0x%x/%s", c.mark, carrierExactMask), "lookup", c.lookup}
	if c.suppress {
		args = append(args, "suppress_prefixlength", "0")
	}
	return append(args, "priority", strconv.Itoa(c.prio))
}

func (c carrierRule) presentIn(ruleShow string) bool {
	for _, line := range strings.Split(ruleShow, "\n") {
		prio, ok := rulePriority(line)
		if !ok || prio != c.prio || !exactMarkMatches(ruleFieldValue(line, "fwmark"), c.mark) {
			continue
		}
		if !lookupMatches(ruleFieldValue(line, "lookup"), c.lookup) {
			continue
		}
		if c.suppress != strings.Contains(line, "suppress_prefixlength 0") {
			continue
		}
		return true
	}
	return false
}

func lookupMatches(lookup, want string) bool {
	if isLocalLookup(want) {
		return isLocalLookup(lookup)
	}
	return tables.RouteLookupMatchesTable(lookup, want)
}

func exactMarkMatches(fw string, mark uint) bool {
	want := fmt.Sprintf("0x%x", mark)
	value, mask, masked := strings.Cut(fw, "/")
	return value == want && (!masked || mask == carrierExactMask)
}

func DiscoveryDeviceName(cfg *config.Config) string {
	if cfg != nil && cfg.Queue.TUN.Device() == carrierDeviceName {
		return carrierDeviceAltName
	}
	return carrierDeviceName
}

type carrierRouting struct {
	src          string
	capture      string
	routeTable   int
	captureTable int
	explicit     int
	pinned       []int
	outIface     string
}

func (r *routeManager) carrierRouting() carrierRouting {
	r.mu.Lock()
	defer r.mu.Unlock()
	return carrierRouting{
		src:          r.srcIP,
		capture:      r.resolvedCapture,
		routeTable:   r.routeTable,
		captureTable: r.captureTable,
		explicit:     r.explicitTable,
		pinned:       append([]int(nil), r.pinnedTables...),
		outIface:     r.outIface,
	}
}

func (r *routeManager) carrierMarksCaptured(flow, injected uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolvedCapture != "ports" {
		return nil
	}
	for _, mark := range []uint{flow, injected, injected | engine.ReinjectMarkBit} {
		if !discoveryMarkReturned(r.mark, r.discoveryFlow, r.discoveryInjected, mark) {
			return fmt.Errorf("the Discovery marks were changed during the run, and %s no longer returns the run's mark 0x%x, so its packets would enter %s", tunCaptureChain, mark, r.tunName)
		}
	}
	return nil
}

func (cr carrierRouting) engineTables() map[int]bool {
	avoid := map[int]bool{xrayUITproxyTable: true}
	for _, t := range []int{cr.routeTable, cr.captureTable, captureTableFor(cr.routeTable), cr.explicit} {
		if t > 0 {
			avoid[t] = true
		}
	}
	if cr.explicit > 0 {
		avoid[captureTableFor(cr.explicit)] = true
	}
	return avoid
}

func pickCarrierTable(u tableUsage, avoid map[int]bool) (int, error) {
	for t := carrierTableFirst; t >= carrierTableFloor; t-- {
		if avoid[t] || u.takenBy(t) != "" {
			continue
		}
		return t, nil
	}
	return 0, fmt.Errorf("every routing table from %d to %d is reserved or already in use", carrierTableFloor, carrierTableFirst)
}

func carrierRules(ruleShow string, flow, injected uint, table int, cr carrierRouting) []carrierRule {
	tableStr := strconv.Itoa(table)
	wholeDefault := cr.capture != "ports"
	var rules []carrierRule
	if localGuardNeeded(ruleShow, carrierFlowPrio) {
		rules = append(rules, carrierRule{prio: carrierGuardPrio, mark: flow, lookup: "local"})
		if wholeDefault {
			rules = append(rules, carrierRule{prio: carrierGuardPrio, mark: injected, lookup: "local"})
		}
	}
	rules = append(rules, carrierRule{prio: carrierFlowPrio, mark: flow, lookup: tableStr})
	if wholeDefault {
		rules = append(rules,
			carrierRule{prio: carrierInjectedMainPrio, mark: injected, lookup: "main", suppress: true},
			carrierRule{prio: carrierInjectedUplinkPrio, mark: injected, lookup: strconv.Itoa(cr.routeTable)},
		)
	}
	return rules
}

func isCarrierPriority(prio int) bool {
	return prio >= carrierGuardPrio && prio <= carrierInjectedUplinkPrio
}

func planCarrierSweep(ruleShow string, flow, injected uint, avoid map[int]bool) (dels [][]string, flush []string) {
	doomed := map[string]bool{}
	var kept []string
	for _, line := range strings.Split(ruleShow, "\n") {
		lookup := ruleFieldValue(line, "lookup")
		if lookup == "" {
			continue
		}
		prio, ok := rulePriority(line)
		fw := ruleFieldValue(line, "fwmark")
		ours := ok && isCarrierPriority(prio) && ruleMatchesAllTraffic(strings.Fields(line)) &&
			(exactMarkMatches(fw, flow) || exactMarkMatches(fw, injected))
		if !ours {
			kept = append(kept, lookup)
			continue
		}
		args := []string{"ip", "rule", "del", "priority", strconv.Itoa(prio), "fwmark", fw, "lookup", lookup}
		if strings.Contains(line, "suppress_prefixlength 0") {
			args = append(args, "suppress_prefixlength", "0")
		}
		dels = append(dels, args)
		if n, err := strconv.Atoi(lookup); err == nil && prio == carrierFlowPrio && !isBuiltinTable(lookup) && !avoid[n] {
			doomed[lookup] = true
		}
	}
	for _, l := range kept {
		delete(doomed, l)
	}
	for l := range doomed {
		flush = append(flush, l)
	}
	sort.Strings(flush)
	return dels, flush
}

func sweepDiscoveryCarrier(name string, flow, injected uint, avoid map[int]bool) int {
	removed := 0
	if out, err := run("ip", "rule", "show"); err == nil {
		dels, flush := planCarrierSweep(out, flow, injected, avoid)
		for _, d := range dels {
			if _, err := run(d...); err != nil {
				log.Tracef("TUN: stale Discovery carrier rule not removed: %v", err)
				continue
			}
			removed++
		}
		for _, table := range flush {
			if _, err := run("ip", "route", "flush", "table", table); err != nil {
				log.Tracef("TUN: Discovery carrier table %s not flushed: %v", table, err)
			}
		}
	}
	if interfaceExists(name) && isTunDevice(name) {
		if _, err := run("ip", "link", "del", name); err == nil {
			removed++
		}
	}
	return removed
}

func (e *Engine) carrierDevice() string {
	if e.carrierName != "" {
		return e.carrierName
	}
	return DiscoveryDeviceName(e.config())
}

func ClearStaleDiscoveryCarrier(cfg *config.Config) {
	if n := sweepDiscoveryCarrier(DiscoveryDeviceName(cfg), cfg.DiscoveryFlowMark(), cfg.DiscoveryInjectedMark(), nil); n > 0 {
		log.Infof("TUN: removed %d Discovery carrier leftover(s) from a previous run", n)
	}
}

type DiscoveryCarrier struct {
	name     string
	file     io.ReadCloser
	proc     packetProcessor
	send     ipv4Sender
	routes   *routeManager
	rules    []carrierRule
	table    int
	src      string
	flow     uint
	injected uint
	lost     func(error)
	lostOnce sync.Once
	quit     chan struct{}
	laneWG   sync.WaitGroup
	checkWG  sync.WaitGroup
	stopOnce sync.Once

	processed atomic.Uint64
	dropped   atomic.Uint64
	nonIPv4   atomic.Uint64
	sendErrs  atomic.Uint64
	lastErr   atomic.Int64
}

func (e *Engine) StartDiscoveryCarrier(req DiscoveryCarrierRequest) (*DiscoveryCarrier, error) {
	if req.Worker == nil {
		return nil, errors.New("no Discovery worker to carry packets to")
	}
	return e.startDiscoveryCarrier(req, req.Worker)
}

func (e *Engine) startDiscoveryCarrier(req DiscoveryCarrierRequest, proc packetProcessor) (*DiscoveryCarrier, error) {
	select {
	case <-e.quit:
		return nil, errors.New("the TUN engine is stopping")
	default:
	}
	if bits := (req.FlowMark | req.InjectedMark) & carrierReservedMarkBits; bits != 0 {
		return nil, fmt.Errorf("Discovery's flow mark 0x%x or injected mark 0x%x has bits 0x%x of the marks the TUN engine reserves (0x%x steer, 0x%x client, 0x%x reinject), which would send its probes or the packets the carrier re-sends past the carrier; choose Discovery marks without them",
			req.FlowMark, req.InjectedMark, bits, uint(engine.TunSteerMark), uint(engine.ClientMark), uint(engine.ReinjectMarkBit))
	}
	if e.routes == nil {
		return nil, errors.New("the TUN engine has no routing set up")
	}
	cr := e.routes.carrierRouting()
	if cr.src == "" {
		return nil, fmt.Errorf("the TUN engine has no source address for uplink %q, and without one the probes would leave with an address the uplink does not route back", cr.outIface)
	}
	name := e.carrierDevice()
	if interfaceExists(name) && !isTunDevice(name) {
		return nil, fmt.Errorf("interface %s exists and is not a TUN device; refusing to delete it", name)
	}
	avoid := cr.engineTables()
	if n := sweepDiscoveryCarrier(name, req.FlowMark, req.InjectedMark, avoid); n > 0 {
		log.Infof("TUN: removed %d Discovery carrier leftover(s) from a previous run", n)
	}
	table, err := pickCarrierTable(readTableUsage(cr.pinned), avoid)
	if err != nil {
		return nil, err
	}

	c := &DiscoveryCarrier{
		name:     name,
		proc:     proc,
		routes:   e.routes,
		table:    table,
		src:      cr.src,
		flow:     req.FlowMark,
		injected: req.InjectedMark,
		lost:     req.Lost,
		quit:     make(chan struct{}),
	}
	if err := c.setup(e, req, cr); err != nil {
		c.Stop()
		return nil, err
	}
	log.Infof("TUN: Discovery carrier %s is up (source %s, table %d, ip rule priority %d, %d rule(s))", c.name, c.src, c.table, carrierFlowPrio, len(c.rules))
	return c, nil
}

func (c *DiscoveryCarrier) setup(e *Engine, req DiscoveryCarrierRequest, cr carrierRouting) error {
	f, name, err := openCarrierDevice(c.name)
	if err != nil {
		return fmt.Errorf("open %s: %w", c.name, err)
	}
	c.file = f
	c.name = name
	disableCarrierIPv6(name)

	mtu := interfaceMTU(e.tunName)
	if mtu <= 0 {
		mtu = interfaceMTU(cr.outIface)
	}
	if mtu <= 0 {
		mtu = 1500
	}
	if _, err := run("ip", "link", "set", name, "mtu", strconv.Itoa(mtu)); err != nil {
		log.Warnf("TUN: could not set MTU %d on %s: %v", mtu, name, err)
	}
	if _, err := run("ip", "link", "set", name, "up"); err != nil {
		return fmt.Errorf("ip link set %s up: %w", name, err)
	}
	tableStr := strconv.Itoa(c.table)
	if _, err := run("ip", "route", "replace", "default", "dev", name, "src", c.src, "table", tableStr); err != nil {
		return routingError(fmt.Sprintf("ip route replace default (Discovery carrier, table %d)", c.table), c.table, err)
	}

	sender, err := newCarrierSender(int(req.InjectedMark) | engine.ReinjectMarkBit)
	if err != nil {
		return fmt.Errorf("Discovery carrier sender: %w", err)
	}
	c.send = sender

	ruleShow, err := run("ip", "rule", "show")
	if err != nil {
		return fmt.Errorf("ip rule show: %w", err)
	}
	for _, rule := range carrierRules(ruleShow, req.FlowMark, req.InjectedMark, c.table, cr) {
		if _, err := run(rule.args("add")...); err != nil {
			if rule.suppress {
				log.Warnf("TUN: this iproute2 rejected the rule that lets Discovery's DNS queries reach your own networks (%v), so a query to a server on the LAN leaves by the uplink", err)
				continue
			}
			return routingError(fmt.Sprintf("ip rule add (Discovery carrier, priority %d)", rule.prio), c.table, err)
		}
		c.rules = append(c.rules, rule)
	}

	c.laneWG.Add(1)
	go c.lane()
	c.checkWG.Add(1)
	go c.reconcileLoop()
	return nil
}

func (c *DiscoveryCarrier) lane() {
	defer c.laneWG.Done()
	buf := make([]byte, tunBufSize)
	for {
		n, err := c.file.Read(buf)
		if err != nil {
			select {
			case <-c.quit:
				return
			default:
			}
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
				return
			}
			if !carrierDeviceExists(c.name) {
				c.reportLost(fmt.Errorf("the Discovery carrier device %s is gone", c.name))
				return
			}
			c.noteError("read error", err)
			time.Sleep(10 * time.Millisecond)
			continue
		}
		c.handle(buf[:n])
	}
}

func (c *DiscoveryCarrier) handle(raw []byte) {
	if len(raw) < 20 || raw[0]>>4 != 4 {
		c.nonIPv4.Add(1)
		return
	}
	c.processed.Add(1)
	if c.proc.ProcessPacketDir(raw, engine.DirOriginal) != engine.VerdictAccept {
		c.dropped.Add(1)
		return
	}
	if err := c.send.SendIPv4(raw, net.IP(raw[16:20])); err != nil {
		c.sendErrs.Add(1)
		c.noteError(fmt.Sprintf("could not send a probe packet on (%s -> %s)", net.IP(raw[12:16]), net.IP(raw[16:20])), err)
	}
}

func (c *DiscoveryCarrier) noteError(what string, err error) {
	now := time.Now().Unix()
	last := c.lastErr.Load()
	if now-last >= 5 && c.lastErr.CompareAndSwap(last, now) {
		log.Warnf("TUN: Discovery carrier %s: %s (%d send errors so far): %v", c.name, what, c.sendErrs.Load(), err)
	}
}

func (c *DiscoveryCarrier) reconcileLoop() {
	defer c.checkWG.Done()
	ticker := time.NewTicker(carrierReconcileEvery)
	defer ticker.Stop()
	for {
		select {
		case <-c.quit:
			return
		case <-ticker.C:
		}
		err := c.check()
		select {
		case <-c.quit:
			return
		default:
		}
		if err != nil {
			c.reportLost(err)
			return
		}
	}
}

func (c *DiscoveryCarrier) check() error {
	if err := c.routes.carrierMarksCaptured(c.flow, c.injected); err != nil {
		return err
	}
	if !carrierDeviceExists(c.name) {
		return fmt.Errorf("the Discovery carrier device %s is gone", c.name)
	}
	if now := c.routes.currentSrcIP(); now != c.src {
		return fmt.Errorf("the uplink source address changed from %s to %q during the run", c.src, now)
	}
	routes, err := run("ip", "route", "show", "table", strconv.Itoa(c.table))
	if err != nil || !carrierRoutePresent(routes, c.name, c.src) {
		return fmt.Errorf("the default route of Discovery carrier table %d (dev %s src %s) was removed or changed", c.table, c.name, c.src)
	}
	ruleShow, err := run("ip", "rule", "show")
	if err != nil {
		return fmt.Errorf("ip rule show: %w", err)
	}
	for _, rule := range c.rules {
		if !rule.presentIn(ruleShow) {
			return fmt.Errorf("the Discovery carrier's ip rule at priority %d (fwmark 0x%x lookup %s) was removed", rule.prio, rule.mark, rule.lookup)
		}
	}
	return nil
}

func carrierRoutePresent(routes, dev, src string) bool {
	for _, line := range strings.Split(routes, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		if extractField(line, "dev") == dev && extractField(line, "src") == src {
			return true
		}
	}
	return false
}

func (c *DiscoveryCarrier) reportLost(err error) {
	if c.lost == nil {
		return
	}
	c.lostOnce.Do(func() {
		log.Warnf("TUN: %v", err)
		c.lost(err)
	})
}

func (c *DiscoveryCarrier) Stop() {
	c.stopOnce.Do(func() {
		close(c.quit)
		c.checkWG.Wait()
		for i := len(c.rules) - 1; i >= 0; i-- {
			if _, err := run(c.rules[i].args("del")...); err != nil {
				log.Warnf("TUN: could not remove the Discovery carrier rule at priority %d: %v", c.rules[i].prio, err)
			}
		}
		c.rules = nil
		if c.file != nil {
			c.file.Close()
		}
		c.laneWG.Wait()
		if c.table > 0 {
			if _, err := run("ip", "route", "flush", "table", strconv.Itoa(c.table)); err != nil {
				log.Tracef("TUN: Discovery carrier table %d not flushed: %v", c.table, err)
			}
		}
		if c.file != nil && interfaceExists(c.name) && isTunDevice(c.name) {
			if _, err := run("ip", "link", "del", c.name); err != nil {
				log.Warnf("TUN: could not delete %s: %v", c.name, err)
			}
		}
		if c.send != nil {
			c.send.Close()
		}
		log.Infof("TUN: Discovery carrier %s stopped (%d probe packets processed, %d dropped by the strategy, %d non-IPv4 dropped, %d send errors)",
			c.name, c.processed.Load(), c.dropped.Load(), c.nonIPv4.Load(), c.sendErrs.Load())
	})
}
