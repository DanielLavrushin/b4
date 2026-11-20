package tables

import (
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type captureCollectedSet struct {
	setID   string
	v6      bool
	entries []string
	rules   []Rule
}

var captureCollectedApplied = map[string]*captureCollectedSet{}

type captureCollectedPool struct {
	addrs [2][]string
	known [2]bool
}

var captureCollectedSynced *captureCollectedPool

func captureCollectedPoolOf(cfg *config.Config) *captureCollectedPool {
	pool := &captureCollectedPool{}
	seen := make(map[string]bool)
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.Targets.UseCollectedIPs || cfg.Queue.IsDiscovery {
			continue
		}
		for family, version := range [2]uint8{4, 6} {
			if cfg.IPFamilyEnabled(version == 6) && set.MatchesIPVersion(version) {
				pool.known[family] = true
			}
		}
		for _, raw := range set.Targets.CollectedIPs {
			ip, err := netip.ParseAddr(raw)
			if err != nil || seen[raw] {
				continue
			}
			seen[raw] = true
			family := 0
			if ip.Is6() {
				family = 1
			}
			pool.addrs[family] = append(pool.addrs[family], raw)
		}
	}
	return pool
}

func captureCollectedFilled(cfg *config.Config) *config.Config {
	synced := captureCollectedSynced
	if synced == nil {
		return cfg
	}
	current := captureCollectedPoolOf(cfg)
	var ips []string
	for family := range synced.addrs {
		if synced.known[family] {
			ips = append(ips, synced.addrs[family]...)
		} else {
			ips = append(ips, current.addrs[family]...)
		}
	}
	return cfg.WithCollectedIPs(ips)
}

func captureCollectedMSSName(setIdx int, v6 bool) string {
	if v6 {
		return fmt.Sprintf("b4_mss_%d_v6c", setIdx)
	}
	return fmt.Sprintf("b4_mss_%d_v4c", setIdx)
}

func captureCollectedDupName(v6 bool) string {
	if v6 {
		return "b4_dup_v6c"
	}
	return "b4_dup_v4c"
}

func captureCollectedEmitted(cfg *config.Config) bool {
	return cfg.Queue.Mode != "tun" && !config.RunningEngineTUN()
}

func captureCollectedMSSEntries(cfg *config.Config, e config.SetMSSClampEntry, v6 bool) []string {
	set := mssClampEntrySet(captureCollectedFilled(cfg), e)
	if set == nil {
		return nil
	}
	return routeCollectedFamilyEntries(set, v6)
}

func captureCollectedDupEntries(cfg *config.Config, v6 bool) ([]string, bool) {
	v4s, v6s, used := cfg.CollectedDuplicateIPs()
	if v6 {
		return v6s, used
	}
	return v4s, used
}

func captureCollectedDupFill(cfg *config.Config, v6 bool) ([]string, bool) {
	return captureCollectedDupEntries(captureCollectedFilled(cfg), v6)
}

func captureCollectedForget() {
	captureCollectedApplied = map[string]*captureCollectedSet{}
}

func captureCollectedForgetPool() {
	captureCollectedSynced = nil
}

func captureCollectedRecord(name string, set *captureCollectedSet) {
	captureCollectedApplied[name] = set
}

func captureCollectedDesired(cfg *config.Config, s *captureCollectedSet) []string {
	if s.setID == "" {
		entries, _ := captureCollectedDupEntries(cfg, s.v6)
		return entries
	}
	set := cfg.GetSetById(s.setID)
	if set == nil || !set.Enabled || !set.MSSClamp.Enabled || !set.Targets.UseCollectedIPs {
		return nil
	}
	return routeCollectedFamilyEntries(set, s.v6)
}

func SyncCollectedCaptureSets(cfg *config.Config) error {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	if cfg == nil || cfg.System.Tables.SkipSetup || rulesAppliedCfg == nil {
		return nil
	}
	captureCollectedSynced = captureCollectedPoolOf(cfg)
	if len(captureCollectedApplied) == 0 {
		return nil
	}
	names := make([]string, 0, len(captureCollectedApplied))
	for name := range captureCollectedApplied {
		names = append(names, name)
	}
	sort.Strings(names)
	nft := rulesAppliedBackend == backendNFTables
	var failures []string
	for _, name := range names {
		s := captureCollectedApplied[name]
		want := captureCollectedDesired(cfg, s)
		next := slices.Clone(want)
		if gone := routeEntriesGone(s.entries, want); len(gone) > 0 {
			if stuck := captureCollectedDel(nft, name, gone); len(stuck) > 0 {
				next = append(next, stuck...)
				failures = append(failures, fmt.Sprintf("%s kept %s", name, strings.Join(stuck, ", ")))
			}
		}
		if failed := captureCollectedAdd(nft, name, want); len(failed) > 0 {
			failures = append(failures, fmt.Sprintf("%s refused %s", name, strings.Join(failed, ", ")))
		}
		s.entries = next
	}
	if len(failures) > 0 {
		return fmt.Errorf("collected capture sets: %s", strings.Join(failures, "; "))
	}
	return nil
}

func captureCollectedAdd(nft bool, name string, ips []string) []string {
	if len(ips) == 0 {
		return nil
	}
	if nft {
		if _, err := runNftStdin(captureNftElementScript("add", name, ips)); err == nil {
			return nil
		}
		var failed []string
		for _, ip := range ips {
			if !runLogged("capture: add element "+ip, "nft", "add", "element", "inet", nftTableName, name, "{", ip, "}") {
				failed = append(failed, ip)
			}
		}
		return failed
	}
	var sb strings.Builder
	for _, ip := range ips {
		fmt.Fprintf(&sb, "add %s %s\n", name, ip)
	}
	if err := runStdin(sb.String(), "ipset", "restore", "-exist"); err == nil {
		return nil
	}
	var failed []string
	for _, ip := range ips {
		if !runLogged("capture: ipset add "+ip, "ipset", "add", name, ip, "-exist") {
			failed = append(failed, ip)
		}
	}
	return failed
}

func captureCollectedDel(nft bool, name string, ips []string) []string {
	if !nft {
		return (&routeIptBackend{}).delElements(name, ips)
	}
	if _, err := runNftStdin(captureNftElementScript("delete", name, ips)); err == nil {
		return nil
	}
	var stuck []string
	for _, ip := range ips {
		out, err := run("nft", "delete", "element", "inet", nftTableName, name, "{", ip, "}")
		if err == nil {
			continue
		}
		log.Tracef("capture: delete element %s from %s failed: %s", ip, name, strings.TrimSpace(out))
		if !strings.Contains(out, "No such file or directory") {
			stuck = append(stuck, ip)
		}
	}
	return stuck
}

func captureNftElementScript(verb, name string, ips []string) string {
	return fmt.Sprintf("%s element inet %s %s { %s }\n", verb, nftTableName, name, strings.Join(ips, ", "))
}

func iptCaptureCollectedRulesPresent() bool {
	for name, s := range captureCollectedApplied {
		for _, r := range s.rules {
			if !r.manager.existsRule(r.IPT, r.Table, r.Chain, r.Spec) {
				log.Tracef("Monitor[%s]: the %s rule that matches the collected capture set %s is missing", r.IPT, r.Chain, name)
				return false
			}
		}
	}
	return true
}

func nftCaptureCollectedRulesPresent(n *NFTablesManager) bool {
	if len(captureCollectedApplied) == 0 {
		return true
	}
	var listing strings.Builder
	for _, chain := range []string{"output", "forward", "prerouting", nftChainName} {
		out, _ := n.runNft("list", "chain", "inet", nftTableName, chain)
		listing.WriteString(out)
		listing.WriteByte('\n')
	}
	all := listing.String()
	for name := range captureCollectedApplied {
		if !strings.Contains(all, "@"+name+" ") && !strings.Contains(all, "@"+name+"\n") {
			log.Tracef("Monitor: no rule matches the collected capture set %s", name)
			return false
		}
	}
	return true
}
