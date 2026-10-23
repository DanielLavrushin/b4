package tables

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func (m *mockRouteBackend) revertChainRules(snap routeChainSnapshot) {}

type revertNftRule struct {
	handle int
	text   string
}

type revertFirewall struct {
	mu     sync.Mutex
	nft    map[string][]revertNftRule
	ipt    map[string][][]string
	handle int
	refuse string
}

func stubRevertFirewall(t *testing.T) *revertFirewall {
	t.Helper()
	f := &revertFirewall{nft: map[string][]revertNftRule{}, ipt: map[string][][]string{}}
	prevRun, prevLogged, prevDel, prevProto := run, runLogged, routeDelRuleLoop, routeIPSupportsProto
	t.Cleanup(func() {
		run, runLogged, routeDelRuleLoop, routeIPSupportsProto = prevRun, prevLogged, prevDel, prevProto
	})
	run = f.run
	runLogged = func(op string, args ...string) bool {
		_, err := f.run(args...)
		return err == nil
	}
	routeDelRuleLoop = func(bool, string, string) {}
	routeIPSupportsProto = func() bool { return false }
	return f
}

func (f *revertFirewall) refuseRules(needle string) {
	f.mu.Lock()
	f.refuse = needle
	f.mu.Unlock()
}

func (f *revertFirewall) run(args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(args) == 0 {
		return "", nil
	}
	switch args[0] {
	case "nft":
		return f.nftCmd(args[1:])
	case backendIPTables, backendIP6Tables:
		return f.iptCmd(args[0], args[1:])
	}
	return "", nil
}

func (f *revertFirewall) refused(line string) bool {
	return f.refuse != "" && strings.Contains(line, f.refuse)
}

func (f *revertFirewall) nftCmd(args []string) (string, error) {
	line := strings.Join(args, " ")
	switch {
	case len(args) >= 5 && args[0] == "add" && args[1] == "rule":
		if f.refused(line) {
			return "Error: Could not process rule: Operation not supported", errors.New("exit status 1")
		}
		f.handle++
		f.nft[args[4]] = append(f.nft[args[4]], revertNftRule{handle: f.handle, text: strings.Join(args[5:], " ")})
	case len(args) == 6 && args[0] == "-a" && args[1] == "list" && args[2] == "chain":
		var sb strings.Builder
		fmt.Fprintf(&sb, "table inet %s {\n\tchain %s {\n", args[4], args[5])
		for _, r := range f.nft[args[5]] {
			fmt.Fprintf(&sb, "\t\t%s # handle %d\n", r.text, r.handle)
		}
		sb.WriteString("\t}\n}\n")
		return sb.String(), nil
	case len(args) == 7 && args[0] == "delete" && args[1] == "rule" && args[5] == "handle":
		rules := f.nft[args[4]]
		for i, r := range rules {
			if strconv.Itoa(r.handle) == args[6] {
				f.nft[args[4]] = append(rules[:i:i], rules[i+1:]...)
				return "", nil
			}
		}
		return "Error: Could not process rule: No such file or directory", errors.New("exit status 1")
	case len(args) == 5 && args[0] == "flush" && args[1] == "chain":
		delete(f.nft, args[4])
	}
	return "", nil
}

func (f *revertFirewall) iptCmd(cmd string, args []string) (string, error) {
	if len(args) < 5 || args[0] != "-w" || args[1] != "-t" {
		return "", nil
	}
	table, op, chain, rest := args[2], args[3], args[4], args[5:]
	key := cmd + "|" + table + "|" + chain
	switch op {
	case "-A":
		if f.refused(strings.Join(args, " ")) {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		f.ipt[key] = append(f.ipt[key], rest)
	case "-I":
		if len(rest) > 0 {
			rest = rest[1:]
		}
		f.ipt[key] = append([][]string{rest}, f.ipt[key]...)
	case "-L":
		var sb strings.Builder
		fmt.Fprintf(&sb, "Chain %s (0 references)\nnum  target     prot opt source               destination\n", chain)
		for i, rule := range f.ipt[key] {
			target := "-"
			if j := slices.Index(rule, "-j"); j >= 0 && j+1 < len(rule) {
				target = rule[j+1]
			}
			fmt.Fprintf(&sb, "%-4d %-10s all  --  0.0.0.0/0            0.0.0.0/0\n", i+1, target)
		}
		return sb.String(), nil
	case "-D":
		rules := f.ipt[key]
		if len(rest) == 1 {
			if n, err := strconv.Atoi(rest[0]); err == nil {
				if n < 1 || n > len(rules) {
					return "iptables: Index of deletion too big.", errors.New("exit status 1")
				}
				f.ipt[key] = append(rules[:n-1:n-1], rules[n:]...)
				return "", nil
			}
		}
		for i, rule := range rules {
			if slices.Equal(rule, rest) {
				f.ipt[key] = append(rules[:i:i], rules[i+1:]...)
				return "", nil
			}
		}
		return "iptables: Bad rule (does a matching rule exist in that chain?).", errors.New("exit status 1")
	case "-F":
		delete(f.ipt, key)
	}
	return "", nil
}

func (f *revertFirewall) chainRules(be routeBackend, chain string, isMangle bool) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	if be.name() == backendNFTables {
		for _, r := range f.nft[chain] {
			out = append(out, fmt.Sprintf("%d %s", r.handle, r.text))
		}
		return out
	}
	for _, cmd := range []string{backendIPTables, backendIP6Tables} {
		for _, rule := range f.ipt[cmd+"|"+iptTable(isMangle)+"|"+chain] {
			out = append(out, cmd+" "+strings.Join(rule, " "))
		}
	}
	return out
}

func (f *revertFirewall) setChains(be routeBackend, st routeState) [][]string {
	return [][]string{
		f.chainRules(be, st.chainPre, true),
		f.chainRules(be, st.chainOut, true),
		f.chainRules(be, st.chainSNAT, false),
	}
}

func sameChains(a, b [][]string) bool {
	return slices.EqualFunc(a, b, func(x, y []string) bool { return slices.Equal(x, y) })
}

func TestAFailedInstallTakesBackTheRulesItAppended(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			familyResetGlobals(t)
			gwIsolate(t)
			fw := stubRevertFirewall(t)
			stubBinaries(t, "ip", backendIPTables, backendIP6Tables, "ipset")
			cfg := familyTestConfig(true, true)
			set := gwTestSet("revert", "lo", "")
			cfg.Sets = []*config.SetConfig{set}
			st := gwResolvedState(cfg, set)
			var be routeBackend = &routeNftBackend{}
			guard := `iifname "lo" return`
			if engine == backendIPTables {
				be = &routeIptBackend{}
				guard = "-A " + st.chainPre + " -i lo -j RETURN"
			}

			fw.refuseRules(guard)
			for attempt := 1; attempt <= 3; attempt++ {
				if err := routeEnsureRule(be, cfg, set, st, nil); err == nil {
					t.Fatalf("attempt %d: the guard was refused, yet the install reported success", attempt)
				}
				for i, rules := range fw.setChains(be, st) {
					if len(rules) != 0 {
						t.Fatalf("attempt %d left %d rules in chain %d of a set that never installed, so every retry piles up more:\n%s", attempt, len(rules), i, strings.Join(rules, "\n"))
					}
				}
			}

			fw.refuseRules("")
			if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
				t.Fatalf("routeEnsureRule: %v", err)
			}
			installed := fw.setChains(be, st)
			if len(installed[0]) == 0 || len(installed[1]) == 0 {
				t.Fatalf("the install that went through left empty chains: %v", installed)
			}

			fw.refuseRules(guard)
			for attempt := 1; attempt <= 3; attempt++ {
				if err := routeEnsureRule(be, cfg, set, st, nil); err == nil {
					t.Fatalf("rebuild %d: the guard was refused, yet the install reported success", attempt)
				}
				if got := fw.setChains(be, st); !sameChains(got, installed) {
					t.Fatalf("rebuild %d did not leave the chains as the last good install left them:\nwant %v\ngot  %v", attempt, installed, got)
				}
			}
		})
	}
}

func TestAFailedInstallKeepsThePreviousRulesOfAChainItCouldNotList(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			familyResetGlobals(t)
			gwIsolate(t)
			fw := stubRevertFirewall(t)
			stubBinaries(t, "ip", backendIPTables, backendIP6Tables, "ipset")
			cfg := familyTestConfig(true, true)
			set := gwTestSet("unlisted", "lo", "")
			cfg.Sets = []*config.SetConfig{set}
			st := gwResolvedState(cfg, set)
			var be routeBackend = &routeNftBackend{}
			guard := `iifname "lo" return`
			listing := "-a list chain inet " + routeNftTable + " " + st.chainPre
			if engine == backendIPTables {
				be = &routeIptBackend{}
				guard = "-A " + st.chainPre + " -i lo -j RETURN"
				listing = backendIPTables + " -w -t mangle -L " + st.chainPre + " "
			}
			if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
				t.Fatalf("routeEnsureRule: %v", err)
			}
			installed := fw.setChains(be, st)
			if len(installed[0]) == 0 {
				t.Fatalf("the first install left %s empty", st.chainPre)
			}

			unlisted := 1
			firewall := run
			run = func(args ...string) (string, error) {
				if unlisted > 0 && strings.Contains(strings.Join(args, " "), listing) {
					unlisted--
					return "Fatal: can't open lock file /run/xtables.lock: Resource temporarily unavailable", errors.New("exit status 4")
				}
				return firewall(args...)
			}
			fw.refuseRules(guard)
			if err := routeEnsureRule(be, cfg, set, st, nil); err == nil {
				t.Fatal("the guard was refused, yet the rebuild reported success")
			}
			if unlisted != 0 {
				t.Fatalf("the rebuild never listed %s before it started", st.chainPre)
			}

			got := fw.setChains(be, st)
			for _, rule := range installed[0] {
				if !slices.Contains(got[0], rule) {
					t.Fatalf("listing %s failed before the attempt, and the failed attempt removed the previous install's rule %q, so the set marks nothing while b4 believes it is installed:\n%s", st.chainPre, rule, strings.Join(got[0], "\n"))
				}
			}
			if !sameChains(got[1:], installed[1:]) {
				t.Fatalf("the chains that were listed must go back to what the last good install left:\nwant %v\ngot  %v", installed[1:], got[1:])
			}
		})
	}
}

func TestAProxySetWhoseQUICRejectFailsTakesBackItsRules(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			familyResetGlobals(t)
			localGuardReset(t)
			fw := stubRevertFirewall(t)
			stubBinaries(t, "ip", backendIPTables, backendIP6Tables, "ipset")
			prevSysctl := writeSysctl
			t.Cleanup(func() { writeSysctl = prevSysctl })
			writeSysctl = func(path, value string) {}
			cfg := familyTestConfig(true, true)
			set := orderTestSet("quicfail", config.RoutingModeProxy, nil)
			cfg.Sets = []*config.SetConfig{set}
			st := proxyGuardState(0x5300)
			st.setID, st.ipv6 = set.Id, true
			st.setV4, st.setV6 = routeBuildSetNames(set.Id)
			st.chainPre, st.chainOut, st.chainSNAT = routeBuildChainNames(set.Id)
			st.chainQUIC = routeBuildQUICChainName(set.Id)
			st.chainQOut = routeBuildQUICOutChainName(set.Id)
			st.quicReject = true
			var be routeBackend = &routeNftBackend{}
			if engine == backendIPTables {
				be = &routeIptBackend{}
			}

			refuse := true
			firewall := run
			run = func(args ...string) (string, error) {
				line := strings.Join(args, " ")
				if refuse && (strings.Contains(line, "add chain inet "+routeNftTable+" "+st.chainQUIC) || strings.Contains(line, "-N "+st.chainQUIC)) {
					return "Error: Could not process rule: Operation not supported", errors.New("exit status 1")
				}
				return firewall(args...)
			}

			for attempt := 1; attempt <= 3; attempt++ {
				if err := routeEnsureProxyRule(be, cfg, set, st, nil); err == nil {
					t.Fatalf("attempt %d: the QUIC chain was refused, yet the install reported success", attempt)
				}
				for i, rules := range fw.setChains(be, st) {
					if len(rules) != 0 {
						t.Fatalf("attempt %d left %d rules in chain %d of a proxy set that never installed, so every retry piles up more:\n%s", attempt, len(rules), i, strings.Join(rules, "\n"))
					}
				}
			}

			refuse = false
			if err := routeEnsureProxyRule(be, cfg, set, st, nil); err != nil {
				t.Fatalf("routeEnsureProxyRule: %v", err)
			}
			installed := fw.setChains(be, st)
			if len(installed[0]) == 0 || len(installed[1]) == 0 {
				t.Fatalf("the install that went through left empty chains: %v", installed)
			}

			refuse = true
			for attempt := 1; attempt <= 3; attempt++ {
				if err := routeEnsureProxyRule(be, cfg, set, st, nil); err == nil {
					t.Fatalf("rebuild %d: the QUIC chain was refused, yet the install reported success", attempt)
				}
				if got := fw.setChains(be, st); !sameChains(got, installed) {
					t.Fatalf("rebuild %d did not leave the chains as the last good install left them:\nwant %v\ngot  %v", attempt, installed, got)
				}
			}
		})
	}
}

func TestAnInstallThatFailsBeforeAppendingTouchesNoChain(t *testing.T) {
	familyResetGlobals(t)
	gwIsolate(t)
	fw := stubRevertFirewall(t)
	stubBinaries(t, "ip", "nft")
	cfg := familyTestConfig(true, false)
	set := gwTestSet("revertset", "lo", "")
	cfg.Sets = []*config.SetConfig{set}
	st := gwResolvedState(cfg, set)
	var ops []string
	run = func(args ...string) (string, error) {
		line := strings.Join(args, " ")
		ops = append(ops, line)
		if strings.HasPrefix(line, "nft add set") {
			return "Error: Could not process rule: Operation not supported", errors.New("exit status 1")
		}
		return fw.run(args...)
	}
	if err := routeEnsureRule(&routeNftBackend{}, cfg, set, st, nil); err == nil {
		t.Fatal("the set could not be created, yet the install reported success")
	}
	for _, op := range ops {
		if strings.Contains(op, "flush chain") || strings.Contains(op, "delete rule") {
			t.Errorf("nothing was appended, so nothing needs taking back, ran %q", op)
		}
	}
}

func ageInstallFailure(t *testing.T, setID string) {
	t.Helper()
	routeInstallFailedAt.Store(setID, time.Now().Add(-2*routeInstallFailureMemo))
}

func forgetInstallFailures(t *testing.T) {
	t.Helper()
	forget := func() {
		routeInstallFailedAt.Range(func(k, _ any) bool {
			routeInstallFailedAt.Delete(k)
			return true
		})
	}
	forget()
	t.Cleanup(forget)
}

func TestTheDNSPathWaitsOutARecentInstallFailure(t *testing.T) {
	familyResetGlobals(t)
	forgetInstallFailures(t)
	stubBinaries(t, "ip")
	prevRun := run
	t.Cleanup(func() { run = prevRun })
	run = func(args ...string) (string, error) { return "", nil }
	var attempts atomic.Int32
	be := &mockRouteBackend{ensureChainFn: func(string, bool) error {
		attempts.Add(1)
		return errors.New("module missing")
	}}
	routeEngine = be
	cfg := familyTestConfig(true, false)
	set := familyTestSet()
	cfg.Sets = []*config.SetConfig{set}

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.61")})
	if attempts.Load() != 1 || !routeInstallFailedRecently(set.Id) {
		t.Fatalf("the first answer must try the install once and remember the failure, tried %d times", attempts.Load())
	}
	for i := 0; i < 3; i++ {
		RoutingHandleDNS(cfg, set, []net.IP{net.IPv4(198, 51, 100, byte(62+i))})
	}
	if n := attempts.Load(); n != 1 {
		t.Fatalf("answers within a minute of a failed install ran the install again %d times; each attempt costs a burst of firewall commands and an error line", n-1)
	}

	ageInstallFailure(t, set.Id)
	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.70")})
	if attempts.Load() != 2 {
		t.Fatalf("once the failure is a minute old the next answer must try again, tried %d times in all", attempts.Load())
	}
}
