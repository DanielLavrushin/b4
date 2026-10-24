package tables

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const (
	ctdirProbeCombined = "-p tcp -m connbytes --connbytes-dir original --connbytes-mode packets --connbytes 0:10 -m conntrack --ctdir ORIGINAL -j ACCEPT"
	ctdirProbePlain    = "-p tcp -m connbytes --connbytes-dir original --connbytes-mode packets --connbytes 0:10 -j ACCEPT"
)

func stubProbeAppends(t *testing.T, reject func(spec string) error) *[]string {
	t.Helper()
	var appended []string
	orig := run
	t.Cleanup(func() { run = orig })
	run = func(args ...string) (string, error) {
		i := slices.Index(args, "-A")
		if i < 0 || i+2 > len(args) {
			return "", nil
		}
		spec := strings.Join(args[i+2:], " ")
		appended = append(appended, spec)
		if err := reject(spec); err != nil {
			return "iptables: No chain/target/match by that name.", err
		}
		return "", nil
	}
	return &appended
}

func probeRejection(spec string) error {
	return fmt.Errorf("exit status 2 for [%s]", spec)
}

func TestCtdirProbeRidesOnTheConnbytesProbe(t *testing.T) {
	t.Run("both matches load in one probe", func(t *testing.T) {
		appended := stubProbeAppends(t, func(string) error { return nil })
		im := NewIPTablesManager(&config.Config{}, false)
		if err := im.checkConnbytesSupport(backendIPTables); err != nil {
			t.Fatalf("checkConnbytesSupport: %v", err)
		}
		if supported, probed := im.ctdirSupport[backendIPTables]; !probed || !supported {
			t.Errorf("ctdirSupport = %v (probed %v), want a cached true", supported, probed)
		}
		if !slices.Equal(*appended, []string{ctdirProbeCombined}) {
			t.Errorf("probe rules %q, want only %q: on a kernel with both matches the direction probe must not cost an iptables run of its own", *appended, ctdirProbeCombined)
		}
		if err := im.checkConnbytesSupport(backendIPTables); err != nil || len(*appended) != 1 {
			t.Errorf("a second check probed again (err %v, %d probe rules)", err, len(*appended))
		}
	})

	t.Run("a missing conntrack match keeps connbytes", func(t *testing.T) {
		appended := stubProbeAppends(t, func(spec string) error {
			if strings.Contains(spec, "conntrack") {
				return probeRejection(spec)
			}
			return nil
		})
		im := NewIPTablesManager(&config.Config{}, false)
		if err := im.checkConnbytesSupport(backendIP6Tables); err != nil {
			t.Fatalf("a kernel without the conntrack match must keep the family's queue rules: %v", err)
		}
		if supported, probed := im.ctdirSupport[backendIP6Tables]; !probed || supported {
			t.Errorf("ctdirSupport = %v (probed %v), want a cached false", supported, probed)
		}
		if !slices.Equal(*appended, []string{ctdirProbeCombined, ctdirProbePlain}) {
			t.Errorf("probe rules %q, want the combined rule and then connbytes alone", *appended)
		}
	})

	for _, loaded := range []bool{false, true} {
		t.Run(fmt.Sprintf("a connbytes failure reads as before, module loaded %v", loaded), func(t *testing.T) {
			fx := newKmodFixture(t)
			if loaded {
				if err := os.MkdirAll(filepath.Join(fx.sysRoot, "xt_connbytes"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			appended := stubProbeAppends(t, probeRejection)
			im := NewIPTablesManager(&config.Config{}, false)
			err := im.checkConnbytesSupport(backendIPTables)

			plainErr := probeRejection(ctdirProbePlain)
			want := fmt.Errorf("xt_connbytes kernel module is not available for %s (%v) - install it with: modprobe xt_connbytes (or apt install xtables-addons-common / linux-modules-extra-$(uname -r))", backendIPTables, plainErr)
			if loaded {
				want = fmt.Errorf("%s rejected the connbytes match although the xt_connbytes kernel module is loaded (%v) - the iptables match library is missing (OpenWrt/Entware: opkg install iptables-mod-conntrack-extra) or another program kept rewriting the filter table while b4 was probing it", backendIPTables, plainErr)
			}
			if err == nil || err.Error() != want.Error() {
				t.Fatalf("error = %v\nwant    %v", err, want)
			}
			if cached := im.connbytesSupport[backendIPTables]; cached == nil || cached.Error() != want.Error() {
				t.Errorf("cached connbytes result = %v, want the same error", cached)
			}
			if im.ctdirSupport[backendIPTables] {
				t.Error("the conntrack direction match counts as available after its probe failed")
			}
			if !slices.Equal(*appended, []string{ctdirProbeCombined, ctdirProbePlain}) {
				t.Errorf("probe rules %q", *appended)
			}
		})
	}
}

func ctdirTestManager(t *testing.T, multiport bool) *IPTablesManager {
	t.Helper()
	manager := dnsQueryTestManager(t, nil)
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "wide", "wide", true
	set.TCP.DPortFilter = "443,10000-20000"
	manager.cfg.Sets = []*config.SetConfig{&set}
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		manager.multiportSupport[bin] = multiport
	}
	return manager
}

func ctdirTestPorts(cfg *config.Config) []string {
	ports := cfg.CollectTCPPorts()
	for i, p := range ports {
		ports[i] = strings.ReplaceAll(p, "-", ":")
	}
	return ports
}

func wantTCPQueueSpecs(manager *IPTablesManager, multiport, ctdir bool) (pre, out []string) {
	cfg := manager.cfg
	nfq := strings.Join(manager.buildNFQSpec(cfg.Queue.StartNum, cfg.Queue.Threads), " ")
	window := fmt.Sprintf("0:%d", cfg.Queue.TCPConnBytesLimit)
	reply, original := " ", " "
	if ctdir {
		reply, original = " -m conntrack --ctdir REPLY ", " -m conntrack --ctdir ORIGINAL "
	}
	ports := ctdirTestPorts(cfg)
	sources, destinations := []string{}, []string{}
	if multiport {
		list := strings.Join(ports, ",")
		sources = append(sources, "-m multiport --sports "+list)
		destinations = append(destinations, "-m multiport --dports "+list)
	} else {
		for _, p := range ports {
			sources = append(sources, "--sport "+p)
			destinations = append(destinations, "--dport "+p)
		}
	}
	for _, src := range sources {
		pre = append(pre,
			"-p tcp "+src+" -m connbytes --connbytes-dir reply --connbytes-mode packets --connbytes "+window+reply+nfq,
			"-p tcp "+src+" --tcp-flags SYN,ACK SYN,ACK"+reply+nfq,
			"-p tcp "+src+" --tcp-flags RST RST"+reply+nfq,
		)
	}
	for _, dst := range destinations {
		out = append(out, "-p tcp "+dst+" -m connbytes --connbytes-dir original --connbytes-mode packets --connbytes "+window+original+nfq)
	}
	return pre, out
}

func tcpQueueSpecs(m Manifest, ipt, chain string) []string {
	var out []string
	for _, s := range manifestSpecs(m, ipt, "mangle", chain) {
		if strings.HasPrefix(s, "-p tcp") && strings.Contains(s, "NFQUEUE") {
			out = append(out, s)
		}
	}
	return out
}

func TestQueueRulesMatchConntrackDirectionPerBinary(t *testing.T) {
	for _, multiport := range []bool{true, false} {
		t.Run(fmt.Sprintf("multiport %v", multiport), func(t *testing.T) {
			manager := ctdirTestManager(t, multiport)
			manager.ctdirSupport[backendIP6Tables] = false
			m, err := manager.buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			for ipt, ctdir := range map[string]bool{backendIPTables: true, backendIP6Tables: false} {
				wantPre, wantOut := wantTCPQueueSpecs(manager, multiport, ctdir)
				if got := tcpQueueSpecs(m, ipt, "B4_PREROUTING"); !slices.Equal(got, wantPre) {
					t.Errorf("%s B4_PREROUTING TCP queue rules:\n got %q\nwant %q", ipt, got, wantPre)
				}
				if got := tcpQueueSpecs(m, ipt, iptChainName); !slices.Equal(got, wantOut) {
					t.Errorf("%s B4 TCP queue rules:\n got %q\nwant %q", ipt, got, wantOut)
				}
			}
			if want := map[string]bool{backendIPTables: true, backendIP6Tables: false}; !mapsEqual(m.DirectionExact, want) {
				t.Errorf("DirectionExact = %v, want %v", m.DirectionExact, want)
			}
			if v4, v6 := m.directionExactFamilies(); !v4 || v6 {
				t.Errorf("families exact v4=%v v6=%v, want IPv4 only", v4, v6)
			}
			wantPre, wantOut := wantTCPQueueSpecs(manager, multiport, true)
			directed := append(wantPre, wantOut...)
			for _, r := range m.Rules {
				spec := strings.Join(r.Spec, " ")
				if strings.Contains(spec, "conntrack") && (r.IPT != backendIPTables || !slices.Contains(directed, spec)) {
					t.Errorf("only the TCP queue rules select by conntrack direction, yet %s %s %s carries it: %q", r.IPT, r.Table, r.Chain, spec)
				}
			}
		})
	}
}

func mapsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func stripCtdir(spec string) string {
	spec = strings.ReplaceAll(spec, " -m conntrack --ctdir REPLY", "")
	return strings.ReplaceAll(spec, " -m conntrack --ctdir ORIGINAL", "")
}

func TestDuplicationQueueRulesTakeOnlyTheConnectionsOwnDirection(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true, "ipset": true})
	stubIPSetProbe(t, nil)
	for _, multiport := range []bool{true, false} {
		manager := NewIPTablesManager(dupTestConfig(), false)
		stubProbes(manager, backendIPTables, backendIP6Tables)
		manager.ctdirSupport[backendIP6Tables] = false
		for _, bin := range []string{backendIPTables, backendIP6Tables} {
			manager.multiportSupport[bin] = multiport
		}
		m, err := manager.buildManifest()
		if err != nil {
			t.Fatalf("buildManifest: %v", err)
		}
		for ipt, c := range map[string]struct {
			set   string
			ctdir bool
		}{backendIPTables: {"b4_dup_v4", true}, backendIP6Tables: {"b4_dup_v6", false}} {
			var dup []string
			for _, s := range manifestSpecs(m, ipt, "mangle", iptChainName) {
				if strings.Contains(s, "--match-set "+c.set+" dst") {
					dup = append(dup, s)
				}
			}
			if len(dup) == 0 {
				t.Fatalf("multiport %v, %s: no duplication queue rule", multiport, ipt)
			}
			for _, s := range dup {
				if has := strings.Contains(s, "-m conntrack --ctdir ORIGINAL -j NFQUEUE"); has != c.ctdir {
					t.Errorf("multiport %v, %s: duplication rule %q carries the ORIGINAL direction match: %v, want %v; without it a reply from a duplication address is queued at POSTROUTING and duplicated as client traffic", multiport, ipt, s, has, c.ctdir)
				}
			}
		}
	}
}

func TestQueueRulesWithoutTheConntrackMatchAreTodaysRules(t *testing.T) {
	for _, multiport := range []bool{true, false} {
		t.Run(fmt.Sprintf("multiport %v", multiport), func(t *testing.T) {
			on := ctdirTestManager(t, multiport)
			off := ctdirTestManager(t, multiport)
			for _, bin := range []string{backendIPTables, backendIP6Tables} {
				off.ctdirSupport[bin] = false
			}
			mOn, err := on.buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			mOff, err := off.buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}

			if len(mOn.Rules) != len(mOff.Rules) || len(mOn.Chains) != len(mOff.Chains) || len(mOn.IPSets) != len(mOff.IPSets) {
				t.Fatalf("rules %d/%d, chains %d/%d, ipsets %d/%d: the conntrack match may only change rules, never add or drop them",
					len(mOn.Rules), len(mOff.Rules), len(mOn.Chains), len(mOff.Chains), len(mOn.IPSets), len(mOff.IPSets))
			}
			for i := range mOff.Rules {
				a, b := mOn.Rules[i], mOff.Rules[i]
				if a.IPT != b.IPT || a.Table != b.Table || a.Chain != b.Chain || a.Action != b.Action || a.BelowDSCP != b.BelowDSCP {
					t.Fatalf("rule %d moved: %+v vs %+v", i, a, b)
				}
				offSpec := strings.Join(b.Spec, " ")
				if strings.Contains(offSpec, "conntrack") {
					t.Errorf("without the conntrack match a rule still carries it: %q", offSpec)
				}
				if got := stripCtdir(strings.Join(a.Spec, " ")); got != offSpec {
					t.Errorf("rule %d differs by more than the conntrack match:\n  on  %q\n  off %q", i, got, offSpec)
				}
			}
			for _, ipt := range []string{backendIPTables, backendIP6Tables} {
				wantPre, wantOut := wantTCPQueueSpecs(off, multiport, false)
				if got := tcpQueueSpecs(mOff, ipt, "B4_PREROUTING"); !slices.Equal(got, wantPre) {
					t.Errorf("%s B4_PREROUTING:\n got %q\nwant %q", ipt, got, wantPre)
				}
				if got := tcpQueueSpecs(mOff, ipt, iptChainName); !slices.Equal(got, wantOut) {
					t.Errorf("%s B4:\n got %q\nwant %q", ipt, got, wantOut)
				}
			}
			if v4, v6 := mOff.directionExactFamilies(); v4 || v6 {
				t.Errorf("rules without the conntrack match published as direction exact: v4=%v v6=%v", v4, v6)
			}
		})
	}
}

func TestTeardownRemovesTheQueueRulesInTheFormTheProbeFound(t *testing.T) {
	manager := ctdirTestManager(t, true)
	delete(manager.ctdirSupport, backendIPTables)
	manager.ctdirSupport[backendIP6Tables] = false

	m, err := manager.buildTeardownManifest()
	if err != nil {
		t.Fatalf("buildTeardownManifest: %v", err)
	}
	for ipt, ctdir := range map[string]bool{backendIPTables: true, backendIP6Tables: false} {
		wantPre, wantOut := wantTCPQueueSpecs(manager, true, ctdir)
		if got := tcpQueueSpecs(m, ipt, "B4_PREROUTING"); !slices.Equal(got, wantPre) {
			t.Errorf("%s teardown B4_PREROUTING:\n got %q\nwant %q", ipt, got, wantPre)
		}
		if got := tcpQueueSpecs(m, ipt, iptChainName); !slices.Equal(got, wantOut) {
			t.Errorf("%s teardown B4:\n got %q\nwant %q", ipt, got, wantOut)
		}
	}
	if len(m.DirectionExact) != 0 {
		t.Errorf("a teardown manifest carries direction flags: %v", m.DirectionExact)
	}
	if _, probed := manager.ctdirSupport[backendIPTables]; probed {
		t.Error("a teardown ran the conntrack direction probe")
	}
}

func TestQueueDirectionExactFamilies(t *testing.T) {
	for _, tc := range []struct {
		exact  map[string]bool
		v4, v6 bool
	}{
		{nil, false, false},
		{map[string]bool{backendIPTables: true}, true, false},
		{map[string]bool{backendIP6Tables: true}, false, true},
		{map[string]bool{backendIPTables: true, backendIP6Tables: false}, true, false},
		{map[string]bool{backendIPTablesLegacy: true, backendIP6TablesLegacy: true}, true, true},
		{map[string]bool{backendIPTables: true, backendIPTablesLegacy: false}, false, false},
	} {
		if v4, v6 := queueDirectionExactFamilies(tc.exact); v4 != tc.v4 || v6 != tc.v6 {
			t.Errorf("%v: v4=%v v6=%v, want v4=%v v6=%v", tc.exact, v4, v6, tc.v4, tc.v6)
		}
	}
}

type directionRig struct {
	h      *goldenHost
	events []string
	refuse func(args []string) bool
}

func newDirectionRig(t *testing.T) *directionRig {
	t.Helper()
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	r := &directionRig{h: h}
	run = func(args ...string) (string, error) {
		if r.refuse != nil && r.refuse(args) {
			h.record(strings.Join(args, " ")+" -> refused", "")
			return "refused by the test", errors.New("exit status 2")
		}
		return h.run(args...)
	}
	QueueDirectionExactFunc = func(v4, v6 bool) {
		event := fmt.Sprintf("publish v4=%v v6=%v", v4, v6)
		r.events = append(r.events, event)
		h.record(event, "")
	}
	return r
}

func (r *directionRig) since(n int) []string {
	return slices.Clone(r.events[n:])
}

func directionConfig(backend string) *config.Config {
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backend
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = true
	return &cfg
}

func firstLine(lines []string, match func(string) bool) int {
	return slices.IndexFunc(lines, match)
}

func lastLine(lines []string, match func(string) bool) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if match(lines[i]) {
			return i
		}
	}
	return -1
}

func isIptQueueRuleAdd(line string) bool {
	return (strings.Contains(line, " -A B4 ") || strings.Contains(line, " -I B4_PREROUTING ")) && strings.Contains(line, "NFQUEUE") && strings.Contains(line, "-p tcp")
}

func isNftRuleAdd(line string) bool {
	return strings.HasPrefix(line, "exec nft add rule inet "+nftTableName+" ")
}

func iptPortOnlyTCPQueueRules(h *goldenHost, bin string) []string {
	var out []string
	for _, line := range strings.Split(h.iptTable(bin, "mangle").save(""), "\n") {
		if (strings.HasPrefix(line, "-A B4 ") || strings.HasPrefix(line, "-A B4_PREROUTING ")) && strings.Contains(line, "-p tcp") && strings.Contains(line, "NFQUEUE") && !strings.Contains(line, "--ctdir") {
			out = append(out, line)
		}
	}
	return out
}

func refuseCtdirProbe(bin string) func(args []string) bool {
	return func(args []string) bool {
		return args[0] == bin && slices.Contains(args, "B4_MODULE_TEST") && slices.Contains(args, "conntrack")
	}
}

func TestIPTablesPublishesDirectionExactAfterTheRulesAreIn(t *testing.T) {
	r := newDirectionRig(t)
	cfg := directionConfig(backendIPTables)

	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	lines := r.h.take()
	if got := r.since(0); !slices.Equal(got, []string{"publish v4=true v6=true"}) {
		t.Fatalf("publications %q, want one for both families once the rules are in", got)
	}
	at := firstLine(lines, func(l string) bool { return l == "publish v4=true v6=true" })
	if last := lastLine(lines, isIptQueueRuleAdd); last < 0 || at < last {
		t.Errorf("published at line %d, before the last queue rule went in at line %d", at, last)
	}

	r.h.iptTable(backendIPTables, "mangle").chain("POSTROUTING").rules = nil
	n := len(r.events)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := r.since(n); !slices.Equal(got, []string{"publish v4=true v6=true"}) {
		t.Errorf("a restore of the same rules published %q; the flags must stay up throughout", got)
	}

	n = len(r.events)
	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if got := r.since(n); !slices.Equal(got, []string{"publish v4=false v6=false"}) {
		t.Errorf("ClearRules published %q, want both families down", got)
	}
}

func TestIPTablesFamilyWithoutConntrackMatchKeepsPortRules(t *testing.T) {
	r := newDirectionRig(t)
	r.refuse = refuseCtdirProbe(backendIP6Tables)
	if err := AddRules(directionConfig(backendIPTables)); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	if got := r.since(0); !slices.Equal(got, []string{"publish v4=true v6=false"}) {
		t.Errorf("publications %q, want IPv4 exact and IPv6 not", got)
	}
	if left := iptPortOnlyTCPQueueRules(r.h, backendIPTables); len(left) > 0 {
		t.Errorf("iptables kept TCP queue rules without the direction match: %q", left)
	}
	mangle := r.h.iptTable(backendIP6Tables, "mangle")
	for _, chain := range []string{iptChainName, "B4_PREROUTING"} {
		if rules := mangle.save(chain); strings.Contains(rules, "conntrack") {
			t.Errorf("ip6tables %s got the conntrack match its probe refused:\n%s", chain, rules)
		}
	}
	if got := iptPortOnlyTCPQueueRules(r.h, backendIP6Tables); len(got) != 4 {
		t.Errorf("ip6tables should hold the four port-selected TCP queue rules of today, got %q", got)
	}
}

func TestIPTablesLowersTheFlagBeforePortRulesGoIn(t *testing.T) {
	r := newDirectionRig(t)
	cfg := directionConfig(backendIPTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	r.h.take()

	r.refuse = refuseCtdirProbe(backendIPTables)
	n := len(r.events)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules without the IPv4 conntrack match: %v", err)
	}
	lines := r.h.take()
	if got := r.since(n); !slices.Equal(got, []string{"publish v4=false v6=true", "publish v4=false v6=true"}) {
		t.Fatalf("publications %q, want IPv4 lowered before the apply and kept low after it", got)
	}
	lowered := firstLine(lines, func(l string) bool { return l == "publish v4=false v6=true" })
	firstPortRule := firstLine(lines, func(l string) bool {
		return strings.HasPrefix(l, backendIPTables+" ") && isIptQueueRuleAdd(l) && !strings.Contains(l, "--ctdir")
	})
	if firstPortRule < 0 {
		t.Fatalf("the restore without the conntrack match added no port-selected rule; the test proves nothing:\n%s", strings.Join(lines, "\n"))
	}
	if lowered > firstPortRule {
		t.Errorf("IPv4 was still published exact at line %d when a port-selected rule went in at line %d", lowered, firstPortRule)
	}

	r.refuse = nil
	n = len(r.events)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules with the conntrack match back: %v", err)
	}
	lines = r.h.take()
	if got := r.since(n); len(got) == 0 || got[len(got)-1] != "publish v4=true v6=true" {
		t.Errorf("publications %q, want both families exact at the end", got)
	}
	if left := iptPortOnlyTCPQueueRules(r.h, backendIPTables); len(left) > 0 {
		t.Errorf("IPv4 is published exact while port-selected queue rules from the earlier apply are still installed: %q", left)
	}
	if firstLine(lines, func(l string) bool { return l == backendIPTables+" -w -t mangle -F B4" }) < 0 {
		t.Errorf("the rules were not rebuilt when the conntrack match came back:\n%s", strings.Join(lines, "\n"))
	}
}

func TestIPTablesFailedApplyLowersTheFlags(t *testing.T) {
	r := newDirectionRig(t)
	cfg := directionConfig(backendIPTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	changed := directionConfig(backendIPTables)
	changed.Queue.UDPConnBytesLimit = cfg.Queue.UDPConnBytesLimit + 1
	r.refuse = func(args []string) bool {
		return args[0] == backendIPTables && slices.Contains(args, "-A") && slices.Contains(args, iptChainName) && slices.Contains(args, "udp") && slices.Contains(args, "NFQUEUE")
	}
	n := len(r.events)
	if err := AddRules(changed); err == nil {
		t.Fatal("the refused UDP rule did not fail the apply; the test proves nothing")
	}
	if got := r.since(n); len(got) == 0 || got[len(got)-1] != "publish v4=false v6=false" {
		t.Errorf("publications after a failed apply %q, want both families down", got)
	}
}

func nftQueueRuleLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if isNftRuleAdd(l) && strings.Contains(l, " queue ") && strings.Contains(l, "tcp ") {
			out = append(out, strings.TrimPrefix(l, "exec nft add rule inet "+nftTableName+" "))
		}
	}
	return out
}

func TestNFTablesQueueRulesSelectByDirection(t *testing.T) {
	r := newDirectionRig(t)
	cfg := directionConfig(backendNFTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	lines := r.h.take()
	queue := nftQueueAction(cfg)
	want := []string{
		"b4_chain tcp dport 443 ct original packets < 20 ct direction original counter " + queue,
		"prerouting tcp sport 443 ct original packets < 20 ct direction reply counter " + queue,
		"prerouting tcp sport 443 tcp flags & (syn|ack) == (syn|ack) ct direction reply counter " + queue,
		"prerouting tcp sport 443 tcp flags & rst == rst ct direction reply counter " + queue,
	}
	if got := nftQueueRuleLines(lines); !slices.Equal(got, want) {
		t.Errorf("TCP queue rules:\n got %q\nwant %q", got, want)
	}
	udp := firstLine(lines, func(l string) bool { return isNftRuleAdd(l) && strings.Contains(l, "udp dport 443") })
	switch {
	case udp < 0:
		t.Error("no UDP window rule went in")
	case strings.Contains(lines[udp], "direction"):
		t.Errorf("the UDP window must stay without a direction match: %q", lines[udp])
	}
	if got := r.since(0); !slices.Equal(got, []string{"publish v4=true v6=true"}) {
		t.Fatalf("publications %q, want both families exact once", got)
	}
	at := firstLine(lines, func(l string) bool { return l == "publish v4=true v6=true" })
	if last := lastLine(lines, isNftRuleAdd); at < last {
		t.Errorf("published at line %d, before the last rule went in at line %d", at, last)
	}

	n := len(r.events)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := r.since(n); !slices.Equal(got, []string{"publish v4=true v6=true"}) {
		t.Errorf("rebuilding the same table published %q; the flags must stay up throughout", got)
	}

	n = len(r.events)
	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if got := r.since(n); !slices.Equal(got, []string{"publish v4=false v6=false"}) {
		t.Errorf("ClearRules published %q, want both families down", got)
	}
}

func nftQueueAction(cfg *config.Config) string {
	return nfqueueActionExpr(cfg.Queue.StartNum, cfg.Queue.Threads)
}

func refuseNftCtDirectionProbe(args []string) bool {
	return args[0] == "nft" && slices.Contains(args, "_b4_queue_probe") && slices.Contains(args, "direction")
}

func forgetQueueProbes(t *testing.T) {
	t.Helper()
	queueProbeMu.Lock()
	queueProbeCache = map[string]queueCaps{}
	queueProbeMu.Unlock()
}

func TestNFTablesWithoutCtDirectionKeepsTodaysRules(t *testing.T) {
	r := newDirectionRig(t)
	r.refuse = refuseNftCtDirectionProbe
	cfg := directionConfig(backendNFTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	lines := r.h.take()
	queue := nftQueueAction(cfg)
	want := []string{
		"b4_chain tcp dport 443 ct original packets < 20 counter " + queue,
		"prerouting tcp sport 443 ct original packets < 20 counter " + queue,
		"prerouting tcp sport 443 tcp flags & (syn|ack) == (syn|ack) counter " + queue,
		"prerouting tcp sport 443 tcp flags & rst == rst counter " + queue,
	}
	if got := nftQueueRuleLines(lines); !slices.Equal(got, want) {
		t.Errorf("TCP queue rules:\n got %q\nwant %q", got, want)
	}
	for _, l := range lines {
		if isNftRuleAdd(l) && strings.Contains(l, "ct direction") {
			t.Errorf("a rule uses the direction match its probe refused: %q", l)
		}
	}
	if got := r.since(0); !slices.Equal(got, []string{"publish v4=false v6=false"}) {
		t.Errorf("publications %q, want both families not exact", got)
	}
}

func TestNFTablesLowersTheFlagBeforeRulesWithoutDirectionGoIn(t *testing.T) {
	r := newDirectionRig(t)
	cfg := directionConfig(backendNFTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	r.h.take()

	forgetQueueProbes(t)
	r.refuse = refuseNftCtDirectionProbe
	n := len(r.events)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules without ct direction: %v", err)
	}
	lines := r.h.take()
	if got := r.since(n); len(got) == 0 || got[0] != "publish v4=false v6=false" || got[len(got)-1] != "publish v4=false v6=false" {
		t.Fatalf("publications %q, want both families lowered before the rebuild and kept low", got)
	}
	lowered := firstLine(lines, func(l string) bool { return l == "publish v4=false v6=false" })
	if first := firstLine(lines, isNftRuleAdd); first < 0 || lowered > first {
		t.Errorf("still published exact at line %d when the first rule went in at line %d", lowered, first)
	}
}

func TestNFTablesFailedApplyLowersTheFlags(t *testing.T) {
	r := newDirectionRig(t)
	cfg := directionConfig(backendNFTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	forgetQueueProbes(t)
	r.refuse = func(args []string) bool {
		return args[0] == "nft" && slices.Contains(args, "_b4_queue_probe") && slices.Contains(args, "queue")
	}
	n := len(r.events)
	if err := AddRules(cfg); err == nil {
		t.Fatal("the refused queue probe did not fail the apply; the test proves nothing")
	}
	if got := r.since(n); len(got) == 0 || got[len(got)-1] != "publish v4=false v6=false" {
		t.Errorf("publications after a failed apply %q, want both families down", got)
	}
}
