package tables

import (
	"context"
	"fmt"
	"hash/fnv"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const hostRouteCTMark = uint32(0x40000000)

type routeState struct {
	mode        string
	mark        uint32
	table       int
	iface       string
	tproxyPort  int
	upstreamKey string
	sourcesKey  string
	setV4       string
	setV6       string
	chainPre    string
	chainOut    string
	chainSNAT   string
}

type routeBackend interface {
	name() string
	available() bool
	ensureBase() error
	ensureIPSet(name string, v6 bool) error
	addElements(setName string, ips []string, ttlSec int)
	ensureChain(chain string, isMangle bool) error
	flushChain(chain string, isMangle bool)
	deleteChain(chain string, isMangle bool)
	addBypassRule(chain string, mark uint32)
	addMarkRule(chain string, v6 bool, setName string, mark uint32, sourceIface string, tagHostConntrack bool)
	ensureJumpRule(baseChain, targetChain string, isMangle bool)
	deleteJumpRules(baseChain, targetChain string, isMangle bool)
	addMasqueradeRule(chain string, mark uint32, iface string, v6 bool)
	flushIPSet(name string)
	destroyIPSet(name string)
	clearAll()
}

var (
	routeMu            sync.Mutex
	routeRuleCache     = make(map[string]routeState)
	routeIfaceAuto     = make(map[string]routeState)
	routeEngine        routeBackend
	routeLastReResolve = make(map[string]time.Time)
)

func getRouteBackend(cfg *config.Config) routeBackend {
	if routeEngine != nil {
		return routeEngine
	}
	be := detectFirewallBackend(cfg)
	nft := &routeNftBackend{}
	ipt := &routeIptBackend{legacy: be == backendIPTablesLegacy}
	switch be {
	case backendNFTables:
		if nft.available() {
			routeEngine = nft
		}
	default:
		if ipt.available() {
			routeEngine = ipt
		}
	}
	if routeEngine == nil && nft.available() {
		routeEngine = nft
	} else if routeEngine == nil && ipt.available() {
		routeEngine = ipt
	}
	return routeEngine
}

func RoutingHandleDNS(cfg *config.Config, set *config.SetConfig, ips []net.IP) {
	if cfg == nil || set == nil || !set.Routing.Enabled || len(ips) == 0 {
		return
	}
	mode := set.Routing.Mode
	if mode == "" {
		mode = config.RoutingModeInterface
	}
	if mode == config.RoutingModeInterface && set.Routing.EgressInterface == "" {
		return
	}
	if !hasBinary("ip") {
		log.Tracef("Routing: ip binary is missing, skipping")
		return
	}

	routeMu.Lock()
	defer routeMu.Unlock()

	be := getRouteBackend(cfg)
	if be == nil {
		log.Tracef("Routing: no firewall backend available (need nft or iptables+ipset)")
		return
	}

	if err := be.ensureBase(); err != nil {
		log.Errorf("Routing: failed to ensure base (%s): %v", be.name(), err)
		return
	}

	cur := buildRouteState(cfg, set)
	sources := routeNormalizedSources(set.Routing.SourceInterfaces)

	if old, ok := routeRuleCache[set.Id]; ok {
		if !routeStateEqual(old, cur) {
			routeCleanupAny(be, old)
			delete(routeRuleCache, set.Id)
		}
	}

	if _, ok := routeRuleCache[set.Id]; !ok {
		var err error
		if cur.mode == config.RoutingModeProxy {
			err = routeEnsureProxyRule(be, cfg, set, cur, sources)
		} else {
			err = routeEnsureRule(be, cfg, set, cur, sources)
		}
		if err != nil {
			log.Errorf("Routing: failed to ensure rule for set '%s': %v", set.Name, err)
			return
		}
		routeRuleCache[set.Id] = cur
		if cur.mode == config.RoutingModeProxy {
			log.Infof("Routing [%s]: enabled proxy set '%s' -> %s:%d mark=0x%x port=%d", be.name(), set.Name, set.Routing.Upstream.Host, set.Routing.Upstream.Port, cur.mark, cur.tproxyPort)
		} else {
			log.Infof("Routing [%s]: enabled set '%s' -> iface=%s mark=0x%x table=%d", be.name(), set.Name, set.Routing.EgressInterface, cur.mark, cur.table)
		}
	}

	ttl := set.Routing.IPTTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}

	routeAddIPsToSets(be, cur, ttl, ips, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
}

func buildRouteState(cfg *config.Config, set *config.SetConfig) routeState {
	mode := set.Routing.Mode
	if mode == "" {
		mode = config.RoutingModeInterface
	}
	sources := routeNormalizedSources(set.Routing.SourceInterfaces)
	sourcesKey := strings.Join(sources, ",")
	setV4, setV6 := routeBuildSetNames(set.Id)
	chainPre, chainOut, chainSNAT := routeBuildChainNames(set.Id)

	st := routeState{
		mode:       mode,
		sourcesKey: sourcesKey,
		setV4:      setV4, setV6: setV6,
		chainPre: chainPre, chainOut: chainOut, chainSNAT: chainSNAT,
	}

	if mode == config.RoutingModeProxy {
		mark, port := proxyMarkAndPort(set)
		st.mark = mark
		st.table = proxyTable()
		st.tproxyPort = port
		st.upstreamKey = fmt.Sprintf("%s:%d|%s", set.Routing.Upstream.Host, set.Routing.Upstream.Port, set.Routing.Upstream.Username)
	} else {
		mark, table := routeResolveIDs(cfg, set)
		st.mark = mark
		st.table = table
		st.iface = set.Routing.EgressInterface
	}
	return st
}

func routeStateEqual(a, b routeState) bool {
	return a.mode == b.mode &&
		a.mark == b.mark &&
		a.table == b.table &&
		a.iface == b.iface &&
		a.tproxyPort == b.tproxyPort &&
		a.upstreamKey == b.upstreamKey &&
		a.sourcesKey == b.sourcesKey
}

func routeCleanupAny(be routeBackend, st routeState) {
	if st.mode == config.RoutingModeProxy {
		routeCleanupProxyRule(be, st)
		return
	}
	routeCleanupRule(be, st)
}

func routeAddIPsToSets(be routeBackend, st routeState, ttl int, ips []net.IP, ipv4Enabled, ipv6Enabled bool) {
	v4 := make([]string, 0, len(ips))
	v6 := make([]string, 0, len(ips))
	seen4 := make(map[string]struct{}, len(ips))
	seen6 := make(map[string]struct{}, len(ips))

	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			if !ipv4Enabled {
				continue
			}
			s := ip4.String()
			if _, ok := seen4[s]; ok {
				continue
			}
			seen4[s] = struct{}{}
			v4 = append(v4, s)
			continue
		}
		if ip6 := ip.To16(); ip6 != nil {
			if !ipv6Enabled {
				continue
			}
			s := ip6.String()
			if _, ok := seen6[s]; ok {
				continue
			}
			seen6[s] = struct{}{}
			v6 = append(v6, s)
		}
	}

	if len(v4) > 0 {
		be.addElements(st.setV4, v4, ttl)
	}
	if len(v6) > 0 {
		be.addElements(st.setV6, v6, ttl)
	}
}

func routeCollectEntries(set *config.SetConfig) (v4, v6 []string) {
	if set == nil || len(set.Targets.IpsToMatch) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(set.Targets.IpsToMatch))

	for _, raw := range set.Targets.IpsToMatch {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var entry string
		var isV6 bool

		if strings.Contains(raw, "/") {
			ip, ipNet, err := net.ParseCIDR(raw)
			if err != nil || ip == nil || ipNet == nil {
				continue
			}
			entry = ipNet.String()
			isV6 = ip.To4() == nil
		} else {
			ip := net.ParseIP(raw)
			if ip == nil {
				continue
			}
			entry = ip.String()
			isV6 = ip.To4() == nil
		}

		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}

		if isV6 {
			v6 = append(v6, entry)
		} else {
			v4 = append(v4, entry)
		}
	}

	return v4, v6
}

func RoutingClearAll() {
	routeMu.Lock()
	defer routeMu.Unlock()

	be := routeEngine
	if be == nil {
		nft := &routeNftBackend{}
		if nft.available() {
			nft.clearAll()
		}
		for _, legacy := range []bool{false, true} {
			ipt := &routeIptBackend{legacy: legacy}
			if hasBinary(ipt.ipt4()) || hasBinary(ipt.ipt6()) {
				ipt.clearAll()
			}
		}
	} else {
		for _, st := range routeRuleCache {
			routeCleanupAny(be, st)
		}
		be.clearAll()
	}
	routeRuleCache = make(map[string]routeState)
	routeIfaceAuto = make(map[string]routeState)
	routeEngine = nil
	routeLastReResolve = make(map[string]time.Time)
}

func RoutingSyncConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}

	routeMu.Lock()
	defer routeMu.Unlock()

	be := getRouteBackend(cfg)
	if be == nil {
		log.Tracef("Routing: no firewall backend available, skipping sync")
		routeRuleCache = make(map[string]routeState)
		routeIfaceAuto = make(map[string]routeState)
		return
	}

	if !hasBinary("ip") {
		log.Tracef("Routing: ip binary is missing, skipping sync")
		routeRuleCache = make(map[string]routeState)
		routeIfaceAuto = make(map[string]routeState)
		return
	}

	if err := be.ensureBase(); err != nil {
		log.Errorf("Routing: failed to ensure base during sync (%s): %v", be.name(), err)
		return
	}

	desired := make(map[string]*config.SetConfig, len(cfg.Sets))
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.Routing.Enabled {
			continue
		}
		mode := set.Routing.Mode
		if mode == "" {
			mode = config.RoutingModeInterface
		}
		if mode == config.RoutingModeInterface && set.Routing.EgressInterface == "" {
			continue
		}
		if mode == config.RoutingModeProxy && set.Routing.Upstream.Port < 1 {
			continue
		}
		desired[set.Id] = set
	}

	for setID, st := range routeRuleCache {
		if _, ok := desired[setID]; !ok {
			routeCleanupAny(be, st)
			delete(routeRuleCache, setID)
		}
	}

	var newRoutingSets []*config.SetConfig
	for _, set := range cfg.Sets {
		if set == nil {
			continue
		}
		if _, ok := desired[set.Id]; !ok {
			continue
		}

		cur := buildRouteState(cfg, set)
		sources := routeNormalizedSources(set.Routing.SourceInterfaces)

		if old, ok := routeRuleCache[set.Id]; ok {
			if !routeStateEqual(old, cur) {
				routeCleanupAny(be, old)
				delete(routeRuleCache, set.Id)
			}
		}

		if _, ok := routeRuleCache[set.Id]; !ok {
			var err error
			if cur.mode == config.RoutingModeProxy {
				err = routeEnsureProxyRule(be, cfg, set, cur, sources)
			} else {
				err = routeEnsureRule(be, cfg, set, cur, sources)
			}
			if err != nil {
				log.Errorf("Routing: failed to ensure rule for set '%s' during sync: %v", set.Name, err)
				continue
			}
			routeRuleCache[set.Id] = cur
			newRoutingSets = append(newRoutingSets, set)
		}

		staticV4, staticV6 := routeCollectEntries(set)
		if cfg.Queue.IPv4Enabled && len(staticV4) > 0 {
			be.addElements(cur.setV4, staticV4, 0)
		}
		if cfg.Queue.IPv6Enabled && len(staticV6) > 0 {
			be.addElements(cur.setV6, staticV6, 0)
		}
	}

	routeIfaceAuto = make(map[string]routeState)
	for _, st := range routeRuleCache {
		if st.mode == config.RoutingModeProxy || st.iface == "" {
			continue
		}
		if _, ok := routeIfaceAuto[st.iface]; !ok {
			routeIfaceAuto[st.iface] = routeState{mark: st.mark, table: st.table}
		}
	}

	if len(newRoutingSets) > 0 {
		cfgSnapshot := *cfg
		go routePreResolveDomains(&cfgSnapshot, newRoutingSets)
	}
}

func RoutingPeriodicReResolve(cfg *config.Config) {
	if cfg == nil {
		return
	}

	routeMu.Lock()
	if len(routeRuleCache) == 0 {
		routeMu.Unlock()
		return
	}

	var setsToResolve []*config.SetConfig
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.Routing.Enabled {
			continue
		}
		mode := set.Routing.Mode
		if mode == "" {
			mode = config.RoutingModeInterface
		}
		if mode == config.RoutingModeInterface && set.Routing.EgressInterface == "" {
			continue
		}
		if _, ok := routeRuleCache[set.Id]; !ok {
			continue
		}
		if len(set.Targets.SNIDomains) == 0 {
			continue
		}
		ttl := set.Routing.IPTTLSeconds
		if ttl <= 0 {
			ttl = 3600
		}
		interval := time.Duration(ttl) * time.Second / 2
		if interval < 5*time.Minute {
			interval = 5 * time.Minute
		}
		last := routeLastReResolve[set.Id]
		if time.Since(last) < interval {
			continue
		}
		setsToResolve = append(setsToResolve, set)
	}
	if len(setsToResolve) == 0 {
		routeMu.Unlock()
		return
	}
	now := time.Now()
	for _, set := range setsToResolve {
		routeLastReResolve[set.Id] = now
	}
	routeMu.Unlock()

	cfgSnapshot := *cfg
	go routePreResolveDomains(&cfgSnapshot, setsToResolve)
}

func routePreResolveDomains(cfg *config.Config, sets []*config.SetConfig) {
	for _, set := range sets {
		for _, domain := range set.Targets.SNIDomains {
			domain = strings.TrimSpace(domain)
			if domain == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, domain)
			cancel()
			if err != nil {
				log.Tracef("Routing: pre-resolve %s failed: %v", domain, err)
				continue
			}
			resolved := make([]net.IP, 0, len(ips))
			for _, ip := range ips {
				if ip.IP.To4() != nil && !cfg.Queue.IPv4Enabled {
					continue
				}
				if ip.IP.To4() == nil && !cfg.Queue.IPv6Enabled {
					continue
				}
				resolved = append(resolved, ip.IP)
			}
			if len(resolved) > 0 {
				RoutingHandleDNS(cfg, set, resolved)
				log.Tracef("Routing: pre-resolved %s -> %d IPs", domain, len(resolved))
			}
		}
	}
}

func routeEnsureRule(be routeBackend, cfg *config.Config, set *config.SetConfig, st routeState, sources []string) error {
	if cfg.Queue.IPv4Enabled {
		if err := be.ensureIPSet(st.setV4, false); err != nil {
			return err
		}
	}
	if cfg.Queue.IPv6Enabled {
		if err := be.ensureIPSet(st.setV6, true); err != nil {
			return err
		}
	}

	if err := be.ensureChain(st.chainPre, true); err != nil {
		return err
	}
	if err := be.ensureChain(st.chainOut, true); err != nil {
		return err
	}
	if err := be.ensureChain(st.chainSNAT, false); err != nil {
		return err
	}

	be.flushChain(st.chainPre, true)
	be.flushChain(st.chainOut, true)
	be.flushChain(st.chainSNAT, false)

	queueMark := routeQueueBypassMark(cfg)
	be.addBypassRule(st.chainPre, queueMark)
	be.addBypassRule(st.chainOut, queueMark)
	be.addBypassRule(st.chainPre, st.mark)
	be.addBypassRule(st.chainOut, st.mark)

	if cfg.Queue.IPv4Enabled {
		routeAddMarkRules(be, st.chainPre, false, st.setV4, st.mark, sources, true)
		routeAddMarkRules(be, st.chainOut, false, st.setV4, st.mark, nil, true)
	}
	if cfg.Queue.IPv6Enabled {
		routeAddMarkRules(be, st.chainPre, true, st.setV6, st.mark, sources, true)
		routeAddMarkRules(be, st.chainOut, true, st.setV6, st.mark, nil, true)
	}

	be.ensureJumpRule("PREROUTING", st.chainPre, true)
	be.ensureJumpRule("OUTPUT", st.chainOut, true)
	be.ensureJumpRule("POSTROUTING", st.chainSNAT, false)

	routeAddMasqueradeRules(be, set.Routing.EgressInterface, st.chainSNAT, st.mark, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
	routeEnsurePolicyRouting(set.Routing.EgressInterface, st.mark, st.table, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
	return nil
}

func routeAddMarkRules(be routeBackend, chain string, v6 bool, setName string, mark uint32, sources []string, tagHostCT bool) {
	if len(sources) == 0 {
		be.addMarkRule(chain, v6, setName, mark, "", tagHostCT)
		return
	}
	for _, src := range sources {
		be.addMarkRule(chain, v6, setName, mark, src, tagHostCT)
	}
}

func routeAddMasqueradeRules(be routeBackend, iface, chain string, mark uint32, ipv4, ipv6 bool) {
	if ipv4 {
		be.addMasqueradeRule(chain, mark, iface, false)
	}
	if ipv6 {
		be.addMasqueradeRule(chain, mark, iface, true)
	}
}

func interfaceShareCount(mark uint32, table int) int {
	n := 0
	for _, st := range routeRuleCache {
		if st.mode == config.RoutingModeProxy {
			continue
		}
		if st.mark == mark && st.table == table {
			n++
		}
	}
	return n
}

func routeCleanupRule(be routeBackend, st routeState) {
	markStr := fmt.Sprintf("0x%x", st.mark)
	markStrMask := fmt.Sprintf("0x%x/0x%x", st.mark, st.mark)
	tableStr := fmt.Sprintf("%d", st.table)
	if hasBinary("ip") && interfaceShareCount(st.mark, st.table) <= 1 {
		routeDelRuleLoop(false, markStr, tableStr)
		routeDelRuleLoop(false, markStrMask, tableStr)
		routeDelRuleLoop(true, markStr, tableStr)
		routeDelRuleLoop(true, markStrMask, tableStr)
		runLogged("routing: flush route table v4", "ip", "route", "flush", "table", tableStr)
		runLogged("routing: flush route table v6", "ip", "-6", "route", "flush", "table", tableStr)
	}

	be.deleteJumpRules("PREROUTING", st.chainPre, true)
	be.deleteJumpRules("OUTPUT", st.chainOut, true)
	be.deleteJumpRules("POSTROUTING", st.chainSNAT, false)

	be.flushChain(st.chainPre, true)
	be.deleteChain(st.chainPre, true)
	be.flushChain(st.chainOut, true)
	be.deleteChain(st.chainOut, true)
	be.flushChain(st.chainSNAT, false)
	be.deleteChain(st.chainSNAT, false)

	be.flushIPSet(st.setV4)
	be.destroyIPSet(st.setV4)
	be.flushIPSet(st.setV6)
	be.destroyIPSet(st.setV6)
}

func routeEnsurePolicyRouting(iface string, mark uint32, table int, ipv4, ipv6 bool) {
	prio := 10000 + table
	markStr := fmt.Sprintf("0x%x", mark)
	markStrMask := fmt.Sprintf("0x%x/0x%x", mark, mark)
	tableStr := fmt.Sprintf("%d", table)
	prioStr := fmt.Sprintf("%d", prio)

	if ipv4 {
		routeDelRuleLoop(false, markStr, tableStr)
		routeDelRuleLoop(false, markStrMask, tableStr)
		runLogged("routing: add ip rule v4", "ip", "rule", "add", "fwmark", markStrMask, "lookup", tableStr, "priority", prioStr)
	}
	if ipv6 {
		routeDelRuleLoop(true, markStr, tableStr)
		routeDelRuleLoop(true, markStrMask, tableStr)
		runLogged("routing: add ip rule v6", "ip", "-6", "rule", "add", "fwmark", markStrMask, "lookup", tableStr, "priority", prioStr)
	}

	if _, err := net.InterfaceByName(iface); err != nil {
		log.Infof("Routing: interface %s not present (%v); default route deferred until it appears", iface, err)
		return
	}

	ifaceV4 := routeGetIfaceAddr(iface, false)
	ifaceV6 := routeGetIfaceAddr(iface, true)
	if ipv4 {
		routeReplaceDefaultRoute(iface, ifaceV4, tableStr, false)
	}
	if ipv6 {
		routeReplaceDefaultRoute(iface, ifaceV6, tableStr, true)
	}
}

func RoutingReinstallForInterface(cfg *config.Config, iface string) {
	if cfg == nil || iface == "" || !hasBinary("ip") {
		return
	}
	if _, err := net.InterfaceByName(iface); err != nil {
		log.Tracef("Routing: interface %s no longer present; skipping reinstall", iface)
		return
	}
	routeMu.Lock()
	defer routeMu.Unlock()

	ipv4 := cfg.Queue.IPv4Enabled
	ipv6 := cfg.Queue.IPv6Enabled
	count := 0
	for _, st := range routeRuleCache {
		if st.mode == config.RoutingModeProxy || st.iface != iface {
			continue
		}
		routeEnsurePolicyRouting(st.iface, st.mark, st.table, ipv4, ipv6)
		count++
	}
	if count > 0 {
		log.Infof("Routing: reinstalled policy routes for interface %s (%d set(s))", iface, count)
	}
}

func routeReplaceDefaultRoute(iface, src, table string, ipv6 bool) {
	family := "v4"
	ipCmd := []string{"ip"}
	if ipv6 {
		family = "v6"
		ipCmd = append(ipCmd, "-6")
	}

	if gw := routeDefaultGatewayForIface(iface, ipv6); gw != "" {
		args := append([]string{}, ipCmd...)
		args = append(args, "route", "replace", "default", "via", gw, "dev", iface)
		if src != "" {
			args = append(args, "src", src)
		}
		args = append(args, "table", table)
		runLogged("routing: add ip route "+family+" (via gw)", args...)
		return
	}

	args := append([]string{}, ipCmd...)
	args = append(args, "route", "replace", "default", "dev", iface)
	if src != "" {
		args = append(args, "src", src)
	}
	args = append(args, "table", table)
	runLogged("routing: add ip route "+family+" (direct)", args...)
}

func routeDefaultGatewayForIface(iface string, ipv6 bool) string {
	args := []string{"ip"}
	if ipv6 {
		args = append(args, "-6")
	} else {
		args = append(args, "-4")
	}
	args = append(args, "route", "show", "default", "dev", iface)
	out, err := run(args...)
	if err != nil {
		log.Tracef("Routing: gateway lookup failed for %s: %v", iface, err)
	} else {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			for i := 0; i+1 < len(fields); i++ {
				if fields[i] == "via" {
					return fields[i+1]
				}
			}
		}
	}

	// fallback
	if gw := routeMainDefaultGateway(ipv6); gw != "" && ifaceReachesIP(iface, gw) {
		return gw
	}
	return ""
}

func routeMainDefaultGateway(ipv6 bool) string {
	args := []string{"ip"}
	if ipv6 {
		args = append(args, "-6")
	} else {
		args = append(args, "-4")
	}
	args = append(args, "route", "show", "default")
	out, err := run(args...)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "via" {
				return fields[i+1]
			}
		}
	}
	return ""
}

func ifaceReachesIP(iface, ip string) bool {
	target := net.ParseIP(ip)
	if target == nil {
		return false
	}
	ifaceObj, err := net.InterfaceByName(iface)
	if err != nil {
		return false
	}
	addrs, err := ifaceObj.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet == nil {
			continue
		}
		if ipNet.Contains(target) {
			return true
		}
	}
	return false
}

func routeGetIfaceAddr(iface string, wantV6 bool) string {
	ifaceObj, err := net.InterfaceByName(iface)
	if err != nil {
		return ""
	}
	addrs, err := ifaceObj.Addrs()
	if err != nil {
		return ""
	}
	best := ""
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		ip := ipNet.IP
		if wantV6 {
			if ip.To4() != nil {
				continue
			}

			if !ip.IsGlobalUnicast() {
				continue
			}
			return ip.String()
		} else {
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			if ip4.IsGlobalUnicast() {
				return ip4.String()
			}
			if best == "" {
				best = ip4.String()
			}
		}
	}
	return best
}

func routeResolveIDs(cfg *config.Config, set *config.SetConfig) (uint32, int) {
	if set.Routing.FWMark > 0 && set.Routing.Table > 0 {
		return set.Routing.FWMark, set.Routing.Table
	}
	if st, ok := routeIfaceAuto[set.Routing.EgressInterface]; ok && st.mark > 0 && st.table > 0 {
		return st.mark, st.table
	}

	usedMarks := map[uint32]struct{}{}
	usedTables := map[int]struct{}{}
	if cfg != nil {
		usedMarks[routeQueueBypassMark(cfg)] = struct{}{}
	}
	for _, st := range routeRuleCache {
		if st.mark > 0 {
			usedMarks[st.mark] = struct{}{}
		}
		if st.table > 0 {
			usedTables[st.table] = struct{}{}
		}
	}
	for _, st := range routeIfaceAuto {
		if st.mark > 0 {
			usedMarks[st.mark] = struct{}{}
		}
		if st.table > 0 {
			usedTables[st.table] = struct{}{}
		}
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(set.Routing.EgressInterface))
	base := h.Sum32()

	for attempt := uint32(0); attempt < 4096; attempt++ {
		table := 100 + int((base+attempt)%150)
		mark := uint32(0x100 + (base+attempt)%0x7E00)
		if _, ok := usedMarks[mark]; ok {
			continue
		}
		if _, ok := usedTables[table]; ok {
			continue
		}
		routeIfaceAuto[set.Routing.EgressInterface] = routeState{mark: mark, table: table}
		return mark, table
	}

	mark := uint32(0x66)
	table := 100
	for i := 0; i < 4096; i++ {
		_, markUsed := usedMarks[mark]
		_, tableUsed := usedTables[table]
		if !markUsed && !tableUsed {
			break
		}
		mark++
		table++
		if table > 249 {
			table = 100
		}
	}
	routeIfaceAuto[set.Routing.EgressInterface] = routeState{mark: mark, table: table}
	return mark, table
}

func routeDelRuleLoop(ipv6 bool, mark, table string) {
	for i := 0; i < 100; i++ {
		var err error
		if ipv6 {
			_, err = run("ip", "-6", "rule", "del", "fwmark", mark, "lookup", table)
		} else {
			_, err = run("ip", "rule", "del", "fwmark", mark, "lookup", table)
		}
		if err != nil {
			return
		}
	}
}

func routeNormalizedSources(sources []string) []string {
	if len(sources) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func routeBuildSetNames(setID string) (string, string) {
	s := routeSanitizeSetID(setID)
	return "b4r_" + s + "_v4", "b4r_" + s + "_v6"
}

func routeBuildChainNames(setID string) (string, string, string) {
	s := routeSanitizeSetID(setID)
	return "b4r_" + s + "_pre", "b4r_" + s + "_out", "b4r_" + s + "_nat"
}

func routeSanitizeSetID(setID string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(setID) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		}
	}
	s := b.String()
	if s == "" {
		s = "default"
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(setID))
	suffix := fmt.Sprintf("_%x", h.Sum32()%0xFFFF)
	maxPrefix := 20 - len(suffix)
	if len(s) > maxPrefix {
		s = s[:maxPrefix]
	}
	return s + suffix
}

func routeQueueBypassMark(cfg *config.Config) uint32 {
	if cfg == nil || cfg.Queue.Mark == 0 {
		return 0x8000
	}
	return uint32(cfg.Queue.Mark)
}
