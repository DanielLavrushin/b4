package tables

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

var (
	unaffectedDNSKernel = dnsKernel{Release: "6.8.0"}
	keeneticDNSKernel   = dnsKernel{Release: "4.9-ndm-5", KeeneticOS: true, TwinDropped: true}
	affectedDNSKernel   = dnsKernel{Release: "4.9.337", TwinDropped: true}
)

func stubDNSKernel(t *testing.T, k dnsKernel) {
	t.Helper()
	orig := dnsKernelFacts
	dnsKernelFacts = func() dnsKernel { return k }
	t.Cleanup(func() { dnsKernelFacts = orig })
}

func stubDNSCopyListener(t *testing.T, open func() (uint16, error)) {
	t.Helper()
	orig, origGroup := DNSQueryCopyOpenFunc, dnsCopyGroup.Load()
	DNSQueryCopyOpenFunc = open
	t.Cleanup(func() {
		DNSQueryCopyOpenFunc = orig
		dnsCopyGroup.Store(origGroup)
	})
}

func stubDNSCopyAlive(t *testing.T, alive func() bool) {
	t.Helper()
	orig := DNSQueryCopyAliveFunc
	DNSQueryCopyAliveFunc = alive
	t.Cleanup(func() { DNSQueryCopyAliveFunc = orig })
}

type firewallRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *firewallRecorder) commands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *firewallRecorder) reset() {
	r.mu.Lock()
	r.calls = nil
	r.mu.Unlock()
}

func recordFirewall(t *testing.T, fail func(args []string) bool) *firewallRecorder {
	t.Helper()
	rec := &firewallRecorder{}
	origRun, origSnap := run, sysctlSnapPath
	sysctlSnapPath = filepath.Join(t.TempDir(), "sysctl.json")
	t.Cleanup(func() { run, sysctlSnapPath = origRun, origSnap })
	run = func(args ...string) (string, error) {
		rec.mu.Lock()
		rec.calls = append(rec.calls, strings.Join(args, " "))
		rec.mu.Unlock()
		if slices.Contains(args, "-C") || slices.Contains(args, "-D") || (fail != nil && fail(args)) {
			return "", errors.New("exit status 1")
		}
		return "", nil
	}
	return rec
}

func rawTableCommand(args []string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-t" && args[i+1] == "raw" {
			return true
		}
	}
	return false
}

func commandsMatching(cmds []string, match func(args []string) bool) []string {
	var out []string
	for _, c := range cmds {
		if match(strings.Fields(c)) {
			out = append(out, c)
		}
	}
	return out
}

func logLinesWith(logged string, parts ...string) int {
	n := 0
	for _, line := range strings.Split(logged, "\n") {
		all := true
		for _, p := range parts {
			if !strings.Contains(line, p) {
				all = false
				break
			}
		}
		if all {
			n++
		}
	}
	return n
}

func TestKernelVersionAndHeldTwin(t *testing.T) {
	for _, tc := range []struct {
		release             string
		major, minor, patch int
		ok                  bool
	}{
		{"4.9-ndm-5", 4, 9, 0, true},
		{"4.14.173", 4, 14, 173, true},
		{"7.0.0-38-generic", 7, 0, 0, true},
		{"3.10.14", 3, 10, 14, true},
		{"4", 0, 0, 0, false},
		{"", 0, 0, 0, false},
		{"garbage", 0, 0, 0, false},
	} {
		major, minor, patch, ok := kernelVersion(tc.release)
		if major != tc.major || minor != tc.minor || patch != tc.patch || ok != tc.ok {
			t.Errorf("kernelVersion(%q) = %d, %d, %d, %v; want %d, %d, %d, %v", tc.release, major, minor, patch, ok, tc.major, tc.minor, tc.patch, tc.ok)
		}
	}

	for _, release := range []string{"4.9-ndm-5", "4.9.337", "4.4.198", "4.1.52", "3.10.14", "3.4.113", "4.14.172", "4.15.0", "4.17.19"} {
		if !kernelDropsHeldTwin(release) {
			t.Errorf("kernel %s drops the second of two queries held after conntrack, yet the gate treats it as safe", release)
		}
	}
	for _, release := range []string{"4.14.173", "4.14.336", "4.18.0", "4.19.294", "5.10.0", "7.0.0-38-generic", "", "garbage"} {
		if kernelDropsHeldTwin(release) {
			t.Errorf("kernel %q merges a held twin query, yet the gate marks it as affected", release)
		}
	}
}

func TestKeeneticOSDetection(t *testing.T) {
	origRel, origNdm, origProc := kmodRelease, ndmRunDir, readProcNetFile
	t.Cleanup(func() { kmodRelease, ndmRunDir, readProcNetFile = origRel, origNdm, origProc })

	root := t.TempDir()
	ndmDir := filepath.Join(root, "ndm")
	if err := os.Mkdir(ndmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ndmFile := filepath.Join(root, "ndm-file")
	if err := os.WriteFile(ndmFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	procNames := func(v4, v6 string) func(string) ([]byte, error) {
		return func(path string) ([]byte, error) {
			switch path {
			case "/proc/net/ip_tables_names":
				return []byte(v4), nil
			case "/proc/net/ip6_tables_names":
				return []byte(v6), nil
			}
			return nil, os.ErrNotExist
		}
	}

	for _, tc := range []struct {
		name    string
		release string
		ndm     string
		v4, v6  string
		want    bool
	}{
		{"release carries -ndm", "4.9-ndm-5", filepath.Join(root, "absent"), "filter\nmangle\n", "filter\n", true},
		{"NDMS run directory", "4.9.337", ndmDir, "filter\nmangle\n", "", true},
		{"raw_out table on IPv4", "4.9.337", filepath.Join(root, "absent"), "filter\nraw_out\nmangle\n", "", true},
		{"raw_out table on IPv6", "4.9.337", filepath.Join(root, "absent"), "filter\n", "raw_out\n", true},
		{"vanilla 4.9", "4.9.337", filepath.Join(root, "absent"), "filter\nmangle\nraw\n", "filter\nraw\n", false},
		{"a file named like the NDMS run directory", "5.15.0", ndmFile, "raw_output\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := tc.release
			kmodRelease = func() string { return release }
			ndmRunDir = tc.ndm
			readProcNetFile = procNames(tc.v4, tc.v6)
			facts := readDNSKernelFacts()
			if facts.KeeneticOS != tc.want {
				t.Errorf("KeeneticOS = %v, want %v", facts.KeeneticOS, tc.want)
			}
			if facts.Release != tc.release || facts.TwinDropped != kernelDropsHeldTwin(tc.release) {
				t.Errorf("facts = %+v, want release %q with the twin flag of that kernel", facts, tc.release)
			}
		})
	}
}

func TestCopyUnavailableFallsBackToMangle(t *testing.T) {
	bins := []string{backendIPTables, backendIP6Tables}
	groupTaken := errors.New("the NFLOG group is bound by another program")
	for _, tc := range []struct {
		name   string
		facts  dnsKernel
		rawErr error
		open   error
		marker string
		want   []string
		reject []string
	}{
		{
			name:   "KeeneticOS whose NFLOG group is taken",
			facts:  keeneticDNSKernel,
			open:   groupTaken,
			marker: "b4 cannot copy DNS queries with NFLOG (",
			want: []string{
				"b4 cannot copy DNS queries with NFLOG (b4 could not bind its NFLOG listener: the NFLOG group is bound by another program), so it holds them in its queue after conntrack, and on this kernel (4.9-ndm-5) a device that asks for a name's IPv4 and IPv6 addresses at once from one socket often loses the second answer",
				`NFLOG comes with the "Kernel modules for Netfilter" component of KeeneticOS`,
			},
			reject: []string{"kmod-ipt-raw", "opkg", "raw table"},
		},
		{
			name:   "KeeneticOS without a listener",
			facts:  keeneticDNSKernel,
			marker: "b4 cannot copy DNS queries with NFLOG (",
			want: []string{
				"b4 cannot copy DNS queries with NFLOG (b4 has no DNS query copy listener), so it holds them in its queue after conntrack, and on this kernel (4.9-ndm-5) a device that asks for a name's IPv4 and IPv6 addresses at once from one socket often loses the second answer",
			},
			reject: []string{"kmod-ipt-raw", "opkg", "raw table"},
		},
		{
			name:   "vanilla kernel without a working raw table",
			facts:  affectedDNSKernel,
			rawErr: errors.New("cannot queue packets from the raw table (exit status 1)"),
			open:   groupTaken,
			marker: "b4 cannot copy DNS queries with NFLOG either",
			want: []string{
				"cannot queue packets from the raw table (exit status 1), and b4 cannot copy DNS queries with NFLOG either (b4 could not bind its NFLOG listener: the NFLOG group is bound by another program), so it holds them in its queue after conntrack, where this kernel (4.9.337) drops the second of two queries",
				"(OpenWrt: opkg install kmod-ipt-raw) to queue them from the raw table",
				"or xt_NFLOG and nfnetlink_log (OpenWrt: opkg install kmod-ipt-nflog iptables-mod-nflog) to copy them",
			},
			reject: []string{"so DNS queries are queued in the mangle table", "Keenetic"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureTablesLog(t)
			newKmodFixture(t)
			manager := dnsQueryTestManager(t, tc.rawErr)
			stubDNSKernel(t, tc.facts)
			opened := 0
			var open func() (uint16, error)
			if tc.open != nil {
				open = func() (uint16, error) {
					opened++
					return 0, tc.open
				}
			}
			stubDNSCopyListener(t, open)
			t.Cleanup(func() { noteDNSQueryPlacement(nil) })
			rec := recordFirewall(t, nil)

			for apply := 1; apply <= 2; apply++ {
				m := NewIPTablesManager(manager.cfg, false)
				stubProbes(m, bins...)
				for _, ipt := range bins {
					if tc.rawErr != nil {
						m.rawQueueSupport[ipt] = tc.rawErr
					} else {
						delete(m.rawQueueSupport, ipt)
					}
				}
				manifest, err := m.buildManifest()
				if err != nil {
					t.Fatalf("apply %d: buildManifest: %v", apply, err)
				}
				for _, ipt := range bins {
					if got := manifest.DNSQueries[ipt]; got != dnsHeldMangle {
						t.Errorf("apply %d, %s: DNS queries placed in %s, want mangle while b4 can neither queue them early nor copy them", apply, ipt, got)
					}
					if !hasSpecWith(manifestSpecs(manifest, ipt, "mangle", "B4_PREROUTING"), "-p udp --dport 53 -j NFQUEUE") ||
						!hasSpecWith(manifestSpecs(manifest, ipt, "mangle", "OUTPUT"), "-p udp --dport 53 -j NFQUEUE") {
						t.Errorf("apply %d, %s: the fallback must queue DNS queries in mangle as before, or b4 never sees them", apply, ipt)
					}
					if manifestHasChain(manifest, ipt, "raw", iptRawChainName) || len(manifestSpecs(manifest, ipt, "raw", "PREROUTING")) > 0 {
						t.Errorf("apply %d, %s: raw table rules were emitted for a placement in mangle", apply, ipt)
					}
					if _, opened := m.dnsCopySupport[ipt]; !opened {
						t.Errorf("apply %d, %s: an affected kernel must try to copy DNS queries before it holds them after conntrack", apply, ipt)
					}
				}
			}
			if tc.facts.KeeneticOS {
				if raw := commandsMatching(rec.commands(), rawTableCommand); len(raw) > 0 {
					t.Errorf("KeeneticOS runs its raw table after conntrack, so b4 must never probe or touch it there:\n%s", strings.Join(raw, "\n"))
				}
			}
			if tc.open != nil && opened != 2*len(bins) {
				t.Errorf("a listener that failed to bind must be tried again on every apply, as the group frees up after a restart; bound %d times over two applies", opened)
			}
			if probes := commandsMatching(rec.commands(), func(args []string) bool { return slices.Contains(args, "NFLOG") }); len(probes) > 0 {
				t.Errorf("without a bound listener b4 must not probe or install NFLOG rules:\n%s", strings.Join(probes, "\n"))
			}

			logged := buf.String()
			for _, ipt := range bins {
				if n := logLinesWith(logged, "IPTABLES["+ipt+"]", tc.marker); n != 1 {
					t.Errorf("%s: the fallback warning appeared %d times over two applies, want once:\n%s", ipt, n, logged)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(logged, want) {
					t.Errorf("the warning lacks %q:\n%s", want, logged)
				}
			}
			for _, reject := range tc.reject {
				if strings.Contains(logged, reject) {
					t.Errorf("the log mentions %q, which does not apply to this router:\n%s", reject, logged)
				}
			}
		})
	}
}

func TestUnaffectedKernelRunsNoCopyCommand(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rawErr error
	}{
		{"raw table works", nil},
		{"raw table fails", errors.New("can't initialize iptables table `raw'")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := dnsQueryTestManager(t, tc.rawErr)
			stubDNSCopyListener(t, func() (uint16, error) {
				t.Errorf("an unaffected kernel opened the DNS query copy listener")
				return 537, nil
			})
			noteDNSQueryPlacement(nil)
			t.Cleanup(func() { noteDNSQueryPlacement(nil) })
			rec := recordFirewall(t, nil)

			if err := manager.Apply(); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if err := manager.Clear(); err != nil {
				t.Fatalf("Clear: %v", err)
			}
			cmds := rec.commands()
			if len(cmds) == 0 {
				t.Fatalf("no command was recorded, so the check proves nothing")
			}
			for _, c := range cmds {
				fields := strings.Fields(c)
				mangleInput := iptLineHasTokens(c, []string{"-t", "mangle"}) && slices.Contains(fields, "INPUT")
				if strings.Contains(c, "NFLOG") || strings.Contains(c, "B4_DNSCOPY") || mangleInput {
					t.Errorf("an unaffected kernel must keep today's commands, got %q", c)
				}
			}
			if len(manager.dnsCopySupport) != 0 {
				t.Errorf("an unaffected kernel tried to copy DNS queries: %v", manager.dnsCopySupport)
			}
		})
	}
}

func TestFailedRawProbeIsCachedUntilTheModuleLoads(t *testing.T) {
	fx := newKmodFixture(t)
	stubDNSKernel(t, unaffectedDNSKernel)
	forgetRawFallbackWarnings(t)
	goldenKeepSyncMap(t, &rawQueueProven)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
	rec := recordFirewall(t, func(args []string) bool {
		return rawTableCommand(args) || args[0] == "modprobe" || args[0] == "insmod"
	})

	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = true
	bins := []string{backendIPTables, backendIP6Tables}
	apply := func() Manifest {
		t.Helper()
		rec.reset()
		manager := NewIPTablesManager(&cfg, false)
		stubProbes(manager, bins...)
		for _, ipt := range bins {
			delete(manager.rawQueueSupport, ipt)
		}
		m, err := manager.buildManifest()
		if err != nil {
			t.Fatalf("buildManifest: %v", err)
		}
		return m
	}
	ranRaw := func(ipt string) bool {
		return len(commandsMatching(rec.commands(), func(args []string) bool {
			return len(args) > 0 && args[0] == ipt && rawTableCommand(args)
		})) > 0
	}
	ranModprobe := func(module string) bool {
		return slices.Contains(rec.commands(), "modprobe -q "+module)
	}

	m := apply()
	for _, ipt := range bins {
		if m.DNSQueries[ipt] != dnsHeldMangle {
			t.Fatalf("%s: a raw table that cannot queue must leave DNS queries in mangle", ipt)
		}
		if !ranRaw(ipt) || !ranModprobe(iptRawTableModule(ipt)) {
			t.Fatalf("%s: the first apply must probe the raw table and try to load %s:\n%s", ipt, iptRawTableModule(ipt), strings.Join(rec.commands(), "\n"))
		}
	}

	m = apply()
	for _, ipt := range bins {
		if m.DNSQueries[ipt] != dnsHeldMangle {
			t.Errorf("%s: the cached failure must still leave DNS queries in mangle", ipt)
		}
	}
	if cmds := rec.commands(); len(cmds) != 0 {
		t.Errorf("a raw probe that failed with the module absent must not run again on every apply and monitor restore, got:\n%s", strings.Join(cmds, "\n"))
	}

	fx.loaded("iptable_raw")
	apply()
	if !ranRaw(backendIPTables) {
		t.Errorf("once iptable_raw has loaded, the next apply must probe the raw table again")
	}
	if ranModprobe("iptable_raw") {
		t.Errorf("a loaded iptable_raw must not be loaded again")
	}
	if ranRaw(backendIP6Tables) || ranModprobe("ip6table_raw") {
		t.Errorf("ip6table_raw is still absent, so ip6tables must keep its cached failure:\n%s", strings.Join(rec.commands(), "\n"))
	}

	apply()
	if !ranRaw(backendIPTables) {
		t.Errorf("a rejection while iptable_raw is loaded, as when another program flushes the probe chain, must not be cached")
	}

	if err := os.Remove(filepath.Join(fx.sysRoot, "iptable_raw")); err != nil {
		t.Fatal(err)
	}
	apply()
	if !ranRaw(backendIPTables) || !ranModprobe("iptable_raw") {
		t.Errorf("a rejection seen while iptable_raw was loaded left a cached failure behind, so the apply after the module went away skipped the probe:\n%s", strings.Join(rec.commands(), "\n"))
	}
	apply()
	if ranRaw(backendIPTables) {
		t.Errorf("the failure with iptable_raw absent again must be cached")
	}
}

func TestPlacedFuncHearsWhichFamiliesAreCopied(t *testing.T) {
	type call struct{ v4, v6 bool }
	var got []call
	orig := DNSQueryCopyPlacedFunc
	DNSQueryCopyPlacedFunc = func(v4, v6 bool) { got = append(got, call{v4, v6}) }
	t.Cleanup(func() {
		DNSQueryCopyPlacedFunc = orig
		noteDNSQueryPlacement(nil)
	})

	for _, tc := range []struct {
		placements map[string]dnsQueryPlacement
		want       call
	}{
		{map[string]dnsQueryPlacement{backendIPTables: dnsHeldRaw, backendIP6Tables: dnsCopied}, call{false, true}},
		{map[string]dnsQueryPlacement{backendIPTablesLegacy: dnsCopied, backendIP6TablesLegacy: dnsHeldMangle}, call{true, false}},
		{map[string]dnsQueryPlacement{backendIPTables: dnsCopied, backendIP6Tables: dnsCopied}, call{true, true}},
		{map[string]dnsQueryPlacement{backendNFTables: dnsHeldRaw}, call{false, false}},
		{nil, call{false, false}},
	} {
		got = nil
		noteDNSQueryPlacement(tc.placements)
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("placements %v: the listener heard %v, want one call with %v", tc.placements, got, tc.want)
		}
		if after := dnsQueryPlacementOf(backendIPTables); after != tc.placements[backendIPTables] {
			t.Errorf("placements %v: recorded %s for iptables", tc.placements, after)
		}
	}
}

func TestDNSQueryStateFollowsTheKernel(t *testing.T) {
	newKmodFixture(t)
	forgetRawFallbackWarnings(t)
	t.Cleanup(func() { noteDNSQueryPlacement(nil) })

	noteDNSQueryPlacement(nil)
	if st := DNSQueryPlacement(); st.Applied {
		t.Fatalf("no placement was recorded, yet the state reports one: %+v", st)
	}

	for _, tc := range []struct {
		name       string
		facts      dnsKernel
		placements map[string]dnsQueryPlacement
		loaded     []string
		rejected   []string
		copyErrs   map[string]string
		want       DNSQueryState
	}{
		{
			name:       "unaffected kernel keeps today's fields",
			facts:      unaffectedDNSKernel,
			placements: map[string]dnsQueryPlacement{backendIPTables: dnsHeldRaw, backendIP6Tables: dnsHeldMangle},
			want: DNSQueryState{
				Applied: true, Release: "6.8.0",
				Placements:  map[string]string{backendIPTables: "raw", backendIP6Tables: "mangle"},
				RawMissing:  []string{"ip6table_raw"},
				RawPackages: []string{"kmod-ipt-raw6"},
			},
		},
		{
			name:       "affected vanilla kernel with IPv4 held and IPv6 copied",
			facts:      affectedDNSKernel,
			placements: map[string]dnsQueryPlacement{backendIPTables: dnsHeldMangle, backendIP6Tables: dnsCopied},
			copyErrs:   map[string]string{backendIPTables: "b4 has no DNS query copy listener"},
			want: DNSQueryState{
				Applied: true, Release: "4.9.337", Affected: true,
				Placements:   map[string]string{backendIPTables: "mangle", backendIP6Tables: "copy"},
				RawMissing:   []string{"iptable_raw"},
				RawPackages:  []string{"kmod-ipt-raw"},
				CopyMissing:  []string{"nfnetlink_log", "xt_NFLOG"},
				CopyPackages: []string{"kmod-nfnetlink-log", "kmod-ipt-nflog", "iptables-mod-nflog"},
				CopyError:    "b4 has no DNS query copy listener",
			},
		},
		{
			name:       "KeeneticOS held after conntrack",
			facts:      keeneticDNSKernel,
			placements: map[string]dnsQueryPlacement{backendIPTables: dnsHeldMangle, backendIP6Tables: dnsHeldMangle},
			copyErrs:   map[string]string{backendIPTables: "b4 has no DNS query copy listener", backendIP6Tables: "b4 has no DNS query copy listener"},
			want: DNSQueryState{
				Applied: true, Release: "4.9-ndm-5", KeeneticOS: true, Affected: true,
				Placements:  map[string]string{backendIPTables: "mangle", backendIP6Tables: "mangle"},
				CopyMissing: []string{"nfnetlink_log", "xt_NFLOG"},
				CopyError:   "b4 has no DNS query copy listener",
			},
		},
		{
			name:       "nftables on an affected kernel keeps today's fields",
			facts:      keeneticDNSKernel,
			placements: map[string]dnsQueryPlacement{backendNFTables: dnsHeldMangle},
			want: DNSQueryState{
				Applied: true, Release: "4.9-ndm-5",
				Placements: map[string]string{backendNFTables: "mangle"},
			},
		},
		{
			name:       "affected vanilla kernel whose NFLOG group another program holds",
			facts:      affectedDNSKernel,
			placements: map[string]dnsQueryPlacement{backendIPTables: dnsHeldMangle},
			loaded:     []string{dnsCopyLogModule, dnsCopyModule},
			copyErrs:   map[string]string{backendIPTables: "b4 could not bind its NFLOG listener: the NFLOG group is bound by another program"},
			want: DNSQueryState{
				Applied: true, Release: "4.9.337", Affected: true,
				Placements:  map[string]string{backendIPTables: "mangle"},
				RawMissing:  []string{"iptable_raw"},
				RawPackages: []string{"kmod-ipt-raw"},
				CopyError:   "b4 could not bind its NFLOG listener: the NFLOG group is bound by another program",
			},
		},
		{
			name:       "KeeneticOS whose NFLOG group another program holds",
			facts:      keeneticDNSKernel,
			placements: map[string]dnsQueryPlacement{backendIPTables: dnsHeldMangle},
			loaded:     []string{dnsCopyLogModule, dnsCopyModule},
			copyErrs:   map[string]string{backendIPTables: "b4 could not bind its NFLOG listener: the NFLOG group is bound by another program"},
			want: DNSQueryState{
				Applied: true, Release: "4.9-ndm-5", KeeneticOS: true, Affected: true,
				Placements: map[string]string{backendIPTables: "mangle"},
				CopyError:  "b4 could not bind its NFLOG listener: the NFLOG group is bound by another program",
			},
		},
		{
			name:       "affected vanilla kernel whose iptables rejects the NFLOG target",
			facts:      affectedDNSKernel,
			placements: map[string]dnsQueryPlacement{backendIPTables: dnsHeldMangle},
			loaded:     []string{dnsCopyLogModule, dnsCopyModule},
			rejected:   []string{dnsCopyModule},
			copyErrs:   map[string]string{backendIPTables: "iptables rejected the NFLOG target (exit status 2)"},
			want: DNSQueryState{
				Applied: true, Release: "4.9.337", Affected: true,
				Placements:   map[string]string{backendIPTables: "mangle"},
				RawMissing:   []string{"iptable_raw"},
				RawPackages:  []string{"kmod-ipt-raw"},
				CopyMissing:  []string{"xt_NFLOG"},
				CopyPackages: []string{"iptables-mod-nflog"},
				CopyError:    "iptables rejected the NFLOG target (exit status 2)",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newKmodFixture(t)
			stubDNSKernel(t, tc.facts)
			forgetRawFallbackWarnings(t)
			for _, module := range tc.loaded {
				fx.loaded(module)
			}
			for _, module := range tc.rejected {
				kmodNoteRejected(module, backendIPTables, "Couldn't load target `NFLOG'")
			}
			for bin, msg := range tc.copyErrs {
				dnsCopyErrors.Store(bin, msg)
			}
			noteDNSQueryPlacement(tc.placements)
			if got := DNSQueryPlacement(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DNSQueryPlacement() =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}
