package tables

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

const dnsCopyTestSpec = "-p udp --dport 53 -j NFLOG --nflog-group 537 --nflog-prefix b4dns --nflog-threshold 1"

var dnsCopyTestJumps = map[string]string{
	"INPUT":   "-p udp --dport 53 -j B4_DNSCOPY",
	"FORWARD": "-p udp --dport 53 -j B4_DNSCOPY",
	"OUTPUT":  "! -o lo -p udp --dport 53 -j B4_DNSCOPY",
}

func dnsCopyTestManager(t *testing.T, facts dnsKernel, rawErr error, open func() (uint16, error)) (*IPTablesManager, *kmodFixture) {
	t.Helper()
	fx := newKmodFixture(t)
	manager := dnsQueryTestManager(t, rawErr)
	stubDNSKernel(t, facts)
	stubDNSCopyListener(t, open)
	origProc := readProcNetFile
	readProcNetFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	t.Cleanup(func() { readProcNetFile = origProc })
	noteDNSQueryPlacement(nil)
	t.Cleanup(func() { noteDNSQueryPlacement(nil) })
	return manager, fx
}

func freshDNSCopyManager(manager *IPTablesManager, rawErr map[string]error) *IPTablesManager {
	m := NewIPTablesManager(manager.cfg, false)
	stubProbes(m, backendIPTables, backendIP6Tables)
	for _, ipt := range []string{backendIPTables, backendIP6Tables} {
		if err, ok := rawErr[ipt]; ok {
			m.rawQueueSupport[ipt] = err
		} else {
			delete(m.rawQueueSupport, ipt)
		}
	}
	return m
}

func queuesDNSQueries(spec string) bool {
	fields := strings.Fields(spec)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "--dport" && fields[i+1] == "53" && (i == 0 || fields[i-1] != "!") {
			return slices.Contains(fields, "NFQUEUE")
		}
	}
	return false
}

func serveNatListings(t *testing.T, listings map[string]string) {
	t.Helper()
	recorded := run
	t.Cleanup(func() { run = recorded })
	run = func(args ...string) (string, error) {
		out, err := recorded(args...)
		if len(args) > 0 && iptLineHasTokens(strings.Join(args, " "), []string{"-t", "nat", "-S"}) {
			return listings[args[0]], nil
		}
		return out, err
	}
}

func TestKeeneticCopiesDNSQueriesWithNFLOG(t *testing.T) {
	buf := captureTablesLog(t)
	opened, openedAt := 0, -1
	var rec *firewallRecorder
	manager, _ := dnsCopyTestManager(t, keeneticDNSKernel, nil, func() (uint16, error) {
		opened++
		if openedAt < 0 && rec != nil {
			openedAt = len(rec.commands())
		}
		return 537, nil
	})
	bins := []string{backendIPTables, backendIP6Tables}
	rec = recordFirewall(t, nil)

	m, err := freshDNSCopyManager(manager, nil).buildManifest()
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	if opened == 0 {
		t.Fatalf("KeeneticOS must bind the NFLOG listener before it puts in rules that copy DNS queries to it")
	}
	for _, module := range []string{dnsCopyLogModule, dnsCopyModule} {
		if at := slices.Index(rec.commands(), "modprobe -q "+module); at < 0 || at >= openedAt {
			t.Errorf("NDMS loads %s only on demand, so b4 must load it before it binds the listener (loaded at %d, bound after %d commands)", module, at, openedAt)
		}
	}
	for _, ipt := range bins {
		if got := m.DNSQueries[ipt]; got != dnsCopied {
			t.Errorf("%s: DNS queries placed in %s, want copy: KeeneticOS runs its raw table after conntrack, so holding them anywhere keeps the clash", ipt, got)
		}
		if !manifestHasChain(m, ipt, "mangle", iptDNSCopyChainName) {
			t.Fatalf("%s: the mangle chain %s is missing", ipt, iptDNSCopyChainName)
		}
		if body, want := manifestSpecs(m, ipt, "mangle", iptDNSCopyChainName), []string{"-m mark --mark 0x8000/0x8efd8000 -j RETURN", dnsCopyTestSpec}; !slices.Equal(body, want) {
			t.Errorf("%s: %s must first return b4's own packets and then copy UDP to port 53 with exactly the spec iptables 1.4.21 accepts, got %q, want %q", ipt, iptDNSCopyChainName, body, want)
		}

		lastConnmark, firstJump := -1, -1
		jumps := map[string]int{}
		for i, r := range m.Rules {
			if r.IPT != ipt {
				continue
			}
			spec := strings.Join(r.Spec, " ")
			if r.Table == "raw" || r.Chain == iptRawChainName || strings.Contains(spec, iptRawChainName) {
				t.Errorf("%s: KeeneticOS never uses the raw table, got %s %s %s", ipt, r.Table, r.Chain, spec)
			}
			if queuesDNSQueries(spec) {
				t.Errorf("%s: a DNS query queue rule in %s holds the queries NFLOG copies, so the clash stays: %s", ipt, r.Chain, spec)
			}
			if r.Chain == iptDNSCopyChainName {
				want := "A"
				if strings.HasSuffix(spec, "-j RETURN") {
					want = "I"
				}
				if r.Action != want {
					t.Errorf("%s: in %s the return for b4's own packets must be inserted, so a restore of that rule alone puts it back above the copy rule, and the copy rule appended, got action %q for %s", ipt, iptDNSCopyChainName, r.Action, spec)
				}
			}
			if strings.Contains(strings.ToLower(spec), "connmark") {
				lastConnmark = i
			}
			if !strings.HasSuffix(spec, "-j "+iptDNSCopyChainName) {
				continue
			}
			if firstJump < 0 {
				firstJump = i
			}
			jumps[r.Chain]++
			if want := dnsCopyTestJumps[r.Chain]; spec != want || r.Table != "mangle" {
				t.Errorf("%s: unexpected copy jump %s %s %q, want %q", ipt, r.Table, r.Chain, spec, want)
			}
			if r.Action != "I" {
				t.Errorf("%s: the %s copy jump must be inserted at the top, so no rule above it ends the chain before the copy; got action %q", ipt, r.Chain, r.Action)
			}
		}
		for chain := range dnsCopyTestJumps {
			if jumps[chain] != 1 {
				t.Errorf("%s: mangle %s must jump to %s exactly once, got %d", ipt, chain, iptDNSCopyChainName, jumps[chain])
			}
		}
		if lastConnmark < 0 || firstJump < lastConnmark {
			t.Errorf("%s: the copy jumps must come after the connmark rules in the manifest, so -I leaves them on top of mangle OUTPUT (connmark at %d, first jump at %d)", ipt, lastConnmark, firstJump)
		}

		capture := manifestSpecs(m, ipt, "mangle", iptChainName)
		ret, queue := -1, -1
		for i, s := range capture {
			if s == strings.Join(iptDNSQueryCaptureReturn(), " ") {
				ret = i
			}
			if queue < 0 && strings.HasPrefix(s, "-p udp") && strings.Contains(s, "NFQUEUE") {
				queue = i
			}
		}
		if ret < 0 || queue < 0 || ret > queue {
			t.Errorf("%s: %s must return UDP to port 53 ahead of its UDP queue rules, or a set on UDP port 53 holds the copied queries after conntrack, got %q", ipt, iptChainName, capture)
		}
		for _, chain := range []string{"B4_PREROUTING", "OUTPUT"} {
			if !hasSpecWith(manifestSpecs(m, ipt, "mangle", chain), "--sport 53 ! --dport 53", "NFQUEUE") {
				t.Errorf("%s %s: the answer rule must skip packets to port 53, or a query from port 53 is held as an answer", ipt, chain)
			}
		}
	}
	if raw := commandsMatching(rec.commands(), rawTableCommand); len(raw) > 0 {
		t.Errorf("KeeneticOS runs its raw table after conntrack, so b4 must never probe or touch it there:\n%s", strings.Join(raw, "\n"))
	}

	rec.reset()
	for apply := 1; apply <= 2; apply++ {
		if err := freshDNSCopyManager(manager, nil).Apply(); err != nil {
			t.Fatalf("apply %d: %v", apply, err)
		}
	}
	cmds := rec.commands()
	for _, ipt := range bins {
		want := []string{ipt + " -w -t mangle -A " + iptDNSCopyChainName + " " + dnsCopyTestSpec}
		for chain, spec := range dnsCopyTestJumps {
			want = append(want, ipt+" -w -t mangle -I "+chain+" "+spec)
		}
		for _, w := range want {
			if !slices.Contains(cmds, w) {
				t.Errorf("%s: missing command %q", ipt, w)
			}
		}
		if n := logLinesWith(buf.String(), "IPTABLES["+ipt+"]: KeeneticOS runs its raw table after conntrack (kernel 4.9-ndm-5), so b4 copies DNS queries with NFLOG (group 537) instead of holding them in its queue"); n != 1 {
			t.Errorf("%s: the copy line appeared %d times over two applies, want once:\n%s", ipt, n, buf.String())
		}
	}
	for _, c := range cmds {
		if strings.Contains(c, "--nflog-size") || strings.Contains(c, "--nflog-range") {
			t.Errorf("Entware's iptables 1.4.21 has no --nflog-size, and --nflog-range never worked: %s", c)
		}
	}
	if raw := commandsMatching(cmds, rawTableCommand); len(raw) > 0 {
		t.Errorf("an apply on KeeneticOS without iptable_raw ran raw table commands:\n%s", strings.Join(raw, "\n"))
	}
	if strings.Contains(buf.String(), "kmod-ipt-raw") {
		t.Errorf("the log names an OpenWrt package on KeeneticOS:\n%s", buf.String())
	}
}

func TestAMoveToOrFromCopyingIsLoggedAsSuch(t *testing.T) {
	buf := captureTablesLog(t)
	manager, _ := dnsCopyTestManager(t, keeneticDNSKernel, nil, func() (uint16, error) { return 537, nil })
	recordFirewall(t, nil)
	bins := []string{backendIPTables, backendIP6Tables}

	noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: dnsHeldMangle, backendIP6Tables: dnsHeldMangle})
	if err := freshDNSCopyManager(manager, nil).Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	DNSQueryCopyOpenFunc = func() (uint16, error) { return 0, errors.New("the NFLOG group is bound by another program") }
	if err := freshDNSCopyManager(manager, nil).Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	logged := buf.String()
	for _, ipt := range bins {
		for _, want := range []string{
			"IPTABLES[" + ipt + "]: DNS queries were held in the mangle table and will be copied with NFLOG, rebuilding the rules",
			"IPTABLES[" + ipt + "]: DNS queries were copied with NFLOG and will be held in the mangle table, rebuilding the rules",
		} {
			if n := logLinesWith(logged, want); n != 1 {
				t.Errorf("%q appeared %d times, want once:\n%s", want, n, logged)
			}
		}
	}
	if strings.Contains(logged, "between the raw and mangle tables") {
		t.Errorf("KeeneticOS never uses the raw table, yet the log says DNS queries moved through it:\n%s", logged)
	}
}

func TestVanillaAffectedKernelTriesRawThenCopy(t *testing.T) {
	rawErr := errors.New("iptables cannot queue packets from the raw table (exit status 1)")
	for _, tc := range []struct {
		name   string
		rawErr map[string]error
		want   map[string]dnsQueryPlacement
	}{
		{
			name:   "raw table works",
			rawErr: map[string]error{backendIPTables: nil, backendIP6Tables: nil},
			want:   map[string]dnsQueryPlacement{backendIPTables: dnsHeldRaw, backendIP6Tables: dnsHeldRaw},
		},
		{
			name:   "raw table fails",
			rawErr: map[string]error{backendIPTables: rawErr, backendIP6Tables: rawErr},
			want:   map[string]dnsQueryPlacement{backendIPTables: dnsCopied, backendIP6Tables: dnsCopied},
		},
		{
			name:   "IPv4 raw table works, IPv6 raw table fails",
			rawErr: map[string]error{backendIPTables: nil, backendIP6Tables: rawErr},
			want:   map[string]dnsQueryPlacement{backendIPTables: dnsHeldRaw, backendIP6Tables: dnsCopied},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureTablesLog(t)
			opened := 0
			manager, _ := dnsCopyTestManager(t, affectedDNSKernel, nil, func() (uint16, error) {
				opened++
				return 537, nil
			})
			rec := recordFirewall(t, nil)

			m, err := freshDNSCopyManager(manager, tc.rawErr).buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			copied := 0
			for ipt, want := range tc.want {
				if got := m.DNSQueries[ipt]; got != want {
					t.Errorf("%s: DNS queries placed in %s, want %s", ipt, got, want)
				}
				if got := manifestHasChain(m, ipt, "raw", iptRawChainName); got != (want == dnsHeldRaw) {
					t.Errorf("%s: raw chain present = %v with DNS queries placed in %s", ipt, got, want)
				}
				if got := manifestHasChain(m, ipt, "mangle", iptDNSCopyChainName); got != (want == dnsCopied) {
					t.Errorf("%s: copy chain present = %v with DNS queries placed in %s", ipt, got, want)
				}
				if want != dnsCopied {
					continue
				}
				copied++
				line := "IPTABLES[" + ipt + "]: " + rawErr.Error() + ", and kernel 4.9.337 drops the second of two DNS queries a device sends at once from one socket when they wait in a queue after conntrack, so b4 copies DNS queries with NFLOG (group 537) instead of holding them"
				if n := logLinesWith(buf.String(), line); n != 1 {
					t.Errorf("%s: the copy line appeared %d times, want once:\n%s", ipt, n, buf.String())
				}
			}
			switch {
			case copied == 0 && opened != 0:
				t.Errorf("the raw table queues DNS queries ahead of conntrack, yet b4 bound the NFLOG listener %d times", opened)
			case copied > 0 && opened == 0:
				t.Errorf("DNS queries are copied, yet the NFLOG listener was never bound")
			}
			if copied == 0 {
				for _, c := range rec.commands() {
					if strings.Contains(c, "NFLOG") || strings.Contains(c, iptDNSCopyChainName) {
						t.Errorf("a router whose raw table works must not touch NFLOG: %s", c)
					}
				}
			}
		})
	}
}

func TestRejectedNFLOGProbeIsCachedUntilTheModuleLoads(t *testing.T) {
	buf := captureTablesLog(t)
	opened := 0
	manager, fx := dnsCopyTestManager(t, keeneticDNSKernel, nil, func() (uint16, error) {
		opened++
		return 537, nil
	})
	rec := recordFirewall(t, func(args []string) bool {
		return slices.Contains(args, "B4_MODULE_TEST") && slices.Contains(args, "NFLOG")
	})
	probed := func() bool {
		return len(commandsMatching(rec.commands(), func(args []string) bool {
			return slices.Contains(args, "B4_MODULE_TEST") && slices.Contains(args, "NFLOG")
		})) > 0
	}
	apply := func() Manifest {
		t.Helper()
		rec.reset()
		opened = 0
		m, err := freshDNSCopyManager(manager, nil).buildManifest()
		if err != nil {
			t.Fatalf("buildManifest: %v", err)
		}
		return m
	}

	m := apply()
	for _, ipt := range []string{backendIPTables, backendIP6Tables} {
		if got := m.DNSQueries[ipt]; got != dnsHeldMangle {
			t.Errorf("%s: an iptables that rejects NFLOG must leave DNS queries in mangle, got %s", ipt, got)
		}
		if n := logLinesWith(buf.String(), "IPTABLES["+ipt+"]: b4 cannot copy DNS queries with NFLOG ("+ipt+" rejected the NFLOG target"); n != 1 {
			t.Errorf("%s: the fallback warning must name the rejected target once, got %d:\n%s", ipt, n, buf.String())
		}
	}
	if !probed() || opened == 0 {
		t.Fatalf("the first apply must bind the listener and probe the NFLOG target:\n%s", strings.Join(rec.commands(), "\n"))
	}
	noteDNSQueryPlacement(m.DNSQueries)
	if st := DNSQueryPlacement(); !strings.Contains(st.CopyError, "rejected the NFLOG target") {
		t.Errorf("System Info must carry why the copy failed, got %q", st.CopyError)
	}

	apply()
	if probed() || opened != 0 || slices.Contains(rec.commands(), "modprobe -q "+dnsCopyModule) {
		t.Errorf("a rejection with xt_NFLOG absent must be cached, yet the next apply probed again (opened %d):\n%s", opened, strings.Join(rec.commands(), "\n"))
	}

	fx.loaded(dnsCopyModule)
	apply()
	if !probed() || opened == 0 {
		t.Errorf("once xt_NFLOG has loaded, the next apply must probe again")
	}
	apply()
	if !probed() {
		t.Errorf("a rejection while xt_NFLOG is loaded, as when another program flushes the probe chain, must not be cached")
	}
}

func TestPortChangingNatRuleIsWarnedInCopyMode(t *testing.T) {
	rawErr := errors.New("iptables cannot queue packets from the raw table (exit status 1)")
	listings := map[string]string{
		backendIPTables: "-P PREROUTING ACCEPT\n" +
			"-A PREROUTING -i br0 -p udp -m udp --dport 53 -j REDIRECT --to-ports 5353\n" +
			"-A PREROUTING -p udp -m udp --dport 53 -j REDIRECT --to-ports 53\n" +
			"-A PREROUTING -p tcp -m tcp --dport 53 -j REDIRECT --to-ports 5353\n" +
			"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination 192.168.1.1\n" +
			"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination 192.168.1.1:53\n" +
			"-A PREROUTING -p udp -m udp --dport 853 -j REDIRECT --to-ports 5353\n" +
			"-A OUTPUT -d 10.0.0.0/8 -p udp -m udp --dport 53 -j DNAT --to-destination 192.168.1.1:5300\n",
		backendIP6Tables: "-P PREROUTING ACCEPT\n" +
			"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination [fd00::1]:5353\n" +
			"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination fd00::1\n" +
			"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination [fd00::1]:53\n",
	}
	tail := "; b4 copies DNS queries on this router and cannot rewrite answers that come back from another port, so the sets do not answer, block or redirect the names those devices ask for"
	warnings := map[string]string{
		backendIPTables + " 5353":  `IPTABLES[iptables]: the nat rule "-A PREROUTING -i br0 -p udp -m udp --dport 53 -j REDIRECT --to-ports 5353" moves DNS queries to port 5353` + tail,
		backendIPTables + " 5300":  `IPTABLES[iptables]: the nat rule "-A OUTPUT -d 10.0.0.0/8 -p udp -m udp --dport 53 -j DNAT --to-destination 192.168.1.1:5300" moves DNS queries to port 5300` + tail,
		backendIP6Tables + " 5353": `IPTABLES[ip6tables]: the nat rule "-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination [fd00::1]:5353" moves DNS queries to port 5353` + tail,
	}
	natListed := func(cmds []string, ipt string) bool {
		return slices.Contains(cmds, ipt+" -w -t nat -S")
	}

	t.Run("copied on a vanilla kernel", func(t *testing.T) {
		buf := captureTablesLog(t)
		manager, _ := dnsCopyTestManager(t, affectedDNSKernel, nil, func() (uint16, error) { return 537, nil })
		rec := recordFirewall(t, nil)
		serveNatListings(t, listings)
		raw := map[string]error{backendIPTables: rawErr, backendIP6Tables: rawErr}
		for apply := 1; apply <= 2; apply++ {
			rec.reset()
			if err := freshDNSCopyManager(manager, raw).Apply(); err != nil {
				t.Fatalf("apply %d: %v", apply, err)
			}
			cmds := rec.commands()
			for _, ipt := range []string{backendIPTables, backendIP6Tables} {
				if n := len(commandsMatching(cmds, func(args []string) bool {
					return args[0] == ipt && iptLineHasTokens(strings.Join(args, " "), []string{"-t", "nat", "-S"})
				})); n != 1 {
					t.Errorf("apply %d, %s: the nat table must be listed once per apply, got %d", apply, ipt, n)
				}
			}
		}
		logged := buf.String()
		for _, want := range warnings {
			if n := logLinesWith(logged, want); n != 1 {
				t.Errorf("want the warning once over two applies, got %d: %s\n%s", n, want, logged)
			}
		}
		if n := logLinesWith(logged, "moves DNS queries to port"); n != len(warnings) {
			t.Errorf("only nat rules that move UDP 53 to another port may be warned about, got %d warnings:\n%s", n, logged)
		}
	})

	t.Run("nothing is listed when no binary is copied", func(t *testing.T) {
		for _, facts := range []dnsKernel{affectedDNSKernel, unaffectedDNSKernel} {
			buf := captureTablesLog(t)
			manager, _ := dnsCopyTestManager(t, facts, nil, func() (uint16, error) { return 537, nil })
			rec := recordFirewall(t, nil)
			serveNatListings(t, listings)
			if err := freshDNSCopyManager(manager, map[string]error{backendIPTables: nil, backendIP6Tables: nil}).Apply(); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			cmds := rec.commands()
			for _, ipt := range []string{backendIPTables, backendIP6Tables} {
				if natListed(cmds, ipt) {
					t.Errorf("kernel %s: %s listed its nat table although no DNS query is copied", facts.Release, ipt)
				}
			}
			if strings.Contains(buf.String(), "moves DNS queries to port") {
				t.Errorf("kernel %s: a held placement warned about nat:\n%s", facts.Release, buf.String())
			}
		}
	})

	t.Run("KeeneticOS lists only a nat table that exists", func(t *testing.T) {
		buf := captureTablesLog(t)
		manager, _ := dnsCopyTestManager(t, keeneticDNSKernel, nil, func() (uint16, error) { return 537, nil })
		readProcNetFile = func(path string) ([]byte, error) {
			switch path {
			case "/proc/net/ip_tables_names":
				return []byte("raw_out\nnat\nmangle\nfilter\n"), nil
			case "/proc/net/ip6_tables_names":
				return []byte("mangle\nfilter\n"), nil
			}
			return nil, os.ErrNotExist
		}
		rec := recordFirewall(t, nil)
		serveNatListings(t, listings)
		if err := freshDNSCopyManager(manager, nil).Apply(); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		cmds := rec.commands()
		if !natListed(cmds, backendIPTables) {
			t.Errorf("the IPv4 nat table exists, so b4 must look for port-changing DNS rules in it")
		}
		if natListed(cmds, backendIP6Tables) {
			t.Errorf("without an IPv6 nat table a listing only makes the KeeneticOS kernel try to load ip6table_nat")
		}
		if n := logLinesWith(buf.String(), "moves DNS queries to port"); n != 2 {
			t.Errorf("want the two IPv4 warnings, got %d:\n%s", n, buf.String())
		}
	})
}

func TestDNSNatPortParsing(t *testing.T) {
	for line, want := range map[string]string{
		"-A PREROUTING -p udp -m udp --dport 53 -j REDIRECT --to-ports 5353":                        "5353",
		"-A PREROUTING -p udp -m udp --dport 53 -j REDIRECT --to-ports 5353-5354 --random":          "5353-5354",
		"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination 10.0.0.1-10.0.0.9:5300":    "5300",
		"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination :5300":                     "5300",
		"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination [fd00::1]:5353":            "5353",
		"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination fd00::1":                   "",
		"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination [fd00::1]":                 "",
		"-A PREROUTING -p udp -m udp --dport 53 -j REDIRECT --to-ports 53":                          "",
		"-A PREROUTING -p udp -m udp --dport 53 -j DNAT --to-destination 192.168.1.1":               "",
		"-A PREROUTING -p tcp -m tcp --dport 53 -j REDIRECT --to-ports 5353":                        "",
		"-A PREROUTING -p udp -m udp --dport 5353 -j REDIRECT --to-ports 53":                        "",
		"-A PREROUTING -p udp -m udp ! --dport 53 -j REDIRECT --to-ports 5353":                      "",
		"-A PREROUTING ! -p udp -m tcp --dport 53 -j REDIRECT --to-ports 5353":                      "",
		"-A PREROUTING -p udp -m udp --dport 53 -j MASQUERADE --to-ports 5353":                      "",
		"-A PREROUTING -p udp -m udp --dport 53 -m comment --comment \"--to-ports 5353\" -j ACCEPT": "",
	} {
		port, moved := iptDNSNatPort(line)
		if moved != (want != "") || (moved && port != want) {
			t.Errorf("iptDNSNatPort(%q) = %q, %v; want %q", line, port, moved, want)
		}
	}
}
