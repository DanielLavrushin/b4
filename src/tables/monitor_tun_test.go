package tables

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func newTUNTestConfig() *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.Mode = "tun"
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.System.Tables.Engine = backendIPTables
	return &cfg
}

func newTUNTestMonitor(cfg *config.Config) *Monitor {
	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)
	return &Monitor{
		cfgPtr:       &ptr,
		stop:         make(chan struct{}),
		backend:      backendIPTables,
		tun:          true,
		kick:         make(chan struct{}, 1),
		ifaceState:   make(map[string]ifaceSnapshot),
		egressIPHere: make(map[string]bool),
	}
}

type fakeFirewall struct {
	dumps map[string]string
	fail  func(line string) bool
	calls []string
}

func stubTUNFirewall(t *testing.T, dumps map[string]string) *fakeFirewall {
	t.Helper()
	origRun, origAdd := run, addRulesFn
	hasBinaryCache.Store(backendIPTables, true)
	hasBinaryCache.Store(backendIP6Tables, false)
	fw := &fakeFirewall{dumps: dumps}
	run = func(args ...string) (string, error) {
		line := strings.Join(args, " ")
		fw.calls = append(fw.calls, line)
		if strings.Contains(line, " -C ") || strings.Contains(line, " -D ") {
			return "", errors.New("rule missing")
		}
		if fw.fail != nil && fw.fail(line) {
			return "", errors.New("resource temporarily unavailable")
		}
		for suffix, out := range fw.dumps {
			if strings.HasSuffix(line, suffix) {
				return out, nil
			}
		}
		return "", nil
	}
	addRulesFn = func(*config.Config) error {
		t.Errorf("the TUN monitor ran the NFQUEUE rule set")
		return nil
	}
	t.Cleanup(func() {
		run, addRulesFn = origRun, origAdd
		hasBinaryCache.Delete(backendIPTables)
		hasBinaryCache.Delete(backendIP6Tables)
		masqApplied.Store(nil)
		mssApplied.Store(nil)
		mssAppliedRules.Store(0)
	})
	return fw
}

func (fw *fakeFirewall) called(want string) bool {
	for _, c := range fw.calls {
		if strings.Contains(c, want) {
			return true
		}
	}
	return false
}

const b4Clamp = "-A OUTPUT -p tcp -m tcp --dport 443 --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1360\n"

func TestCountB4MSSRulesSkipsRulesOtherSoftwareOwns(t *testing.T) {
	dump := "-P FORWARD ACCEPT\n" +
		"-A FORWARD -o l2tp-vpn -p tcp -m tcp --tcp-flags SYN,RST SYN -m comment --comment \"!fw3: Zone wan MTU fixing\" -j TCPMSS --clamp-mss-to-pmtu\n" +
		"-A FORWARD -o ppp0 -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1452\n" +
		"-A FORWARD -p tcp -m tcp --dport 443 --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1360\n" +
		"-A FORWARD -p tcp -m tcp --sport 443 -m set --match-set b4_mss_0_v4 src --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1300\n"
	if got := countB4MSSRules(dump); got != 2 {
		t.Fatalf("countB4MSSRules = %d, want 2", got)
	}
}

func TestTUNMonitorRestoresMasqueradeTheFirewallRemoved(t *testing.T) {
	cfg := newTUNTestConfig()
	cfg.System.Tables.Masquerade.Enabled = true
	fw := stubTUNFirewall(t, map[string]string{
		"-t nat -S POSTROUTING": "-P POSTROUTING ACCEPT\n",
		"-t nat -S B4_MASQ":     "-N B4_MASQ\n",
	})
	masqApplied.Store(cfg)
	before, _ := RulesRestores()

	m := newTUNTestMonitor(cfg)
	if !m.tick(false) {
		t.Fatalf("a flushed masquerade read as present")
	}
	if !fw.called("-t nat -A B4_MASQ -j MASQUERADE") {
		t.Fatalf("masquerade was not put back, calls: %v", fw.calls)
	}
	if fw.called("NFQUEUE") || fw.called("-t mangle -S B4_PREROUTING") {
		t.Fatalf("the TUN monitor touched NFQUEUE rules: %v", fw.calls)
	}
	after, last := RulesRestores()
	if after != before+1 || time.Since(last) > time.Minute {
		t.Fatalf("one tick must count one restore: before=%d after=%d last=%v", before, after, last)
	}
}

func TestTUNMonitorLeavesRulesThatAreInPlaceAlone(t *testing.T) {
	cfg := newTUNTestConfig()
	cfg.System.Tables.Masquerade.Enabled = true
	cfg.Queue.MSSClamp.Enabled = true
	cfg.Queue.MSSClamp.Size = 1360
	fw := stubTUNFirewall(t, map[string]string{
		"-t nat -S POSTROUTING":   "-A POSTROUTING -j B4_MASQ\n",
		"-t nat -S B4_MASQ":       "-A B4_MASQ -j MASQUERADE\n",
		"-t mangle -S OUTPUT":     b4Clamp,
		"-t mangle -S FORWARD":    strings.ReplaceAll(b4Clamp, "OUTPUT", "FORWARD"),
		"-t mangle -S PREROUTING": "",
	})
	masqApplied.Store(cfg)
	mssApplied.Store(cfg)
	mssAppliedRules.Store(2)

	m := newTUNTestMonitor(cfg)
	if _, acted := m.ensureRules(false); acted {
		t.Fatalf("rules in place were rebuilt")
	}
	if fw.called(" -A ") || fw.called(" -I ") || fw.called(" -F ") {
		t.Fatalf("a check that found everything wrote to the firewall: %v", fw.calls)
	}
}

func TestTUNMonitorRestoresOnlyTheMissingMSSClamp(t *testing.T) {
	cfg := newTUNTestConfig()
	cfg.System.Tables.Masquerade.Enabled = true
	cfg.Queue.MSSClamp.Enabled = true
	cfg.Queue.MSSClamp.Size = 1360
	fw := stubTUNFirewall(t, map[string]string{
		"-t nat -S POSTROUTING":   "-A POSTROUTING -j B4_MASQ\n",
		"-t nat -S B4_MASQ":       "-A B4_MASQ -j MASQUERADE\n",
		"-t mangle -S OUTPUT":     "-P OUTPUT ACCEPT\n",
		"-t mangle -S FORWARD":    "-A FORWARD -j TCPMSS --clamp-mss-to-pmtu\n",
		"-t mangle -S PREROUTING": "-P PREROUTING ACCEPT\n",
	})
	masqApplied.Store(cfg)
	mssApplied.Store(cfg)
	mssAppliedRules.Store(3)

	m := newTUNTestMonitor(cfg)
	if _, acted := m.ensureRules(false); !acted {
		t.Fatalf("a flushed MSS clamp read as present")
	}
	if !fw.called("-t mangle -I OUTPUT -p tcp --dport 443 --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1360") {
		t.Fatalf("MSS clamp was not put back, calls: %v", fw.calls)
	}
	if fw.called("-t nat -F B4_MASQ") {
		t.Fatalf("a working masquerade was torn down to restore the MSS clamp: %v", fw.calls)
	}
}

func TestTUNMonitorKeepsTheMSSBaselineWhenARestoreFails(t *testing.T) {
	cfg := newTUNTestConfig()
	cfg.Queue.MSSClamp.Enabled = true
	cfg.Queue.MSSClamp.Size = 1360
	fw := stubTUNFirewall(t, map[string]string{
		"-t mangle -S OUTPUT":     "-P OUTPUT ACCEPT\n",
		"-t mangle -S FORWARD":    "-P FORWARD ACCEPT\n",
		"-t mangle -S PREROUTING": "-P PREROUTING ACCEPT\n",
	})
	fw.fail = func(line string) bool { return strings.Contains(line, " -I ") }
	mssApplied.Store(cfg)
	mssAppliedRules.Store(3)

	m := newTUNTestMonitor(cfg)
	m.ensureRules(false)
	if got := mssAppliedRules.Load(); got != 3 || mssApplied.Load() == nil {
		t.Fatalf("a failed restore lowered what the monitor expects: baseline=%d tracked=%v", got, mssApplied.Load() != nil)
	}

	fw.calls = nil
	fw.fail = nil
	if _, acted := m.ensureRules(false); !acted || !fw.called(" -I OUTPUT") {
		t.Fatalf("the next tick did not retry the MSS clamp: %v", fw.calls)
	}
}

func TestTUNMonitorWithNothingInstalledChecksNothing(t *testing.T) {
	cfg := newTUNTestConfig()
	fw := stubTUNFirewall(t, nil)

	m := newTUNTestMonitor(cfg)
	if _, acted := m.ensureRules(false); acted {
		t.Fatalf("nothing was installed, yet the monitor restored")
	}
	if len(fw.calls) != 0 {
		t.Fatalf("expected no firewall calls, got %v", fw.calls)
	}
}

func TestClearMasqueradeUsesTheConfigThatWasApplied(t *testing.T) {
	applied := newTUNTestConfig()
	applied.System.Tables.Masquerade.Enabled = true
	fw := stubTUNFirewall(t, map[string]string{"-t nat -S B4_MASQ": "-N B4_MASQ\n"})
	masqApplied.Store(applied)

	ClearMasqueradeOnly(newTUNTestConfig())
	if !fw.called("-t nat -X B4_MASQ") {
		t.Fatalf("masquerade turned off at runtime left the applied rules behind: %v", fw.calls)
	}
	if masqApplied.Load() != nil {
		t.Fatalf("the cleared masquerade is still tracked")
	}
}

func TestNewMonitorInTUNModeHasATenSecondFloorAndIgnoresZero(t *testing.T) {
	cases := []struct {
		mode     string
		interval int
		want     time.Duration
	}{
		{"tun", 0, 10 * time.Second},
		{"tun", 3, 10 * time.Second},
		{"tun", 30, 30 * time.Second},
		{"nfqueue", 3, 3 * time.Second},
	}
	for _, c := range cases {
		cfg := newTUNTestConfig()
		cfg.Queue.Mode = c.mode
		cfg.System.Tables.MonitorInterval = c.interval
		var ptr atomic.Pointer[config.Config]
		ptr.Store(cfg)
		m := NewMonitor(&ptr)
		if m.interval != c.want || m.tun != (c.mode == "tun") {
			t.Errorf("mode=%s interval=%d: got interval %v tun=%v, want %v", c.mode, c.interval, m.interval, m.tun, c.want)
		}
	}
}

func TestMonitorStopTwiceDoesNotPanic(t *testing.T) {
	var ticks atomic.Int32
	m := newKickTestMonitor(t, &ticks)
	m.Stop()
}

func TestMasqueradeChainExemptsTheRunningTUNDevice(t *testing.T) {
	t.Cleanup(func() { tunDevice.Store(nil) })
	cfg := newTUNTestConfig()

	for _, spec := range masqueradeChainSpecs(cfg) {
		if strings.Contains(strings.Join(spec, " "), "RETURN") {
			t.Fatalf("no TUN device is running, yet the chain got an exemption: %v", spec)
		}
	}

	SetTUNDevice("tun9")
	specs := masqueradeChainSpecs(cfg)
	if len(specs) < 2 || strings.Join(specs[0], " ") != "-o tun9 -j RETURN" {
		t.Fatalf("the running device exemption must come first, got %v", specs)
	}
}

func TestRefreshTUNFirewallAppliesOnlyWhatChanged(t *testing.T) {
	applied := newTUNTestConfig()
	applied.System.Tables.Masquerade.Enabled = true
	fw := stubTUNFirewall(t, map[string]string{"-t nat -S B4_MASQ": "-N B4_MASQ\n"})
	t.Cleanup(func() {
		masqLast.Store(nil)
		mssLast.Store(nil)
	})
	masqApplied.Store(applied)
	masqLast.Store(applied)
	mssLast.Store(applied)

	same := newTUNTestConfig()
	same.System.Tables.Masquerade.Enabled = true
	if err := RefreshTUNFirewall(same); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(fw.calls) != 0 {
		t.Fatalf("unchanged masquerade and MSS settings touched the firewall: %v", fw.calls)
	}

	off := newTUNTestConfig()
	if err := RefreshTUNFirewall(off); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !fw.called("-t nat -X B4_MASQ") {
		t.Fatalf("turning masquerade off left the chain in place: %v", fw.calls)
	}
	if masqApplied.Load() != nil || masqLast.Load() != off {
		t.Fatalf("masquerade state not updated after turning it off")
	}
	if fw.called("TCPMSS") {
		t.Fatalf("an unchanged MSS clamp was rebuilt: %v", fw.calls)
	}
}

func TestRefreshTUNFirewallKeepsTheOldMasqueradeTrackedWhenANewOneFails(t *testing.T) {
	old := newTUNTestConfig()
	old.System.Tables.Masquerade.Enabled = true
	fw := stubTUNFirewall(t, map[string]string{"-t nat -S B4_MASQ": "-N B4_MASQ\n"})
	t.Cleanup(func() {
		masqLast.Store(nil)
		mssLast.Store(nil)
	})
	masqApplied.Store(old)
	masqLast.Store(old)
	mssLast.Store(old)
	fw.fail = func(line string) bool { return strings.Contains(line, "-A B4_MASQ") }

	next := newTUNTestConfig()
	next.System.Tables.Masquerade.Enabled = true
	next.System.Tables.Masquerade.Interfaces = []string{"ppp0"}
	if err := RefreshTUNFirewall(next); err == nil {
		t.Fatalf("a failed masquerade apply returned no error")
	}
	if fw.called("-t nat -X B4_MASQ") {
		t.Fatalf("the working masquerade was torn down before the new one applied: %v", fw.calls)
	}
	if masqApplied.Load() != old {
		t.Fatalf("the monitor no longer tracks the masquerade that was working, so it cannot put it back")
	}
	if masqLast.Load() != nil {
		t.Fatalf("a failed apply was recorded as the last good masquerade, so a revert would do nothing")
	}

	fw.fail = nil
	fw.calls = nil
	if err := RefreshTUNFirewall(old); err != nil {
		t.Fatalf("revert: %v", err)
	}
	if !fw.called("-t nat -A B4_MASQ -j MASQUERADE") {
		t.Fatalf("reverting after a failed apply did not put masquerade back: %v", fw.calls)
	}
}

func TestRefreshTUNFirewallKeepsThePreviousMSSClampWhenTheNewOneFails(t *testing.T) {
	prev := newTUNTestConfig()
	prev.Queue.MSSClamp.Enabled = true
	prev.Queue.MSSClamp.Size = 1360
	fw := stubTUNFirewall(t, map[string]string{
		"-t mangle -S OUTPUT": b4Clamp,
	})
	t.Cleanup(func() {
		masqLast.Store(nil)
		mssLast.Store(nil)
	})
	mssApplied.Store(prev)
	mssAppliedRules.Store(1)
	mssLast.Store(prev)
	masqLast.Store(prev)
	fw.fail = func(line string) bool { return strings.Contains(line, "--set-mss 1300") }

	next := newTUNTestConfig()
	next.Queue.MSSClamp.Enabled = true
	next.Queue.MSSClamp.Size = 1300
	if err := RefreshTUNFirewall(next); err == nil {
		t.Fatalf("a failed MSS apply returned no error")
	}
	if !fw.called("-t mangle -I OUTPUT -p tcp --dport 443 --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1360") {
		t.Fatalf("the previous MSS clamp was not put back after the new one failed: %v", fw.calls)
	}
	if mssLast.Load() != nil {
		t.Fatalf("the next save would not retry the MSS settings that failed")
	}
}

func TestNftTableHoldsOnlyEmptyBaseChains(t *testing.T) {
	empty := `table inet b4_mangle {
	chain prerouting {
		type filter hook prerouting priority -150; policy accept;
	}
	chain forward {
		type filter hook forward priority -150; policy accept;
	}
}`
	if !nftTableHoldsOnlyEmptyBaseChains(empty) {
		t.Fatalf("a table of empty base chains was kept")
	}
	withDiscovery := strings.Replace(empty, "\t}\n}", "\t}\n\tchain b4_discovery {\n\t}\n}", 1)
	if nftTableHoldsOnlyEmptyBaseChains(withDiscovery) {
		t.Fatalf("a table that still holds Discovery's chain would be deleted")
	}
	withRule := strings.Replace(empty, "policy accept;\n\t}", "policy accept;\n\t\tjump b4_discovery\n\t}", 1)
	if nftTableHoldsOnlyEmptyBaseChains(withRule) {
		t.Fatalf("a table with a rule in a base chain would be deleted")
	}
}

func TestClearTUNFirewallStopsLaterRefreshes(t *testing.T) {
	cfg := newTUNTestConfig()
	cfg.System.Tables.Masquerade.Enabled = true
	fw := stubTUNFirewall(t, nil)
	t.Cleanup(func() {
		tunFirewallClosed.Store(false)
		masqLast.Store(nil)
		mssLast.Store(nil)
	})

	ClearTUNFirewall(newTUNTestConfig())
	fw.calls = nil
	if err := RefreshTUNFirewall(cfg); err != nil {
		t.Fatalf("refresh after close: %v", err)
	}
	if err := restoreTUNRules(backendIPTables, tunRuleParts{masq: true, mss: true}); err != nil {
		t.Fatalf("restore after close: %v", err)
	}
	if len(fw.calls) != 0 {
		t.Fatalf("a save or restore after shutdown touched the firewall: %v", fw.calls)
	}
}

func TestNftMSSParsingFindsOnlyTheClampRulesAndSets(t *testing.T) {
	chain := `table inet b4_mangle {
	chain output { # handle 2
		type filter hook output priority -150; policy accept;
		meta mark 0x8002 accept # handle 11
		jump b4_discovery # handle 12
		tcp dport 443 tcp flags syn / syn,rst tcp option maxseg size set 1360 # handle 13
	}
}`
	if got := nftMSSRuleHandles(chain); len(got) != 1 || got[0] != "13" {
		t.Fatalf("nftMSSRuleHandles = %v, want [13]", got)
	}
	table := `table inet b4_mangle {
	set b4_mss_0_v4 {
		type ipv4_addr
		flags interval
	}
	set b4_other {
		type ipv4_addr
	}
}`
	if got := nftMSSSetNames(table); len(got) != 1 || got[0] != "b4_mss_0_v4" {
		t.Fatalf("nftMSSSetNames = %v, want [b4_mss_0_v4]", got)
	}
}
