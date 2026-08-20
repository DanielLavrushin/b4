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

// SelfDialMark is carried by connections b4 opens on its own behalf - the
// MTProto upstream, an upstream SOCKS5, a fail-open direct dial.
const SelfDialMark = config.SelfDialMark

type routeState struct {
	mode        string
	mark        uint32
	table       int
	iface       string
	egressIP    string
	tproxyPort  int
	upstreamKey string
	sourcesKey  string
	deviceKey   string
	blockAction string
	quicReject  bool
	srcScoped   bool
	setV4       string
	setV6       string
	chainPre    string
	chainOut    string
	chainSNAT   string
	chainQUIC   string
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
	addInjectedMarkRule(chain string, v6 bool, setName string, mark, queueMark uint32)
	ensureJumpRule(baseChain, targetChain string, isMangle bool, atTop bool)
	deleteJumpRules(baseChain, targetChain string, isMangle bool)
	addMasqueradeRule(chain string, mark uint32, iface string, v6 bool)
	addSNATRule(chain, setName, iface, srcIP string, v6 bool)
	flushIPSet(name string)
	destroyIPSet(name string)
	clearAll()
}

const routeMaxLearnedHosts = 256

var (
	routeMu             sync.Mutex
	routeRuleCache      = make(map[string]routeState)
	routeIfaceAuto      = make(map[string]routeState)
	routeEngine         routeBackend
	routeLastReResolve  = make(map[string]time.Time)
	routeLearnLast      = make(map[string]time.Time)
	routeLearnedHosts   = make(map[string]map[string]time.Time)
	routeHostResolvedAt = make(map[string]time.Time)
	routeOwnedAddrs     = make(map[string]bool)
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
	if set.Targets.DomainOnly {
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
		if config.RoutingIsBlock(cur.mode) {
			err = routeEnsureBlockRule(be, cfg, set, cur, sources)
		} else if config.RoutingUsesTProxy(cur.mode) {
			err = routeEnsureProxyRule(be, cfg, set, cur, sources)
		} else {
			err = routeEnsureRule(be, cfg, set, cur, sources)
		}
		if err != nil {
			log.Errorf("Routing: failed to ensure rule for set '%s': %v", set.Name, err)
			return
		}
		routeRuleCache[set.Id] = cur
		switch cur.mode {
		case config.RoutingModeMTProtoWS:
			log.Infof("Routing [%s]: enabled MTProto-WS set '%s' mark=0x%x port=%d", be.name(), set.Name, cur.mark, cur.tproxyPort)
		case config.RoutingModeProxy:
			log.Infof("Routing [%s]: enabled proxy set '%s' -> %s:%d mark=0x%x port=%d", be.name(), set.Name, set.Routing.Upstream.Host, set.Routing.Upstream.Port, cur.mark, cur.tproxyPort)
		case config.RoutingModeBlock:
			log.Infof("Routing [%s]: enabled block set '%s' action=%s", be.name(), set.Name, cur.blockAction)
		default:
			log.Infof("Routing [%s]: enabled set '%s' -> iface=%s mark=0x%x table=%d", be.name(), set.Name, set.Routing.EgressInterface, cur.mark, cur.table)
		}
	}

	ttl := set.Routing.IPTTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}

	routeAddIPsToSets(be, cur, ttl, ips, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
}

func RoutingLearnIP(cfg *config.Config, set *config.SetConfig, ip net.IP) {
	if cfg == nil || set == nil || ip == nil || !set.Routing.Enabled {
		return
	}
	if config.RoutingIsBlock(set.Routing.Mode) {
		return
	}

	routeMu.Lock()
	defer routeMu.Unlock()

	st, ok := routeRuleCache[set.Id]
	if !ok {
		return
	}
	be := routeEngine
	if be == nil {
		return
	}

	ttl := set.Routing.IPTTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}

	now := time.Now()
	refresh := time.Duration(ttl) * time.Second / 2
	key := set.Id + "|" + ip.String()
	if last, seen := routeLearnLast[key]; seen && now.Sub(last) < refresh {
		return
	}
	routeLearnLast[key] = now

	if len(routeLearnLast) > 4096 {
		cutoff := time.Duration(ttl) * time.Second
		for k, t := range routeLearnLast {
			if now.Sub(t) > cutoff {
				delete(routeLearnLast, k)
			}
		}
	}

	routeAddIPsToSets(be, st, ttl, []net.IP{ip}, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
}

func RoutingLearnHost(cfg *config.Config, set *config.SetConfig, host string) {
	if cfg == nil || set == nil || !set.Routing.Enabled {
		return
	}
	if config.RoutingIsBlock(set.Routing.Mode) || set.Targets.DomainOnly {
		return
	}

	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || net.ParseIP(host) != nil {
		return
	}
	for _, d := range set.Targets.SNIDomains {
		if strings.ToLower(strings.TrimSpace(d)) == host {
			return
		}
	}

	ttl := set.Routing.IPTTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}

	routeMu.Lock()
	if _, ok := routeRuleCache[set.Id]; !ok {
		routeMu.Unlock()
		return
	}

	now := time.Now()
	refresh := time.Duration(ttl) * time.Second / 2
	key := set.Id + "|" + host
	if last, seen := routeHostResolvedAt[key]; seen && now.Sub(last) < refresh {
		routeMu.Unlock()
		return
	}
	routeHostResolvedAt[key] = now

	hosts := routeLearnedHosts[set.Id]
	if hosts == nil {
		hosts = make(map[string]time.Time)
		routeLearnedHosts[set.Id] = hosts
	}
	hosts[host] = now
	if len(hosts) > routeMaxLearnedHosts {
		oldest, oldestAt := "", now
		for h, t := range hosts {
			if !t.After(oldestAt) {
				oldest, oldestAt = h, t
			}
		}
		if oldest != "" {
			delete(hosts, oldest)
			delete(routeHostResolvedAt, set.Id+"|"+oldest)
		}
	}
	routeMu.Unlock()

	cfgSnapshot := *cfg
	go func(c *config.Config, s *config.SetConfig, h string) {
		if ips := routeResolveHost(c, h); len(ips) > 0 {
			log.Tracef("Routing: learned host %s -> %d IPs (set: %s)", h, len(ips), s.Name)
			routeAddResolvedIPs(c, s, ips)
		}
	}(&cfgSnapshot, set, host)
}

func routeAddResolvedIPs(cfg *config.Config, set *config.SetConfig, ips []net.IP) {
	if cfg == nil || set == nil || len(ips) == 0 {
		return
	}
	if config.RoutingIsBlock(set.Routing.Mode) || set.Targets.DomainOnly {
		return
	}

	routeMu.Lock()
	defer routeMu.Unlock()

	st, ok := routeRuleCache[set.Id]
	if !ok {
		return
	}
	be := routeEngine
	if be == nil {
		return
	}

	ttl := set.Routing.IPTTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}
	routeAddIPsToSets(be, st, ttl, ips, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
}

func routeResolveHost(cfg *config.Config, host string) []net.IP {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		log.Tracef("Routing: resolve %s failed: %v", host, err)
		return nil
	}

	resolved := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if a.IP.To4() != nil && !cfg.Queue.IPv4Enabled {
			continue
		}
		if a.IP.To4() == nil && !cfg.Queue.IPv6Enabled {
			continue
		}
		resolved = append(resolved, a.IP)
	}
	return resolved
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
		deviceKey:  routeSetDeviceGate(cfg, set).key(),
		srcScoped:  routeSetIsSourceScoped(set),
		setV4:      setV4, setV6: setV6,
		chainPre: chainPre, chainOut: chainOut, chainSNAT: chainSNAT,
		chainQUIC: routeBuildQUICChainName(set.Id),
	}

	if config.RoutingIsBlock(mode) {
		st.blockAction = config.NormalizeBlockAction(set.Routing.BlockAction)
	} else if config.RoutingUsesTProxy(mode) {
		mark, port := proxyMarkAndPort(set)
		st.mark = mark
		st.table = proxyTable()
		st.tproxyPort = port
		st.upstreamKey = fmt.Sprintf("%s:%d|%s|udp=%t", set.Routing.Upstream.Host, set.Routing.Upstream.Port, set.Routing.Upstream.Username, set.Routing.Upstream.UDP)
		st.quicReject = mode == config.RoutingModeProxy && !set.Routing.Upstream.UDP
	} else {
		mark, table := routeResolveIDs(cfg, set)
		st.mark = mark
		st.table = table
		st.iface = set.Routing.EgressInterface
		st.egressIP = set.Routing.EgressIP
	}
	return st
}

func routeStateEqual(a, b routeState) bool {
	return a.mode == b.mode &&
		a.mark == b.mark &&
		a.table == b.table &&
		a.iface == b.iface &&
		a.egressIP == b.egressIP &&
		a.tproxyPort == b.tproxyPort &&
		a.upstreamKey == b.upstreamKey &&
		a.blockAction == b.blockAction &&
		a.quicReject == b.quicReject &&
		a.sourcesKey == b.sourcesKey &&
		a.deviceKey == b.deviceKey
}

func routeCleanupAny(be routeBackend, st routeState) {
	if config.RoutingIsBlock(st.mode) {
		routeCleanupBlockRule(be, st)
		return
	}
	if config.RoutingUsesTProxy(st.mode) {
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
		for id, st := range routeRuleCache {
			routeCleanupAny(be, st)
			delete(routeRuleCache, id)
		}
		be.clearAll()
	}
	routeRuleCache = make(map[string]routeState)
	routeIfaceAuto = make(map[string]routeState)
	routeEngine = nil
	routeLastReResolve = make(map[string]time.Time)
	routeLearnLast = make(map[string]time.Time)
	routeLearnedHosts = make(map[string]map[string]time.Time)
	routeHostResolvedAt = make(map[string]time.Time)
	routeOwnedAddrs = make(map[string]bool)
}

func RoutingActiveIPSetNames(ipv4, ipv6 bool) []string {
	routeMu.Lock()
	defer routeMu.Unlock()

	seen := make(map[string]bool)
	var names []string
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	for _, st := range routeRuleCache {
		if config.RoutingIsBlock(st.mode) {
			continue
		}
		if ipv4 {
			add(st.setV4)
		}
		if ipv6 {
			add(st.setV6)
		}
	}
	sort.Strings(names)
	return names
}

func RoutingRulesPresent(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}

	routeMu.Lock()
	defer routeMu.Unlock()

	if len(routeRuleCache) == 0 {
		return true
	}

	be := getRouteBackend(cfg)
	if be == nil {
		return true
	}

	switch eng := be.(type) {
	case *routeNftBackend:
		return routeNftRulesPresent(cfg)
	case *routeIptBackend:
		return routeIptRulesPresent(eng, cfg)
	}
	return true
}

// parseNftRouteChains scans `nft list table inet b4_route` into the set of
// chains that exist and, per chain, the marks it returns on. Kept separate from
// the command so the brace handling can be tested against real output: a set's
// element list closes with a brace too, and mistaking that for the end of a
// chain would lose the rules that follow.
func parseNftRouteChains(out string) (present map[string]bool, bypass map[string]map[uint32]bool) {
	present = make(map[string]bool)
	bypass = make(map[string]map[uint32]bool)
	chain := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "chain "):
			chain = strings.TrimSpace(strings.TrimSuffix(line[len("chain "):], "{"))
			present[chain] = true
			continue
		case line == "}":
			chain = ""
			continue
		case chain == "":
			continue
		}
		if m, verb, ok := nftParseMarkRule(line); ok && verb == "return" {
			if bypass[chain] == nil {
				bypass[chain] = make(map[uint32]bool)
			}
			bypass[chain][m] = true
		}
	}
	return present, bypass
}

func routeNftRulesPresent(cfg *config.Config) bool {
	out, err := run("nft", "list", "table", "inet", routeNftTable)
	if err != nil || strings.TrimSpace(out) == "" {
		return false
	}

	present, bypass := parseNftRouteChains(out)

	for _, st := range routeRuleCache {
		for _, c := range routeStateChains(st) {
			if !present[c.chain] {
				return false
			}
			if !c.wantBypass {
				continue
			}
			for _, m := range routeBypassMarks(cfg) {
				if !bypass[c.chain][m] {
					log.Tracef("Routing: chain %s lost its bypass on mark 0x%x", c.chain, m)
					return false
				}
			}
		}
	}
	return true
}

type routeChainRef struct {
	chain, table string
	wantBypass   bool
}

func routeStateChains(st routeState) []routeChainRef {
	switch {
	case config.RoutingIsBlock(st.mode):
		return []routeChainRef{{st.chainPre, "filter", false}}
	case config.RoutingUsesTProxy(st.mode):
		refs := []routeChainRef{{st.chainPre, "mangle", true}}
		if st.quicReject && st.chainQUIC != "" {
			refs = append(refs, routeChainRef{st.chainQUIC, "filter", true})
		}
		return refs
	default:
		refs := []routeChainRef{{st.chainPre, "mangle", true}}
		if !st.srcScoped {
			refs = append(refs, routeChainRef{st.chainOut, "mangle", true})
		}
		return append(refs, routeChainRef{st.chainSNAT, "nat", false})
	}
}

// routeBypassMarks lists the marks a diverting chain must return on.
func routeBypassMarks(cfg *config.Config) []uint32 {
	return []uint32{routeQueueBypassMark(cfg), SelfDialMark}
}

func routeIptRulesPresent(be *routeIptBackend, cfg *config.Config) bool {
	needed := make(map[string]map[string]bool)
	for _, st := range routeRuleCache {
		for _, c := range routeStateChains(st) {
			if needed[c.table] == nil {
				needed[c.table] = make(map[string]bool)
			}
			needed[c.table][c.chain] = needed[c.table][c.chain] || c.wantBypass
		}
	}
	if len(needed) == 0 {
		return true
	}

	for _, v6 := range []bool{false, true} {
		if v6 && !cfg.Queue.IPv6Enabled {
			continue
		}
		if !v6 && !cfg.Queue.IPv4Enabled {
			continue
		}
		cmd := be.iptFor(v6)
		if !hasBinary(cmd) {
			continue
		}
		for table, wantChains := range needed {
			out, err := run(cmd, "-w", "-t", table, "-L", "-n")
			if err != nil {
				return false
			}
			present := make(map[string]bool)
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "Chain ") {
					fields := strings.Fields(line[len("Chain "):])
					if len(fields) > 0 {
						present[fields[0]] = true
					}
				}
			}
			for chain, wantBypass := range wantChains {
				if !present[chain] {
					return false
				}
				if !wantBypass {
					continue
				}
				spec, serr := run(cmd, "-w", "-t", table, "-S", chain)
				if serr != nil {
					continue
				}
				for _, m := range routeBypassMarks(cfg) {
					if !strings.Contains(spec, fmt.Sprintf("--mark 0x%x/0x%x -j RETURN", m, m)) {
						log.Tracef("Routing: chain %s lost its bypass on mark 0x%x", chain, m)
						return false
					}
				}
			}
		}
	}
	return true
}

func RoutingForceResync(cfg *config.Config) {
	if cfg == nil {
		return
	}

	routeMu.Lock()
	routeRuleCache = make(map[string]routeState)
	routeIfaceAuto = make(map[string]routeState)
	routeLastReResolve = make(map[string]time.Time)
	routeLearnLast = make(map[string]time.Time)
	routeHostResolvedAt = make(map[string]time.Time)
	routeMu.Unlock()

	RoutingSyncConfig(cfg)
}

func RoutingSyncConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}

	routeAsyncForgetAll()

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
		if len(set.Targets.IpsToMatch) == 0 && len(set.Targets.DomainsToMatch) == 0 {
			routeWarnNoDestination(set)
			continue
		}
		routeForgetNoDestinationWarning(set.Id)
		desired[set.Id] = set
	}

	for setID, st := range routeRuleCache {
		if _, ok := desired[setID]; !ok {
			routeCleanupAny(be, st)
			delete(routeRuleCache, setID)
			for host := range routeLearnedHosts[setID] {
				delete(routeHostResolvedAt, setID+"|"+host)
			}
			delete(routeLearnedHosts, setID)
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
			if config.RoutingIsBlock(cur.mode) {
				err = routeEnsureBlockRule(be, cfg, set, cur, sources)
			} else if config.RoutingUsesTProxy(cur.mode) {
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
		if config.RoutingUsesTProxy(st.mode) || st.iface == "" {
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
		if config.RoutingIsBlock(set.Routing.Mode) {
			continue
		}
		for _, domain := range routeResolveTargets(set) {
			resolved := routeResolveHost(cfg, domain)
			if len(resolved) > 0 {
				routeAddResolvedIPs(cfg, set, resolved)
				log.Tracef("Routing: pre-resolved %s -> %d IPs", domain, len(resolved))
			}
		}
	}
}

func routeResolveTargets(set *config.SetConfig) []string {
	seen := make(map[string]struct{})
	targets := make([]string, 0, len(set.Targets.SNIDomains))

	for _, domain := range set.Targets.SNIDomains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		targets = append(targets, domain)
	}

	routeMu.Lock()
	for host := range routeLearnedHosts[set.Id] {
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		targets = append(targets, host)
	}
	routeMu.Unlock()

	return targets
}

func routeEnsureRule(be routeBackend, cfg *config.Config, set *config.SetConfig, st routeState, sources []string) error {
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

	gate := routeSetDeviceGate(cfg, set)
	routeWarnDeviceGate(set.Name, gate)
	routeSelfDialBypass(be, cfg, st.chainPre)
	be.addBypassRule(st.chainPre, st.mark)

	routeAddBlacklistGate(be, "mangle", st.chainPre, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled, gate)

	if cfg.Queue.IPv4Enabled {
		routeAddMarkRules(be, st.chainPre, false, st.setV4, st.mark, sources, true)
	}
	if cfg.Queue.IPv6Enabled {
		routeAddMarkRules(be, st.chainPre, true, st.setV6, st.mark, sources, true)
	}

	sourceScoped := routeSetIsSourceScoped(set)
	if !sourceScoped {
		routeAddOutChainRules(be, cfg, st)
	}
	routeEnsureChainJumps(be, st, gate, sourceScoped)

	routeEnsureEgressAddress(st.iface, st.egressIP)
	routeAddEgressRules(be, st, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
	routeEnsurePolicyRouting(st, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
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

func routeAddOutChainRules(be routeBackend, cfg *config.Config, st routeState) {
	queueMark := routeQueueBypassMark(cfg)
	if cfg.Queue.IPv4Enabled {
		be.addInjectedMarkRule(st.chainOut, false, st.setV4, st.mark, queueMark)
	}
	if cfg.Queue.IPv6Enabled {
		be.addInjectedMarkRule(st.chainOut, true, st.setV6, st.mark, queueMark)
	}

	routeSelfDialBypass(be, cfg, st.chainOut)
	be.addBypassRule(st.chainOut, st.mark)

	if cfg.Queue.IPv4Enabled {
		routeAddMarkRules(be, st.chainOut, false, st.setV4, st.mark, nil, true)
	}
	if cfg.Queue.IPv6Enabled {
		routeAddMarkRules(be, st.chainOut, true, st.setV6, st.mark, nil, true)
	}
}

func routeEnsureChainJumps(be routeBackend, st routeState, gate routeDeviceGate, sourceScoped bool) {
	routeEnsureGatedPreJump(be, st.chainPre, gate)
	if sourceScoped {
		be.deleteJumpRules("OUTPUT", st.chainOut, true)
	} else {
		be.ensureJumpRule("OUTPUT", st.chainOut, true, true)
	}
	be.ensureJumpRule("POSTROUTING", st.chainSNAT, false, st.egressIP != "")
}

func routeEgressIPForFamily(egressIP string, v6 bool) string {
	if egressIP == "" {
		return ""
	}
	parsed := net.ParseIP(egressIP)
	if parsed == nil {
		return ""
	}
	if (parsed.To4() != nil) == v6 {
		return ""
	}
	return parsed.String()
}

func routeEgressIPOnIface(iface, egressIP string) bool {
	want := net.ParseIP(egressIP)
	if iface == "" || want == nil {
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
		if ok && ipNet.IP != nil && ipNet.IP.Equal(want) {
			return true
		}
	}
	return false
}

func routeEgressAddrKey(iface, ip string) string { return iface + "|" + ip }

func routeEgressAddrPrefix(ip net.IP) string {
	if ip.To4() != nil {
		return "/32"
	}
	return "/128"
}

func routeAddressAnsweredElsewhere(iface, ip string) bool {
	if !hasBinary("arping") {
		log.Warnf("Routing: arping is missing, so b4 cannot tell whether %s already belongs to another host before claiming it on %s", ip, iface)
		return false
	}
	_, err := run("arping", "-D", "-c", "2", "-w", "2", "-I", iface, ip)
	return err != nil
}

func routeEnsureEgressAddress(iface, egressIP string) bool {
	if iface == "" || egressIP == "" {
		return false
	}
	if routeEgressIPOnIface(iface, egressIP) {
		return true
	}
	parsed := net.ParseIP(egressIP)
	if parsed == nil || !hasBinary("ip") {
		return false
	}
	if parsed.To4() != nil && routeAddressAnsweredElsewhere(iface, egressIP) {
		log.Warnf("Routing: another host on %s already answers for %s, so claiming it would break that host; masquerading this set instead", iface, egressIP)
		return false
	}
	if _, err := run("ip", "addr", "add", egressIP+routeEgressAddrPrefix(parsed), "dev", iface); err != nil {
		log.Warnf("Routing: could not put egress IP %s on %s (%v); masquerading this set instead", egressIP, iface, err)
		return false
	}
	routeOwnedAddrs[routeEgressAddrKey(iface, egressIP)] = true
	log.Infof("Routing: egress IP %s added to %s; b4 keeps it there while a set uses it and removes it afterwards", egressIP, iface)
	return true
}

func routeReleaseEgressAddress(iface, egressIP string) {
	key := routeEgressAddrKey(iface, egressIP)
	if !routeOwnedAddrs[key] {
		return
	}
	delete(routeOwnedAddrs, key)
	parsed := net.ParseIP(egressIP)
	if parsed == nil || !hasBinary("ip") {
		return
	}
	runLogged("routing: remove egress IP "+egressIP, "ip", "addr", "del", egressIP+routeEgressAddrPrefix(parsed), "dev", iface)
}

func routeUsableEgressIP(st routeState, iface string, v6 bool) string {
	src := routeEgressIPForFamily(st.egressIP, v6)
	if src == "" || !routeEgressIPOnIface(iface, src) {
		return ""
	}
	return src
}

func routeAddEgressRules(be routeBackend, st routeState, ipv4, ipv6 bool) {
	emit := func(v6 bool, setName string) {
		src := routeEgressIPForFamily(st.egressIP, v6)
		if src == "" {
			be.addMasqueradeRule(st.chainSNAT, st.mark, st.iface, v6)
			return
		}
		if !routeEgressIPOnIface(st.iface, src) {
			log.Warnf("Routing: egress IP %s is not on %s, so nothing would answer for the rewritten source and every reply would be lost; masquerading this set instead", src, st.iface)
			be.addMasqueradeRule(st.chainSNAT, st.mark, st.iface, v6)
			return
		}
		be.addSNATRule(st.chainSNAT, setName, st.iface, src, v6)
	}
	if ipv4 {
		emit(false, st.setV4)
	}
	if ipv6 {
		emit(true, st.setV6)
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
		if config.RoutingUsesTProxy(st.mode) {
			continue
		}
		if st.mark == mark && st.table == table {
			n++
		}
	}
	return n
}

func routeCleanupRule(be routeBackend, st routeState) {
	routeReleaseEgressAddress(st.iface, st.egressIP)
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

func routeEnsurePolicyRouting(st routeState, ipv4, ipv6 bool) {
	iface, mark, table := st.iface, st.mark, st.table
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
	if src := routeUsableEgressIP(st, iface, false); src != "" {
		ifaceV4 = src
	}
	if src := routeUsableEgressIP(st, iface, true); src != "" {
		ifaceV6 = src
	}
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
		if config.RoutingUsesTProxy(st.mode) || st.iface != iface {
			continue
		}
		routeEnsurePolicyRouting(st, ipv4, ipv6)
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
		if q := routeQueueBypassMark(cfg); set.Routing.FWMark&q == q {
			log.Warnf("Routing: set '%s' asks for fwmark 0x%x, which carries every bit of the queue mark 0x%x; b4 cannot tell such a packet from one it injected itself, so a mark is assigned instead",
				set.Name, set.Routing.FWMark, q)
		} else {
			return set.Routing.FWMark, set.Routing.Table
		}
	}
	if st, ok := routeIfaceAuto[set.Routing.EgressInterface]; ok && st.mark > 0 && st.table > 0 {
		return st.mark, st.table
	}

	usedMarks := map[uint32]struct{}{}
	usedTables := map[int]struct{}{}
	if cfg != nil {
		usedMarks[routeQueueBypassMark(cfg)] = struct{}{}
		usedMarks[SelfDialMark] = struct{}{}
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

const (
	routeMaxChainNameLen = 28
	routeMaxIPSetNameLen = 31
)

func routeBuildSetNames(setID string) (string, string) {
	s := routeSanitizeSetID(setID)
	return "b4r_" + s + "_v4", "b4r_" + s + "_v6"
}

func routeBuildChainNames(setID string) (string, string, string) {
	s := routeSanitizeSetID(setID)
	return "b4r_" + s + "_pre", "b4r_" + s + "_out", "b4r_" + s + "_nat"
}

func routeBuildQUICChainName(setID string) string {
	return "b4r_" + routeSanitizeSetID(setID) + "_q"
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

// routeSelfDialBypass emits the two RETURNs a routing chain needs on b4's own
// traffic: the queue mark, and the self-dial mark.
//
// They cannot be one value. The queue mark is also what the packet engine puts
// on everything it reinjects, so the mangle chains accept it to keep reinjected
// packets from being queued again - which means a connection b4 opens itself,
// marked to keep it out of its own TPROXY, skips b4's own DPI bypass as well and
// leaves the machine naked at a censored destination. Proven against a Telegram
// data center from a censored network: mark 0x0 connected in 114 ms, mark 0x8000
// timed out. The self-dial mark is returned by the routing chains and by nothing
// else, so those connections stay out of the TPROXY loop and still get
// fragmentation, fake packets and desync applied.
func routeSelfDialBypass(be routeBackend, cfg *config.Config, chain string) {
	be.addBypassRule(chain, routeQueueBypassMark(cfg))
	be.addBypassRule(chain, SelfDialMark)
}
