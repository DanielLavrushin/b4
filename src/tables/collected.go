package tables

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

var routeCollectedApplied = make(map[string]map[string][]string)

func routePrepareCollected(be routeBackend, cfg *config.Config, set *config.SetConfig, st routeState) error {
	prev := routeCollectedApplied[st.setID]
	next := make(map[string][]string, 2)
	var failure error
	for _, family := range []struct {
		v6      bool
		enabled bool
		name    string
	}{{false, st.ipv4, st.setV4}, {true, st.ipv6, st.setV6}} {
		name := family.name + "c"
		recorded, wasRecorded := prev[name]
		if !set.Targets.UseCollectedIPs || !family.enabled {
			if !wasRecorded {
				continue
			}
			if len(recorded) > 0 {
				routeDropCollectedSet(be, name, false)
			}
			next[name] = nil
			continue
		}
		if failure != nil {
			if wasRecorded {
				next[name] = recorded
			}
			continue
		}
		if err := routeEnsureCollectedSet(be, name, family.v6); err != nil {
			if wasRecorded {
				next[name] = recorded
			}
			failure = err
			continue
		}
		entries := routeCollectedFamilyEntries(set, family.v6)
		if gone := routeEntriesGone(recorded, entries); len(gone) > 0 {
			be.delElements(name, gone)
		}
		next[name] = entries
		if failed := be.addElements(name, entries, 0); len(failed) > 0 {
			failure = fmt.Errorf("collected guard membership failed for %s", set.Name)
		}
	}
	if len(next) == 0 {
		delete(routeCollectedApplied, st.setID)
	} else {
		routeCollectedApplied[st.setID] = next
	}
	return failure
}

func routeEnsureCollectedSet(be routeBackend, name string, v6 bool) error {
	if plain, ok := be.(interface {
		ensureCollectedSet(name string, v6 bool) error
	}); ok {
		return plain.ensureCollectedSet(name, v6)
	}
	return be.ensureIPSet(name, v6)
}

func routeDropCollectedSet(be routeBackend, name string, destroy bool) {
	if _, ok := be.(*routeNftBackend); ok {
		runLogged("routing: flush set "+name, "nft", "flush", "set", "inet", routeNftTable, name)
		if destroy {
			runLogged("routing: delete set "+name, "nft", "delete", "set", "inet", routeNftTable, name)
		}
		return
	}
	be.flushIPSet(name)
	if destroy {
		be.destroyIPSet(name)
	}
}

func routeCollectedFamilyEntries(set *config.SetConfig, v6 bool) []string {
	version := uint8(4)
	if v6 {
		version = 6
	}
	var entries []string
	for _, raw := range set.Targets.CollectedIPs {
		ip, err := netip.ParseAddr(raw)
		if err == nil && ip.Is6() == v6 && set.MatchesIPVersion(version) {
			entries = append(entries, raw)
		}
	}
	return entries
}

type routeCollectedGuardChain struct {
	chain   string
	sources []string
}

func routeCollectedGuardChains(be routeBackend, st routeState) []routeCollectedGuardChain {
	if !st.collected {
		return nil
	}
	pre := routeCollectedGuardChain{chain: st.chainPre, sources: st.sources}
	if config.RoutingIsBlock(st.mode) || config.RoutingUsesTProxy(st.mode) && st.srcScoped {
		return []routeCollectedGuardChain{pre}
	}
	return []routeCollectedGuardChain{pre, {chain: st.chainOut}}
}

func routeCollectedGuard(be routeBackend, cfg *config.Config, set *config.SetConfig, st routeState, chain string) error {
	for _, guard := range routeCollectedGuardChains(be, st) {
		if guard.chain == chain {
			return routeCollectedGuardRules(be, cfg, set, st, chain, guard.sources, func(args []string) error { _, err := run(args...); return err })
		}
	}
	return nil
}

func routeCollectedGuardRules(be routeBackend, cfg *config.Config, set *config.SetConfig, st routeState, chain string, sources []string, emit func([]string) error) error {
	if !set.Targets.UseCollectedIPs {
		return nil
	}
	if len(sources) == 0 {
		sources = []string{""}
	}
	proxy := config.RoutingUsesTProxy(st.mode)
	nftRestored := make(map[string]bool, len(sources))
	for _, family := range []struct {
		v6      bool
		enabled bool
		name    string
	}{{false, st.ipv4, st.setV4}, {true, st.ipv6, st.setV6}} {
		if !family.enabled {
			continue
		}
		for _, src := range sources {
			mark := st.mark
			if proxy && chain == st.chainOut {
				mark |= config.RouterOwnProxyMarkBit
			}
			if be.name() == backendNFTables {
				field := "ip"
				if family.v6 {
					field = "ip6"
				}
				scope := []string{"nft", "add", "rule", "inet", routeNftTable, chain}
				if src != "" {
					scope = append(scope, "iifname", fmt.Sprintf("%q", src))
				}
				var ports []string
				if proxy {
					ports = proxyNftPorts(proxyModePorts(st.mode))
				}
				base := append(append(append([]string{}, scope...), field, "daddr", "@"+family.name+"c"), ports...)
				if proxy {
					args := append(append([]string{}, base...), "meta", "l4proto", "tcp", "tcp", "flags", "&", "(syn|ack)", "==", "syn", "ct", "state", "new", "ct", "mark", "set", "ct", "mark", "&", fmt.Sprintf("0x%x", ^routeSetMarkMask), "or", fmt.Sprintf("0x%x", st.mark))
					if err := emit(args); err != nil {
						return err
					}
					if !nftRestored[src] {
						nftRestored[src] = true
						args = append(append(append([]string{}, scope...), ports...), "ct", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "==", fmt.Sprintf("0x%x", st.mark), "meta", "mark", "set", fmt.Sprintf("0x%x", mark))
						if err := emit(args); err != nil {
							return err
						}
					}
				}
				args := append(append([]string{}, base...), "ct", "state", "!=", "new", "ct", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "!=", fmt.Sprintf("0x%x", st.mark), "counter", "return")
				if config.RoutingIsBlock(st.mode) {
					args = append(append([]string{}, base...), "ct", "state", "!=", "new", "counter", "return")
				}
				if err := emit(args); err != nil {
					return err
				}
			} else {
				ipt, ok := be.(*routeIptBackend)
				if !ok {
					return fmt.Errorf("unsupported collected backend")
				}
				tableName := "mangle"
				if config.RoutingIsBlock(st.mode) {
					tableName = "filter"
				}
				scope := []string{ipt.iptFor(family.v6), "-w", "-t", tableName, "-A", chain}
				if src != "" {
					scope = append(scope, "-i", src)
				}
				var ports []string
				if proxy {
					if modePorts := proxyModePorts(st.mode); len(modePorts) > 0 {
						ports = append([]string{"-p", "tcp"}, proxyIptPorts(modePorts)...)
					}
				}
				base := append(append(append([]string{}, scope...), "-m", "set", "--match-set", family.name+"c", "dst"), ports...)
				if proxy {
					tagBase := append([]string{}, base...)
					if len(ports) == 0 {
						tagBase = append(tagBase, "-p", "tcp")
					}
					args := append(tagBase, "--syn", "-m", "conntrack", "--ctstate", "NEW", "-j", "CONNMARK", "--set-xmark", fmt.Sprintf("0x%x/0x%x", st.mark, routeSetMarkMask))
					if err := emit(args); err != nil {
						return err
					}
					args = append(append(append([]string{}, scope...), ports...), "-m", "connmark", "--mark", fmt.Sprintf("0x%x/0x%x", st.mark, routeSetMarkMask), "-j", "MARK", "--set-xmark", fmt.Sprintf("0x%x/0x%x", mark, routeProxyMarkMask))
					if err := emit(args); err != nil {
						return err
					}
				}
				args := append(append([]string{}, base...), "-m", "conntrack", "!", "--ctstate", "NEW", "-m", "connmark", "!", "--mark", fmt.Sprintf("0x%x/0x%x", st.mark, routeSetMarkMask), "-j", "RETURN")
				if config.RoutingIsBlock(st.mode) {
					args = append(append([]string{}, base...), "-m", "conntrack", "!", "--ctstate", "NEW", "-j", "RETURN")
				}
				if err := emit(args); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func CollectedTargetsReady(cfg *config.Config) error {
	routeMu.Lock()
	defer routeMu.Unlock()
	if cfg == nil || cfg.System.Tables.SkipSetup {
		return fmt.Errorf("firewall setup unavailable")
	}
	if routeSyncOutcome.err != "" {
		return fmt.Errorf("routing: %s", routeSyncOutcome.err)
	}
	for _, set := range cfg.RoutingSets() {
		if !set.Enabled || !set.Routing.Enabled || !set.Targets.UseCollectedIPs || len(set.Targets.CollectedIPs) == 0 {
			continue
		}
		st, ok := routeRuleCache[set.Id]
		if !ok || routeEngine == nil {
			return fmt.Errorf("consumer %q not installed", set.Name)
		}
		if failed, ok := routeSyncOutcome.setErrs[set.Id]; ok {
			return fmt.Errorf("consumer %q: %s", set.Name, failed.Error)
		}
		v4, v6 := routeCollectEntries(set)
		if st.ipv4 && len(routeEngine.addElements(st.setV4, v4, 0)) > 0 || st.ipv6 && len(routeEngine.addElements(st.setV6, v6, 0)) > 0 {
			return fmt.Errorf("consumer %q destination membership failed", set.Name)
		}
	}
	return nil
}

var collectedAccounting = SysctlSetting{Name: "net.netfilter.nf_conntrack_acct", Desired: "1"}

func applyCollectedAccounting(cfg *config.Config) {
	for _, set := range cfg.Sets {
		if set != nil && set.Enabled && set.TCP.IPBlockDetect.Collects() && !cfg.Queue.IsDiscovery && cfg.Queue.Mode != "tun" {
			collectedAccounting.Apply()
			return
		}
	}
	revertCollectedAccounting()
}

func revertCollectedAccounting() {
	if _, saved := loadSysctlSnapshot()[collectedAccounting.Name]; saved {
		collectedAccounting.RevertBack()
	}
}

func routeCollectedGuardsPresent(be routeBackend, cfg *config.Config) bool {
	for _, st := range routeRuleCache {
		if !st.collected || st.set == nil {
			continue
		}
		for _, family := range []struct {
			on   bool
			name string
		}{{st.ipv4, st.setV4}, {st.ipv6, st.setV6}} {
			if family.on && !be.hasIPSet(family.name+"c") {
				return false
			}
		}
		for _, guard := range routeCollectedGuardChains(be, st) {
			if be.name() == backendNFTables {
				out, err := run("nft", "list", "chain", "inet", routeNftTable, guard.chain)
				if err != nil {
					return false
				}
				for _, family := range []struct {
					on   bool
					name string
				}{{st.ipv4, st.setV4}, {st.ipv6, st.setV6}} {
					if !family.on {
						continue
					}
					found := false
					for _, line := range strings.Split(out, "\n") {
						if strings.Contains(line, "@"+family.name+"c") && strings.Contains(line, "ct state != new") && strings.Contains(line, "return") {
							found = true
							break
						}
					}
					if !found {
						return false
					}
				}
			} else {
				err := routeCollectedGuardRules(be, cfg, st.set, st, guard.chain, guard.sources, func(args []string) error {
					check := append([]string(nil), args...)
					for i, arg := range check {
						if arg == "-A" {
							check[i] = "-C"
							break
						}
					}
					_, err := run(check...)
					return err
				})
				if err != nil {
					return false
				}
			}
		}
	}
	return true
}

func collectedReplyObservation(cfg *config.Config) bool {
	if cfg == nil || cfg.Queue.IsDiscovery || cfg.Queue.Mode == "tun" {
		return false
	}
	for _, set := range cfg.Sets {
		if set != nil && set.Enabled && set.TCP.IPBlockDetect.Collects() && !set.Targets.UseCollectedIPs {
			return true
		}
	}
	return false
}
