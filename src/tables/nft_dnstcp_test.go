package tables

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const fakeNftScript = `#!/bin/sh
d="$(dirname "$0")"
printf '%s\n' "$*" >> "$d/calls"
case "$1 $2" in
"list tables") cat "$d/tables" ;;
"list chain")
	f="$d/chain.$3.$4.$5"
	[ -f "$f" ] || { echo "Error: No such file or directory" >&2; exit 1; }
	cat "$f"
	;;
esac
exit 0
`

func fakeNft(t *testing.T, tables string, chains map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("tables", tables, 0o644)
	for name, body := range chains {
		write("chain."+name, body, 0o644)
	}
	write("nft", fakeNftScript, 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func nftNatChain(family, table, chain, hook string, rules ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table %s %s {\n\tchain %s {\n\t\ttype nat hook %s priority dstnat; policy accept;\n", family, table, chain, hook)
	for _, r := range rules {
		fmt.Fprintf(&b, "\t\t%s\n", r)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

const (
	nftPaddedReturn = "meta mark & 0x00008000 == 0x00008000 return"
	nftRedirect     = "tcp dport 53 redirect to :5453"
)

func dnsTCPChains(family, table string, pre, out []string) map[string]string {
	return map[string]string{
		family + "." + table + "." + nftDNSTCPPreChain: nftNatChain(family, table, nftDNSTCPPreChain, "prerouting", pre...),
		family + "." + table + "." + nftDNSTCPOutChain: nftNatChain(family, table, nftDNSTCPOutChain, "output", out...),
	}
}

func TestTheNftMonitorWantsTheReturnAheadOfTheRedirectInBothDNSTCPChains(t *testing.T) {
	intact := []string{nftPaddedReturn, nftRedirect}
	both := "table inet b4_mangle\ntable ip b4_dnsnat\ntable ip6 b4_dnsnat6\n"
	for _, tc := range []struct {
		name     string
		tables   string
		pre, out []string
		want     bool
	}{
		{"both chains as nft lists them", both, intact, intact, true},
		{"the return gone from the prerouting chain", both, []string{nftRedirect}, intact, false},
		{"the redirect gone from the output chain", both, intact, []string{nftPaddedReturn}, false},
		{"the return below the redirect", both, []string{nftRedirect, nftPaddedReturn}, intact, false},
		{"a return for another mark", both, []string{"meta mark & 0x00400000 == 0x00400000 return", nftRedirect}, intact, false},
		{"a redirect to another port", both, intact, []string{nftPaddedReturn, "tcp dport 53 redirect to :5454"}, false},
		{"another port's redirect to the listener", both, []string{nftPaddedReturn, "tcp dport 853 redirect to :5453"}, intact, false},
		{"a UDP redirect to the listener", both, intact, []string{nftPaddedReturn, "udp dport 53 redirect to :5453"}, false},
		{"only the IPv6 table", "table inet b4_mangle\ntable ip6 b4_dnsnat6\n", intact, intact, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeNft(t, tc.tables, dnsTCPChains("ip", nftDNSTCPTableName, tc.pre, tc.out))
			c := config.NewConfig()
			if got := NewNFTablesManager(&c).dnsTCPFamilyIntact(false, 0x8000, 5453); got != tc.want {
				t.Errorf("the IPv4 DNS-over-TCP rules read as intact = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheIPv6DNSTCPTableIsNotTakenForTheIPv4One(t *testing.T) {
	intact := []string{nftPaddedReturn, nftRedirect}
	dir := fakeNft(t, "table inet b4_mangle\ntable ip6 b4_dnsnat6\n", dnsTCPChains("ip6", nftDNSTCPTableName6, intact, intact))
	c := config.NewConfig()
	n := NewNFTablesManager(&c)
	if n.dnsTCPFamilyExists(false) {
		t.Error("the ip6 table b4_dnsnat6 was taken for the ip table b4_dnsnat, so a deleted IPv4 table goes unnoticed")
	}
	if !n.dnsTCPFamilyIntact(true, 0x8000, 5453) {
		t.Error("the IPv6 DNS-over-TCP rules read as broken")
	}
	n.clearDNSTCPFamily(false)
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(calls), "\n") {
		if strings.Contains(line, "table ip "+nftDNSTCPTableName) {
			t.Errorf("clearing the IPv4 family touched a table that does not exist: nft %s", line)
		}
	}
}

const statefulDNSTCPNftScript = `#!/bin/sh
d="$(dirname "$0")"
printf '%s\n' "$*" >> "$d/calls"
case "$*" in
"list tables")
	echo "table inet @MANGLE@"
	if [ -f "$d/ip" ]; then echo "table ip @NAT4@"; fi
	if [ -f "$d/ip6" ]; then echo "table ip6 @NAT6@"; fi
	;;
"list chain inet @MANGLE@ "*) cat "$d/mangle" ;;
"list chain ip @NAT4@ "*|"list chain ip6 @NAT6@ "*)
	if [ ! -f "$d/$3" ]; then echo "Error: No such file or directory" >&2; exit 1; fi
	cat "$d/nat"
	;;
"add table ip @NAT4@"|"add table ip6 @NAT6@") touch "$d/$3" ;;
"delete table ip @NAT4@"|"delete table ip6 @NAT6@") rm -f "$d/$3" ;;
"add chain ip @NAT4@ "*|"add chain ip6 @NAT6@ "*)
	left=0
	if [ -f "$d/refuse.$3" ]; then left=$(cat "$d/refuse.$3"); fi
	if [ "$left" -gt 0 ]; then
		echo $((left - 1)) > "$d/refuse.$3"
		echo "Error: Could not process rule: No such file or directory" >&2
		exit 1
	fi
	;;
esac
exit 0
`

type dnsTCPMonitorRig struct {
	dir    string
	cfg    *config.Config
	m      *Monitor
	builds int
}

func newDNSTCPMonitorRig(t *testing.T) *dnsTCPMonitorRig {
	t.Helper()
	resetECHOnlyState(t)
	cfg := ech6Config(t)
	r := &dnsTCPMonitorRig{dir: t.TempDir(), cfg: cfg}

	markRule := fmt.Sprintf("meta mark & 0x%08x == 0x%08x", cfg.MainInjectedMark(), cfg.MainInjectedMark())
	answers := fmt.Sprintf("udp sport 53 counter queue flags bypass to %d", cfg.Queue.StartNum)
	r.write(t, "nft", strings.NewReplacer("@MANGLE@", nftTableName, "@NAT4@", nftDNSTCPTableName, "@NAT6@", nftDNSTCPTableName6).Replace(statefulDNSTCPNftScript), 0o755)
	r.write(t, "mangle", fmt.Sprintf("table inet %s {\n\tchain any {\n\t\tjump %s\n\t\tmeta nfproto ipv4 %s\n\t\tmeta nfproto ipv6 %s\n\t\tudp dport 53 counter queue flags bypass to %d\n\t\ttcp sport 443 counter queue flags bypass to %d\n\t\t%s accept\n\t}\n}\n",
		nftTableName, nftChainName, answers, answers, cfg.Queue.StartNum, cfg.Queue.StartNum, markRule), 0o644)
	r.write(t, "nat", fmt.Sprintf("table any any {\n\tchain any {\n\t\t%s return\n\t\t%s\n\t}\n}\n",
		markRule, strings.Join(nftDNSTCPRedirectRule(cfg.DNSTCPListenPort()), " ")), 0o644)
	t.Setenv("PATH", r.dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	sys := t.TempDir()
	for _, module := range append(append([]string{}, kernelModuleList...), "xt_hashlimit") {
		if err := os.MkdirAll(filepath.Join(sys, kmodCanonical(module)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	origRun, origAdd, origClear, origSys := run, addRulesFn, clearRulesFn, kmodSysRoot
	run = func(...string) (string, error) { return "", nil }
	kmodSysRoot = sys
	addRulesFn = func(c *config.Config) error {
		r.builds++
		return NewNFTablesManager(c).ApplyDNSTCP()
	}
	clearRulesFn = func(*config.Config) error { return nil }
	rulesMu.Lock()
	origApplied, origBackend := rulesAppliedCfg, rulesAppliedBackend
	rulesAppliedCfg, rulesAppliedBackend = cfg, backendNFTables
	rulesMu.Unlock()
	t.Cleanup(func() {
		run, addRulesFn, clearRulesFn, kmodSysRoot = origRun, origAdd, origClear, origSys
		rulesMu.Lock()
		rulesAppliedCfg, rulesAppliedBackend = origApplied, origBackend
		rulesMu.Unlock()
	})

	var current atomic.Pointer[config.Config]
	current.Store(cfg)
	r.m = &Monitor{cfgPtr: &current, backend: backendNFTables}
	return r
}

func (r *dnsTCPMonitorRig) write(t *testing.T, name, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func (r *dnsTCPMonitorRig) refuse(t *testing.T, family string, times int) {
	t.Helper()
	r.write(t, "refuse."+family, strconv.Itoa(times), 0o644)
}

func (r *dnsTCPMonitorRig) apply() {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	_ = addRulesFn(r.cfg)
}

func (r *dnsTCPMonitorRig) tick() bool {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	_, restored := r.m.ensureRulesLocked(false)
	return restored
}

func (r *dnsTCPMonitorRig) has(family string) bool {
	_, err := os.Stat(filepath.Join(r.dir, family))
	return err == nil
}

func (r *dnsTCPMonitorRig) tries(t *testing.T, v6 bool) int {
	t.Helper()
	family, table := nftDNSTCPTable(v6)
	return strings.Count(readLog(t, filepath.Join(r.dir, "calls")), "add chain "+family+" "+table+" "+nftDNSTCPPreChain+" ")
}

func TestTheNftMonitorRetriesOnlyTheDNSOverTCPFamilyTheKernelRefuses(t *testing.T) {
	r := newDNSTCPMonitorRig(t)
	r.refuse(t, "ip6", 1000)
	r.apply()

	for i := 1; i <= 2*nftDNSTCPFailStreak; i++ {
		if r.tick() {
			t.Fatalf("tick %d rebuilt the whole firewall for the IPv6 DNS-over-TCP table nft refuses", i)
		}
	}
	if got := r.tries(t, true); got != nftDNSTCPFailStreak {
		t.Errorf("the IPv6 DNS-over-TCP table was tried %d times, want %d: once by the apply, then by the monitor until %d failures in a row", got, nftDNSTCPFailStreak, nftDNSTCPFailStreak)
	}
	if r.builds != 1 || r.tries(t, false) != 1 || !r.has("ip") {
		t.Errorf("%d full builds, %d IPv4 builds, IPv4 table present %v: the IPv4 DNS-over-TCP table was built once and must be left alone", r.builds, r.tries(t, false), r.has("ip"))
	}
}

func TestTheNftMonitorRebuildsADNSOverTCPFamilyAfterAPassingFailure(t *testing.T) {
	r := newDNSTCPMonitorRig(t)
	r.refuse(t, "ip", 1)
	r.apply()
	if r.has("ip") {
		t.Fatal("the stub was meant to refuse the first IPv4 build")
	}

	if r.tick() {
		t.Fatal("one failed DNS-over-TCP build made the monitor rebuild the whole firewall")
	}
	if !r.has("ip") {
		t.Fatal("the monitor left the IPv4 DNS-over-TCP table out after nft worked again, so one passing error keeps DNS over TCP with the upstream resolver until b4 restarts")
	}
	if r.tick() {
		t.Error("the rebuilt IPv4 DNS-over-TCP table did not satisfy the next check")
	}
}

func TestAnApplyGivesTheNftMonitorItsDNSOverTCPRetriesBack(t *testing.T) {
	r := newDNSTCPMonitorRig(t)
	r.refuse(t, "ip", nftDNSTCPFailStreak)
	r.apply()
	for i := 0; i < nftDNSTCPFailStreak; i++ {
		r.tick()
	}
	if r.has("ip") {
		t.Fatal("the stub was meant to refuse every IPv4 build until the monitor gave up")
	}

	r.refuse(t, "ip", 1)
	r.apply()
	r.tick()
	if !r.has("ip") {
		t.Error("an apply after the monitor gave up must give it its retries back, or one later passing error lasts until b4 restarts")
	}
}

func TestADNSOverTCPFamilyAnApplyLeftOutIsBuiltOnceItsListenerIsUp(t *testing.T) {
	r := newDNSTCPMonitorRig(t)
	r.refuse(t, "ip", 1000)
	r.apply()
	for i := 0; i < nftDNSTCPFailStreak; i++ {
		r.tick()
	}

	SetDNSTCPListenerReady(false, false)
	r.apply()
	r.refuse(t, "ip", 0)
	SetDNSTCPListenerReady(true, true)
	r.tick()
	if !r.has("ip") {
		t.Error("the listener came back without an apply, and the failures from before the apply that left the family out kept its table missing")
	}
}

func TestTheNftMonitorWantsTheDNSAnswerRuleOfEachFamilyWhileOneOnlyLosesECHKeys(t *testing.T) {
	for _, ipv6Support := range []bool{false, true} {
		for _, gone := range []string{"ipv4", "ipv6"} {
			t.Run(fmt.Sprintf("ipv6 support %v, %s answer rule gone", ipv6Support, gone), func(t *testing.T) {
				r := newDNSTCPMonitorRig(t)
				r.cfg.Queue.IPv4Enabled = !ipv6Support
				r.cfg.Queue.IPv6Enabled = ipv6Support
				r.apply()
				if r.tick() {
					t.Fatal("the full listing must satisfy the monitor")
				}

				var kept []string
				for _, line := range strings.Split(readLog(t, filepath.Join(r.dir, "mangle")), "\n") {
					if !strings.Contains(line, "nfproto "+gone+" udp sport 53") {
						kept = append(kept, line)
					}
				}
				r.write(t, "mangle", strings.Join(kept, "\n"), 0o644)
				if !r.tick() {
					t.Errorf("the %s DNS answer rule was gone while the other family's stayed, and the monitor did not restore the rules", gone)
				}
			})
		}
	}
}
