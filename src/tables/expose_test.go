package tables

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type fakeXTStack struct {
	input  []string
	exists bool
	rules  []string
}

type fakeNftRule struct {
	handle int
	text   string
}

type fakeExposeFW struct {
	variant    map[string]string
	xt         map[string]*fakeXTStack
	xtListErr  map[string]string
	inputErr   map[string]string
	nftInput   map[string]bool
	nft        map[string][]fakeNftRule
	nftOrder   []string
	nftHooks   map[string]string
	nftOwned   map[string]bool
	nextHandle int
	calls      []string
	scripts    []string
}

func newFakeExposeFW() *fakeExposeFW {
	return &fakeExposeFW{
		variant:    map[string]string{},
		xt:         map[string]*fakeXTStack{},
		xtListErr:  map[string]string{},
		inputErr:   map[string]string{},
		nftInput:   map[string]bool{},
		nft:        map[string][]fakeNftRule{},
		nftHooks:   map[string]string{},
		nftOwned:   map[string]bool{},
		nextHandle: 100,
	}
}

func (f *fakeExposeFW) addNftChain(family, table, chain string, rules ...string) {
	key := family + " " + table + " " + chain
	f.nftOrder = append(f.nftOrder, key)
	for _, r := range rules {
		f.nextHandle++
		f.nft[key] = append(f.nft[key], fakeNftRule{handle: f.nextHandle, text: r})
	}
	if _, ok := f.nft[key]; !ok {
		f.nft[key] = []fakeNftRule{}
	}
}

func (f *fakeExposeFW) install(t *testing.T, binaries map[string]bool, procNames map[string]string) {
	t.Helper()
	stubBinaryPresence(t, binaries)
	origRun, origStdin, origProc := run, runNftStdin, readProcNetFile
	t.Cleanup(func() {
		run = origRun
		runNftStdin = origStdin
		readProcNetFile = origProc
	})
	run = f.run
	runNftStdin = f.runScript
	readProcNetFile = func(path string) ([]byte, error) {
		if s, ok := procNames[path]; ok {
			return []byte(s), nil
		}
		return nil, os.ErrNotExist
	}
}

func resetExposeState(t *testing.T) {
	t.Helper()
	reset := func() {
		exposeWanted, exposeBlocked, exposeChains = nil, nil, nil
		exposeFailures, exposeOwned = map[string]string{}, map[string]time.Time{}
		exposeSkip, exposeSynced, exposeInstalled, exposeClosed = false, false, false, false
		exposeNow = time.Now
		exposeStatus.Store(nil)
		xtVariantCache.Range(func(k, _ any) bool {
			xtVariantCache.Delete(k)
			return true
		})
		for {
			select {
			case <-exposeKick:
				continue
			default:
			}
			break
		}
	}
	reset()
	settle := exposeSettle
	exposeSettle = 10 * time.Millisecond
	t.Cleanup(func() {
		reset()
		exposeSettle = settle
	})
}

func (f *fakeExposeFW) run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) == 2 && args[1] == "--version" {
		return f.variant[args[0]], nil
	}
	if args[0] == "nft" {
		return f.nftCmd(args[1:])
	}
	st, ok := f.xt[args[0]]
	if !ok {
		return "", fmt.Errorf("%s: no such binary in the fake", args[0])
	}
	if msg, broken := f.xtListErr[args[0]]; broken && strings.Contains(strings.Join(args, " "), "-nL "+exposeChain) {
		return msg, errors.New("exit status 4")
	}
	if msg, broken := f.inputErr[args[0]]; broken && strings.Contains(strings.Join(args, " "), "-L INPUT") {
		return msg, errors.New("exit status 1")
	}
	return f.xtCmd(st, args[1:])
}

func (f *fakeExposeFW) xtCmd(st *fakeXTStack, args []string) (string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-w":
		case "-t":
			if args[i+1] != "filter" {
				return "", fmt.Errorf("unexpected table %s", args[i+1])
			}
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	joined := strings.Join(rest, " ")
	switch {
	case joined == "-N "+exposeChain:
		if st.exists {
			return "iptables: Chain already exists.", errors.New("exit status 1")
		}
		st.exists, st.rules = true, nil
	case joined == "-F "+exposeChain:
		if !st.exists {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		st.rules = nil
	case joined == "-X "+exposeChain:
		if !st.exists {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		for _, j := range st.input {
			if j == exposeChain {
				return "iptables: Too many links.", errors.New("exit status 1")
			}
		}
		st.exists = false
	case strings.HasPrefix(joined, "-A "+exposeChain+" "):
		if !st.exists {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		st.rules = append(st.rules, strings.TrimPrefix(joined, "-A "+exposeChain+" "))
	case strings.HasPrefix(joined, "-I INPUT ") && strings.HasSuffix(joined, " -j "+exposeChain):
		if !st.exists {
			return "iptables v1.8: Couldn't load target `B4_EXPOSE'", errors.New("exit status 2")
		}
		pos, err := strconv.Atoi(strings.Fields(joined)[2])
		if err != nil || pos < 1 || pos > len(st.input)+1 {
			return "iptables: Index of insertion too big.", errors.New("exit status 1")
		}
		st.input = append(st.input[:pos-1], append([]string{exposeChain}, st.input[pos-1:]...)...)
	case joined == "-D INPUT -j "+exposeChain:
		for i, j := range st.input {
			if j == exposeChain {
				st.input = append(st.input[:i], st.input[i+1:]...)
				return "", nil
			}
		}
		return "iptables: Bad rule (does a matching rule exist in that chain?).", errors.New("exit status 1")
	case strings.HasPrefix(joined, "-D INPUT "):
		n, err := strconv.Atoi(strings.TrimPrefix(joined, "-D INPUT "))
		if err != nil || n < 1 || n > len(st.input) {
			return "iptables: Index of deletion too big.", errors.New("exit status 1")
		}
		st.input = append(st.input[:n-1], st.input[n:]...)
	case joined == "-L INPUT -n --line-numbers" || joined == "-nL INPUT":
		var b strings.Builder
		b.WriteString("Chain INPUT (policy DROP)\nnum  target     prot opt source               destination\n")
		for i, j := range st.input {
			target, extra := j, ""
			if strings.HasPrefix(j, "DROP match-set ") {
				target, extra = "DROP", "match-set "+strings.TrimPrefix(j, "DROP match-set ")+" src"
			}
			fmt.Fprintf(&b, "%-4d %-10s all  --  0.0.0.0/0            0.0.0.0/0            %s\n", i+1, target, extra)
		}
		return b.String(), nil
	case joined == "-nL "+exposeChain+" --line-numbers" || joined == "-nL "+exposeChain:
		if !st.exists {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		refs := 0
		for _, j := range st.input {
			if j == exposeChain {
				refs++
			}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Chain B4_EXPOSE (%d references)\nnum  target     prot opt source               destination\n", refs)
		for i, r := range st.rules {
			fields := strings.Fields(r)
			port := fields[len(fields)-3]
			fmt.Fprintf(&b, "%-4d ACCEPT     tcp  --  0.0.0.0/0            0.0.0.0/0            tcp dpt:%s\n", i+1, port)
		}
		return b.String(), nil
	default:
		return "", fmt.Errorf("fake iptables does not know %q", joined)
	}
	return "", nil
}

func (f *fakeExposeFW) nftCmd(args []string) (string, error) {
	joined := strings.Join(args, " ")
	switch {
	case joined == "list chains":
		return f.renderChains(), nil
	case strings.HasPrefix(joined, "list chain ") && strings.HasSuffix(joined, " filter INPUT"):
		fam := strings.Fields(joined)[2]
		if f.nftInput[fam] {
			return "", nil
		}
		return "Error: No such file or directory", errors.New("exit status 1")
	case strings.HasPrefix(joined, "-a list chain "):
		key := strings.TrimPrefix(joined, "-a list chain ")
		rules, ok := f.nft[key]
		if !ok {
			return "Error: No such file or directory", errors.New("exit status 1")
		}
		parts := strings.Fields(key)
		var b strings.Builder
		fmt.Fprintf(&b, "table %s %s {\n\tchain %s { # handle 1\n\t\ttype filter hook input priority filter; policy drop;\n", parts[0], parts[1], parts[2])
		for _, r := range rules {
			fmt.Fprintf(&b, "\t\t%s # handle %d\n", r.text, r.handle)
		}
		b.WriteString("\t}\n}\n")
		return b.String(), nil
	}
	return "", fmt.Errorf("fake nft does not know %q", joined)
}

func (f *fakeExposeFW) renderChains() string {
	var b strings.Builder
	for _, key := range f.nftOrder {
		parts := strings.Fields(key)
		hook := f.nftHooks[key]
		if hook == "" {
			hook = "type filter hook input priority filter; policy drop;"
		}
		fmt.Fprintf(&b, "table %s %s {\n\tchain %s {\n\t\t%s\n\t}\n\tchain forward {\n\t\ttype filter hook forward priority filter; policy drop;\n\t}\n}\n", parts[0], parts[1], parts[2], hook)
	}
	return b.String()
}

func (f *fakeExposeFW) runScript(script string) (string, error) {
	f.scripts = append(f.scripts, script)
	next := make(map[string][]fakeNftRule, len(f.nft))
	for k, v := range f.nft {
		next[k] = append([]fakeNftRule{}, v...)
	}
	handle := f.nextHandle
	for _, line := range strings.Split(strings.TrimSpace(script), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return "", fmt.Errorf("bad script line %q", line)
		}
		key := strings.Join(fields[2:5], " ")
		rules, ok := next[key]
		if !ok {
			return "Error: No such file or directory", fmt.Errorf("no chain %s", key)
		}
		if f.nftOwned[strings.Join(fields[2:4], " ")] {
			return "Error: Could not process rule: Operation not permitted", errors.New("exit status 1")
		}
		switch fields[0] + " " + fields[1] {
		case "delete rule":
			h, _ := strconv.Atoi(fields[6])
			found := false
			for i, r := range rules {
				if r.handle == h {
					next[key] = append(rules[:i], rules[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				return "Error: Could not process rule: No such file or directory", fmt.Errorf("no handle %d", h)
			}
		case "insert rule":
			handle++
			next[key] = append([]fakeNftRule{{handle: handle, text: strings.Join(fields[5:], " ")}}, rules...)
		default:
			return "", fmt.Errorf("unexpected script line %q", line)
		}
	}
	f.nft, f.nextHandle = next, handle
	return "", nil
}

func (f *fakeExposeFW) nftTexts(key string) []string {
	var out []string
	for _, r := range f.nft[key] {
		out = append(out, r.text)
	}
	return out
}

var (
	legacyRouterBinaries = map[string]bool{
		"iptables": true, "ip6tables": true, "iptables-legacy": false, "ip6tables-legacy": false,
		"iptables-nft": false, "ip6tables-nft": false, "nft": false,
	}
	legacyRouterProc = map[string]string{
		"/proc/net/ip_tables_names":  "nat\nmangle\nfilter\n",
		"/proc/net/ip6_tables_names": "mangle\nfilter\n",
	}
	mtprotoWildcard = []config.ExposedPort{{Service: config.ExposeMTProto, Port: 3258, V4: true, V6: true}}
)

func newLegacyRouter(t *testing.T) *fakeExposeFW {
	f := newFakeExposeFW()
	f.variant["iptables"] = "iptables v1.4.21"
	f.variant["ip6tables"] = "ip6tables v1.4.21"
	f.xt["iptables"] = &fakeXTStack{input: []string{"ACCEPT", "logdrop"}}
	f.xt["ip6tables"] = &fakeXTStack{input: []string{"ACCEPT", "DROP"}}
	f.install(t, legacyRouterBinaries, legacyRouterProc)
	return f
}

func TestExposeOpensTheUsersPortOnALegacyRouterForBothFamilies(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)

	SyncExposure(mtprotoWildcard, nil, false)

	for _, bin := range []string{"iptables", "ip6tables"} {
		st := f.xt[bin]
		if len(st.input) == 0 || st.input[0] != exposeChain {
			t.Fatalf("%s: the jump must sit at the top of INPUT, above the firmware's drop; INPUT=%v", bin, st.input)
		}
		if !reflect.DeepEqual(st.rules, []string{"-p tcp --dport 3258 -j ACCEPT"}) {
			t.Fatalf("%s: B4_EXPOSE=%v", bin, st.rules)
		}
	}
	got := ExposureStatus()
	if !reflect.DeepEqual(got.Chains, []string{"iptables filter INPUT", "ip6tables filter INPUT"}) || len(got.Ports) != 1 {
		t.Fatalf("status must report where the rules went: %+v", got)
	}
}

func TestExposeSkipsALegacyStackWhoseFilterTableWasNeverLoaded(t *testing.T) {
	resetExposeState(t)
	f := newFakeExposeFW()
	f.variant["iptables"] = "iptables v1.8.7 (legacy)"
	f.variant["ip6tables"] = "ip6tables v1.8.7 (legacy)"
	f.xt["iptables"] = &fakeXTStack{input: []string{"DROP"}}
	f.xt["ip6tables"] = &fakeXTStack{}
	f.install(t, legacyRouterBinaries, map[string]string{"/proc/net/ip_tables_names": "filter\n"})

	SyncExposure(mtprotoWildcard, nil, false)

	if f.xt["ip6tables"].exists || len(f.xt["ip6tables"].input) != 0 {
		t.Fatalf("no legacy IPv6 filter table exists, so nothing drops there and b4 must not load one: %+v", f.xt["ip6tables"])
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "ip6tables -w") {
			t.Fatalf("touching ip6tables would instantiate its filter table: %s", c)
		}
	}
}

func TestExposeRestoresTheRuleAfterTheFirewallRebuildsINPUT(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)

	f.xt["iptables"].input = []string{"ACCEPT", "logdrop"}
	f.xt["ip6tables"].input = []string{"DROP"}
	f.xt["ip6tables"].exists, f.xt["ip6tables"].rules = false, nil
	exposeCheck()

	for _, bin := range []string{"iptables", "ip6tables"} {
		st := f.xt[bin]
		if st.input[0] != exposeChain || strings.Count(strings.Join(st.input, " "), exposeChain) != 1 {
			t.Fatalf("%s: after a rebuild the jump must come back exactly once at the top: %v", bin, st.input)
		}
		if !reflect.DeepEqual(st.rules, []string{"-p tcp --dport 3258 -j ACCEPT"}) {
			t.Fatalf("%s: B4_EXPOSE=%v", bin, st.rules)
		}
	}
}

func TestExposeLeavesIntactRulesAloneOnAPeriodicCheck(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)
	f.calls = nil

	exposeCheck()

	for _, c := range f.calls {
		if strings.Contains(c, " -F ") || strings.Contains(c, " -I ") || strings.Contains(c, " -A ") || strings.Contains(c, " -D ") {
			t.Fatalf("a check that finds every rule in place must not rewrite them: %s", c)
		}
	}
}

func TestExposeTurnedOffRemovesChainAndJump(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)
	SyncExposure(nil, nil, false)

	for _, bin := range []string{"iptables", "ip6tables"} {
		st := f.xt[bin]
		if st.exists || strings.Contains(strings.Join(st.input, " "), exposeChain) {
			t.Fatalf("%s: switching expose off must remove the chain and its jump: %+v", bin, st)
		}
	}
	if s := ExposureStatus(); len(s.Chains) != 0 || len(s.Ports) != 0 {
		t.Fatalf("status after turning off: %+v", s)
	}
}

func TestExposeFirstSyncSweepsLeftoversOfACrash(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	f.xt["iptables"].exists = true
	f.xt["iptables"].rules = []string{"-p tcp --dport 7000 -j ACCEPT"}
	f.xt["iptables"].input = []string{exposeChain, "ACCEPT", "logdrop"}

	SyncExposure(nil, nil, false)

	if st := f.xt["iptables"]; st.exists || strings.Contains(strings.Join(st.input, " "), exposeChain) {
		t.Fatalf("a port left open by a crashed b4 must be closed at the next start: %+v", st)
	}
}

func TestExposeUnderSkipSetupNeverTouchesTheFirewall(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)

	SyncExposure(mtprotoWildcard, nil, true)
	if len(f.calls) != 0 {
		t.Fatalf("with Skip setup on b4 must not run a single firewall command, ran:\n%s", strings.Join(f.calls, "\n"))
	}
	if s := ExposureStatus(); !s.SkipSetup || len(s.Chains) != 0 {
		t.Fatalf("status must say skip_setup and list no chains: %+v", s)
	}
	exposeCheck()
	ClearExposure()
	if len(f.calls) != 0 {
		t.Fatalf("checks and shutdown under Skip setup must stay away from the firewall, ran:\n%s", strings.Join(f.calls, "\n"))
	}
}

func TestExposeTurningSkipSetupOnRemovesWhatB4Added(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)
	SyncExposure(mtprotoWildcard, nil, true)

	if f.xt["iptables"].exists || f.xt["ip6tables"].exists {
		t.Fatalf("b4 must take back its own rules when it stops managing the firewall")
	}
}

func TestExposeSpecificIPv4BindStaysOffTheIPv6Stack(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure([]config.ExposedPort{{Service: config.ExposeSocks5, Port: 1080, Address: "192.168.1.1", V4: true}}, nil, false)

	if !reflect.DeepEqual(f.xt["iptables"].rules, []string{"-d 192.168.1.1 -p tcp --dport 1080 -j ACCEPT"}) {
		t.Fatalf("IPv4 rule must carry the bind address: %v", f.xt["iptables"].rules)
	}
	if f.xt["ip6tables"].exists {
		t.Fatalf("an IPv4-only listener needs nothing in ip6tables")
	}
}

func newUfwHost(t *testing.T) *fakeExposeFW {
	f := newFakeExposeFW()
	f.variant["iptables"] = "iptables v1.8.10 (nf_tables)"
	f.variant["ip6tables"] = "ip6tables v1.8.10 (nf_tables)"
	f.xt["iptables-nft"] = &fakeXTStack{input: []string{"ufw-before-input", "ufw-reject-input"}}
	f.xt["ip6tables-nft"] = &fakeXTStack{input: []string{"ufw6-before-input"}}
	f.nftInput["ip"], f.nftInput["ip6"] = true, true
	f.addNftChain("ip", "filter", "INPUT")
	f.addNftChain("ip6", "filter", "INPUT")
	f.addNftChain("inet", "b4_route", "input")
	f.install(t, map[string]bool{
		"iptables": true, "ip6tables": true, "iptables-legacy": true, "ip6tables-legacy": true,
		"iptables-nft": true, "ip6tables-nft": true, "nft": true,
	}, map[string]string{})
	return f
}

func TestExposeOnAUfwHostGoesThroughIptablesNftNotRawNft(t *testing.T) {
	resetExposeState(t)
	f := newUfwHost(t)

	SyncExposure(mtprotoWildcard, nil, false)

	if got, want := f.xt["iptables-nft"].input, []string{"ufw-before-input", exposeChain, "ufw-reject-input"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the jump must follow ufw-before-input, which holds ufw's deny rules and fail2ban's ufw bans, and precede ufw's reject: got %v, want %v", got, want)
	}
	if got, want := f.xt["ip6tables-nft"].input, []string{"ufw6-before-input", exposeChain}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IPv6 follows ufw6-before-input: got %v, want %v", got, want)
	}
	if len(f.scripts) != 0 {
		t.Fatalf("iptables-nft owns ip filter INPUT; native nft rules there confuse older iptables-nft, ran:\n%s", strings.Join(f.scripts, "\n"))
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "iptables-legacy") || strings.HasPrefix(c, "ip6tables-legacy") {
			t.Fatalf("no legacy table is loaded, so the legacy binaries must stay unused: %s", c)
		}
	}
}

func newFw4Router(t *testing.T) *fakeExposeFW {
	f := newFakeExposeFW()
	f.addNftChain("inet", "fw4", "input", `iif "lo" accept comment "!fw4: Accept traffic from loopback"`, `tcp dport 3258 accept comment "users own rule"`, `ct state vmap { established : accept, related : accept, invalid : drop }`)
	f.addNftChain("inet", "b4_mangle", "input")
	f.install(t, map[string]bool{
		"iptables": false, "ip6tables": false, "iptables-legacy": false, "ip6tables-legacy": false,
		"iptables-nft": false, "ip6tables-nft": false, "nft": true,
	}, map[string]string{})
	return f
}

func TestExposeOnFw4InsertsIntoItsInputChainAndOnlyRemovesItsOwnRule(t *testing.T) {
	resetExposeState(t)
	f := newFw4Router(t)
	key := "inet fw4 input"

	SyncExposure(mtprotoWildcard, nil, false)
	texts := f.nftTexts(key)
	if len(texts) != 4 || texts[0] != `tcp dport 3258 accept comment "b4-expose:mtproto"` {
		t.Fatalf("the accept must go to the top of fw4's own input chain, got %q", texts)
	}
	if got := ExposureStatus().Chains; !reflect.DeepEqual(got, []string{"nft inet fw4 input"}) {
		t.Fatalf("b4's own tables must never be targets, chains=%v", got)
	}

	SyncExposure(nil, nil, false)
	texts = f.nftTexts(key)
	if len(texts) != 3 || texts[1] != `tcp dport 3258 accept comment "users own rule"` {
		t.Fatalf("removal must take only b4's rule and leave the user's identical one, got %q", texts)
	}
}

func TestExposeOnFw4RestoresAfterAReloadFlushesTheChain(t *testing.T) {
	resetExposeState(t)
	f := newFw4Router(t)
	SyncExposure(mtprotoWildcard, nil, false)

	f.nft["inet fw4 input"] = []fakeNftRule{{handle: 900, text: `ct state vmap { established : accept, related : accept, invalid : drop }`}}
	exposeCheck()

	if texts := f.nftTexts("inet fw4 input"); len(texts) != 2 || !strings.Contains(texts[0], "b4-expose:mtproto") {
		t.Fatalf("fw4 reload flushed the rule, the check must put it back: %q", texts)
	}
}

func TestExposeOnNftWithASpecificAddressUsesTheMatchingFamily(t *testing.T) {
	resetExposeState(t)
	f := newFw4Router(t)
	SyncExposure([]config.ExposedPort{
		{Service: config.ExposeMTProto, Port: 443, Address: "203.0.113.5", V4: true},
		{Service: config.ExposeSocks5, Port: 1080, Address: "2001:db8::1", V6: true},
	}, nil, false)

	texts := f.nftTexts("inet fw4 input")
	want := []string{
		`ip daddr 203.0.113.5 tcp dport 443 accept comment "b4-expose:mtproto"`,
		`ip6 daddr 2001:db8::1 tcp dport 1080 accept comment "b4-expose:socks5"`,
	}
	if !reflect.DeepEqual(texts[:2], want) {
		t.Fatalf("got %q, want %q", texts[:2], want)
	}
}

func TestExposeKickRechecksAfterTheFirewallSettles(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)
	stop := StartExposureWatch(time.Hour, nil)
	defer stop()

	f.xt["iptables"].input = []string{"logdrop"}
	KickExposure()
	KickExposure()

	deadline := time.Now().Add(2 * time.Second)
	for {
		exposeMu.Lock()
		restored := len(f.xt["iptables"].input) > 0 && f.xt["iptables"].input[0] == exposeChain
		exposeMu.Unlock()
		if restored {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("SIGUSR1 from a firewall hook must restore the jump without waiting for the next tick: %v", f.xt["iptables"].input)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestParseNftInputChainsAcrossVersions(t *testing.T) {
	out := `table inet fw4 {
	chain input {
		type filter hook input priority filter; policy drop;
	}
	chain input_wan {
	}
	chain dstnat {
		type nat hook prerouting priority dstnat; policy accept;
	}
}
table ip filter {
	chain INPUT {
		type filter hook input priority 0; policy accept;
	}
}
table ip6 legacyname {
	chain in {
		type filter hook input priority 0; policy drop;
	}
}
table inet firewalld {
	chain filter_INPUT {
		type filter hook input priority filter + 10; policy accept;
	}
}
table inet b4_route {
	chain input {
		type filter hook input priority filter; policy accept;
	}
}
table bridge filter {
	chain input {
		type filter hook input priority filter; policy accept;
	}
}
table ip nat {
	chain INPUT {
		type nat hook input priority 100; policy accept;
	}
}`
	names := func(targets []exposeTarget) []string {
		var out []string
		for _, t := range targets {
			out = append(out, t.name())
		}
		return out
	}

	got := names(parseNftInputChains(out, map[string]bool{"ip": true}))
	want := []string{"nft inet fw4 input", "nft ip6 legacyname in", "nft inet firewalld filter_INPUT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("with iptables-nft handling ip filter: got %v, want %v", got, want)
	}

	got = names(parseNftInputChains(out, nil))
	want = []string{"nft inet fw4 input", "nft ip filter INPUT", "nft ip6 legacyname in", "nft inet firewalld filter_INPUT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("without an iptables-nft binary the ip filter chain is handled natively: got %v, want %v", got, want)
	}
}

func TestListedDestPortsReadsIptablesAndIp6tablesListings(t *testing.T) {
	v4 := `Chain B4_EXPOSE (1 references)
num  target     prot opt source               destination
1    ACCEPT     tcp  --  0.0.0.0/0            0.0.0.0/0            tcp dpt:3258
2    ACCEPT     tcp  --  0.0.0.0/0            192.168.1.1          tcp dpt:1080
`
	v6 := `Chain B4_EXPOSE (1 references)
num  target     prot opt source               destination
1    ACCEPT     tcp      ::/0                 ::/0                 tcp dpt:3258
`
	if got := listedDestPorts(v4); !reflect.DeepEqual(got, []int{3258, 1080}) {
		t.Fatalf("iptables listing: %v", got)
	}
	if got := listedDestPorts(v6); !reflect.DeepEqual(got, []int{3258}) {
		t.Fatalf("ip6tables listing: %v", got)
	}
}

func TestExposeClearAtShutdownOnlyTouchesTheFirewallWhenB4AddedSomething(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(nil, nil, false)
	f.calls = nil

	ClearExposure()
	if len(f.calls) != 0 {
		t.Fatalf("with nothing exposed, stopping b4 must not run firewall commands, ran:\n%s", strings.Join(f.calls, "\n"))
	}

	exposeMu.Lock()
	exposeClosed = false
	exposeMu.Unlock()
	SyncExposure(mtprotoWildcard, nil, false)
	ClearExposure()
	if f.xt["iptables"].exists || f.xt["ip6tables"].exists {
		t.Fatal("stopping b4 must close the ports it opened")
	}
}

func TestExposeSweepRemovesLeftoversEvenFromAnotherProcess(t *testing.T) {
	resetExposeState(t)
	f := newFw4Router(t)
	f.nft["inet fw4 input"] = append([]fakeNftRule{{handle: 5, text: `tcp dport 7000 accept comment "b4-expose:web_server"`}}, f.nft["inet fw4 input"]...)

	SweepExposure()

	for _, text := range f.nftTexts("inet fw4 input") {
		if strings.Contains(text, exposeCommentTag) {
			t.Fatalf("--clear-tables must remove every rule b4 ever added, found %q", text)
		}
	}
	if len(f.nftTexts("inet fw4 input")) != 3 {
		t.Fatalf("the sweep must leave fw4's own rules alone: %q", f.nftTexts("inet fw4 input"))
	}
}

func TestExposeStatusReportsARuleThatCouldNotBeAdded(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	delete(f.xt, "ip6tables")

	SyncExposure(mtprotoWildcard, nil, false)
	s := ExposureStatus()
	if !reflect.DeepEqual(s.Chains, []string{"iptables filter INPUT"}) || !strings.Contains(s.Error, "ip6tables filter INPUT") {
		t.Fatalf("a failed target must be reported so the share dialog can say the port is not open: %+v", s)
	}

	f.xt["ip6tables"] = &fakeXTStack{input: []string{"DROP"}}
	exposeCheck()
	if s := ExposureStatus(); s.Error != "" || len(s.Chains) != 2 {
		t.Fatalf("once the rule lands the error must clear: %+v", s)
	}
}
