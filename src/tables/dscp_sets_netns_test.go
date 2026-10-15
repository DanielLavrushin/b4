package tables

import (
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	b4engine "github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
	"golang.org/x/sys/unix"
)

const (
	setDSCPObserver  = "b4dscpobs"
	setDSCPAddrA     = "10.201.9.10"
	setDSCPAddrB     = "10.201.9.5"
	setDSCPAddrNest  = "10.201.9.20"
	setDSCPAddrLearn = "10.201.9.21"
	setDSCPAddrOther = "10.201.9.40"
	setDSCPDataPort  = 9999
	setDSCPWait      = 20 * time.Second

	setDSCPV6Dev      = "b4t6"
	setDSCPV6Observer = "b4dscpobs6"
	setDSCPAddrA6     = "2001:db8::10"
	setDSCPAddrB6     = "2001:db8::5"
	setDSCPAddrLearn6 = "2001:db8:5::21"
	setDSCPAddrOther6 = "2001:db8:7::1"
)

var setDSCPAllEngines = []string{backendIPTables, backendNFTables, backendIPTablesLegacy}

type setDSCPSeen map[string]map[int]int

func setDSCPEngines(t *testing.T, engines []string, test func(t *testing.T, engine string)) {
	t.Helper()
	netnsRequireNft(t)
	setDSCPLinks(t)
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			if engine == backendIPTablesLegacy {
				setDSCPLegacyLock(t)
			}
			setDSCPIsolate(t, engine)
			test(t, engine)
		})
	}
}

func setDSCPLinks(t *testing.T) {
	t.Helper()
	if _, err := net.InterfaceByName(netnsPrimary); err == nil {
		return
	}
	restore := setDSCPKeepSysctl("net/ipv6/conf/all/disable_ipv6")
	t.Cleanup(func() {
		for _, dev := range []string{netnsPrimary, netnsSecondary} {
			_, _ = run("ip", "link", "del", dev)
		}
		restore()
	})
	netnsSetupLinks(t)
}

func setDSCPClient(t *testing.T) int {
	t.Helper()
	restore := setDSCPKeepSysctl("net/ipv4/ip_forward")
	t.Cleanup(restore)
	return dscpSetupClient(t)
}

func setDSCPKeepSysctl(key string) func() {
	path := "/proc/sys/" + key
	orig, err := os.ReadFile(path)
	if err != nil {
		return func() {}
	}
	return func() { _ = os.WriteFile(path, orig, 0o644) }
}

func setDSCPLegacyLock(t *testing.T) {
	t.Helper()
	for _, bin := range []string{backendIPTablesLegacy, backendIP6TablesLegacy} {
		if !hasBinary(bin) {
			t.Skipf("%s is not installed", bin)
		}
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Skipf("cannot tell which mount namespace this is (%v), and iptables-legacy needs a private /run for its lock file", err)
	}
	if host, err := os.Readlink("/proc/1/ns/mnt"); err == nil && host == self {
		t.Skip("iptables-legacy needs its lock file on a private /run, and this is the host mount namespace; run inside one (make test-netns uses unshare -m)")
	}
	if err := unix.Mount("tmpfs", "/run", "tmpfs", 0, "mode=0755"); err != nil {
		t.Skipf("cannot put a private /run in place for the iptables-legacy lock file: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount("/run", unix.MNT_DETACH); err != nil {
			t.Errorf("could not take the private /run away again: %v", err)
		}
	})
}

func setDSCPIsolate(t *testing.T, engine string) {
	t.Helper()
	routeEngine = nil
	rulesMu.Lock()
	rulesAppliedCfg, rulesAppliedBackend = nil, ""
	rulesMu.Unlock()
	resetDSCPState(t)
	dscpIptResetRecords(t)
	dscpLearnIsolate(t)
	dscpSyncWanted.Store(nil)
	select {
	case <-dscpSyncKick:
	default:
	}
	t.Cleanup(func() {
		StopDSCPSync()
		setDSCPSweep(engine)
		routeEngine = nil
		rulesMu.Lock()
		rulesAppliedCfg, rulesAppliedBackend = nil, ""
		rulesMu.Unlock()
		dscpSyncClosed.Store(false)
		tunFirewallClosed.Store(false)
	})
}

func setDSCPBinaries(engine string) []string {
	switch engine {
	case backendNFTables:
		return nil
	case backendIPTablesLegacy:
		return []string{backendIPTablesLegacy, backendIP6TablesLegacy}
	}
	return []string{backendIPTables, backendIP6Tables}
}

func setDSCPSweep(engine string) {
	for _, bin := range setDSCPBinaries(engine) {
		iptDeleteJumpsTo(bin, "mangle", "POSTROUTING", dscpChainName)
		_, _ = run(bin, "-w", "-t", "mangle", "-F", dscpChainName)
		_, _ = run(bin, "-w", "-t", "mangle", "-X", dscpChainName)
	}
	_, _ = run("nft", "delete", "table", "inet", dscpNftTable)
	for _, name := range setDSCPIpsetNames() {
		_, _ = run("ipset", "destroy", name)
	}
}

func setDSCPIpsetNames() []string {
	out, _ := run("ipset", "list", "-n")
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); strings.HasPrefix(name, dscpIptSetPrefix) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func setDSCPAssertGone(t *testing.T, engine, why string) {
	t.Helper()
	for _, bin := range setDSCPBinaries(engine) {
		if present, known := iptChainPresence(bin, "mangle", dscpChainName); present || !known {
			t.Errorf("%s: %s still lists the mangle chain %s (present=%t, known=%t)", why, bin, dscpChainName, present, known)
		}
		if out, _ := run(bin, "-w", "-t", "mangle", "-S", "POSTROUTING"); strings.Contains(out, "-j "+dscpChainName) {
			t.Errorf("%s: mangle POSTROUTING of %s still jumps to %s:\n%s", why, bin, dscpChainName, out)
		}
	}
	if dscpNftTablePresent() {
		t.Errorf("%s: the %s table is still there", why, dscpNftTable)
	}
	if names := setDSCPIpsetNames(); len(names) > 0 {
		t.Errorf("%s: the ipsets b4 made for the sets' DSCP values are still there: %v", why, names)
	}
	if dscpApplied.Load() != nil {
		t.Errorf("%s: the DSCP rules are still recorded as applied, so the monitor would put them back", why)
	}
}

func setDSCPConfig(engine string, global bool, sets ...*config.SetConfig) *config.Config {
	cfg := dscpNetnsConfig(engine, global)
	cfg.Sets = append(cfg.Sets, sets...)
	return cfg
}

func setDSCPStart(t *testing.T, cfg *config.Config) {
	t.Helper()
	stop := netnsStartQueueListener(t, uint16(cfg.Queue.StartNum))
	t.Cleanup(stop)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	StartDSCPSync()
	t.Cleanup(StopDSCPSync)
}

func setDSCPSync(t *testing.T, cfg *config.Config) {
	t.Helper()
	want := dscpPlanFor(cfg)
	_, _, globalOn := cfg.DSCPStamp()
	SyncDSCP(cfg)
	deadline := time.Now().Add(setDSCPWait)
	for time.Now().Before(deadline) {
		st := dscpApplied.Load()
		switch {
		case st == nil:
			if want.empty() && !globalOn {
				return
			}
		case !st.pending && (st.cfg == cfg || st.plan.equal(want)):
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the DSCP sync worker did not apply the new settings within %v", setDSCPWait)
}

func setDSCPMonitor(cfg *config.Config) bool {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	return ensureDSCPLocked(cfg, false)
}

func setDSCPLearn(t *testing.T, cfg *config.Config, setID string, addrs ...string) {
	t.Helper()
	set := cfg.GetSetById(setID)
	if set == nil {
		t.Fatalf("the config has no set %s", setID)
	}
	dscpLearnWaitAll(t, DSCPLearn(cfg, set, dscpLearnIPs(addrs...), false))
}

func setDSCPAssertPerSet(t *testing.T, engine string) *dscpState {
	t.Helper()
	st := dscpApplied.Load()
	if st == nil || st.pending {
		t.Fatalf("the per-set DSCP rules are not in place (state %+v)", st)
	}
	if engine == backendNFTables {
		if !st.nft.perSet() {
			t.Fatalf("the %s table holds no per-set DSCP objects", dscpNftTable)
		}
		return st
	}
	for _, bin := range setDSCPBinaries(engine) {
		if st.ipt == nil || !dscpIptShapeOf(st.ipt.chains[bin]).guard {
			t.Fatalf("%s carries no per-set DSCP rules", bin)
		}
	}
	return st
}

func setDSCPObserve(t *testing.T) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\nadd table inet %s\n", setDSCPObserver, setDSCPObserver, setDSCPObserver)
	fmt.Fprintf(&b, "add chain inet %s in { type filter hook prerouting priority -300 ; policy accept ; }\n", setDSCPObserver)
	for _, link := range []string{netnsPrimary, netnsSecondary} {
		fmt.Fprintf(&b, "add set inet %s %s { type ipv4_addr . dscp ; flags dynamic ; size 65535 ; counter ; }\n", setDSCPObserver, link)
		fmt.Fprintf(&b, "add rule inet %s in iifname \"%sp\" meta nfproto ipv4 update @%s { ip daddr . ip dscp }\n", setDSCPObserver, link, link)
		fmt.Fprintf(&b, "add rule inet %s in iifname \"%sp\" drop\n", setDSCPObserver, link)
	}
	dscpNftIn(t, 0, b.String())
	t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", setDSCPObserver) })
}

func setDSCPClientObserve(t *testing.T, pid int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\n", setDSCPObserver)
	fmt.Fprintf(&b, "add chain inet %s in { type filter hook prerouting priority -300 ; policy accept ; }\n", setDSCPObserver)
	for _, kind := range []string{"echo-request", "echo-reply"} {
		name := strings.ReplaceAll(kind, "-", "_")
		fmt.Fprintf(&b, "add set inet %s %s { type ipv4_addr . dscp ; flags dynamic ; size 1024 ; counter ; }\n", setDSCPObserver, name)
		fmt.Fprintf(&b, "add rule inet %s in iifname \"%s\" icmp type %s update @%s { ip daddr . ip dscp }\n", setDSCPObserver, dscpClientIf, kind, name)
	}
	dscpNftIn(t, pid, b.String())
}

func setDSCPParse(t *testing.T, listing string) setDSCPSeen {
	t.Helper()
	seen := setDSCPSeen{}
	f := strings.Fields(strings.NewReplacer(",", " ", "{", " ", "}", " ").Replace(listing))
	for i := 0; i+5 < len(f); i++ {
		if f[i+1] != "." || f[i+3] != "counter" || f[i+4] != "packets" {
			continue
		}
		value, verr := strconv.ParseInt(f[i+2], 0, 0)
		n, nerr := strconv.Atoi(f[i+5])
		if verr != nil || nerr != nil || net.ParseIP(f[i]) == nil {
			t.Fatalf("unreadable element %q in:\n%s", strings.Join(f[i:i+6], " "), listing)
		}
		if seen[f[i]] == nil {
			seen[f[i]] = map[int]int{}
		}
		seen[f[i]][int(value)] += n
	}
	return seen
}

func setDSCPWire(t *testing.T, link string) setDSCPSeen {
	t.Helper()
	return setDSCPParse(t, netnsRun(t, "nft", "-n", "list", "set", "inet", setDSCPObserver, link))
}

func setDSCPClientWire(t *testing.T, pid int, kind string) setDSCPSeen {
	t.Helper()
	out, err := exec.Command("nsenter", "-t", strconv.Itoa(pid), "-n", "nft", "-n", "list", "set", "inet", setDSCPObserver, kind).CombinedOutput()
	if err != nil {
		t.Fatalf("read %s in netns %d: %v: %s", kind, pid, err, out)
	}
	return setDSCPParse(t, string(out))
}

func setDSCPFlush(t *testing.T) {
	t.Helper()
	for _, link := range []string{netnsPrimary, netnsSecondary} {
		netnsRun(t, "nft", "flush", "set", "inet", setDSCPObserver, link)
	}
}

func setDSCPExpectOnly(t *testing.T, what string, seen setDSCPSeen, allowed map[string][]int, least int) {
	t.Helper()
	for _, addr := range slices.Sorted(maps.Keys(allowed)) {
		total := 0
		for _, value := range slices.Sorted(maps.Keys(seen[addr])) {
			n := seen[addr][value]
			total += n
			if !slices.Contains(allowed[addr], value) {
				t.Errorf("%s: %d packet(s) to %s left with DSCP %d, want %v", what, n, addr, value, allowed[addr])
			}
		}
		if total < least {
			t.Errorf("%s: %d packet(s) to %s reached the far end of the link, want at least %d, so the test proves nothing", what, total, addr, least)
		}
	}
}

func setDSCPExpect(t *testing.T, what string, seen setDSCPSeen, want map[string]int) {
	t.Helper()
	allowed := make(map[string][]int, len(want))
	for addr, value := range want {
		allowed[addr] = []int{value}
	}
	setDSCPExpectOnly(t, what, seen, allowed, 1)
}

func setDSCPExpectNone(t *testing.T, what string, seen setDSCPSeen, addrs ...string) {
	t.Helper()
	for _, addr := range addrs {
		if len(seen[addr]) > 0 {
			t.Errorf("%s: packets to %s left by this link (%v), they must not", what, addr, seen[addr])
		}
	}
}

func setDSCPCheck(t *testing.T, what string, want map[string]int, send func()) {
	t.Helper()
	setDSCPFlush(t)
	send()
	time.Sleep(150 * time.Millisecond)
	setDSCPExpect(t, what, setDSCPWire(t, netnsPrimary), want)
}

func setDSCPAddrs(want map[string]int) []string {
	return slices.Sorted(maps.Keys(want))
}

func setDSCPSockaddr(addr string, port int) *unix.SockaddrInet4 {
	sa := &unix.SockaddrInet4{Port: port}
	copy(sa.Addr[:], net.ParseIP(addr).To4())
	return sa
}

func setDSCPUDP(sport, tos, port int, addrs []string) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TOS, tos); err != nil {
		return fmt.Errorf("set IP_TOS: %w", err)
	}
	if sport != 0 {
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			return fmt.Errorf("set SO_REUSEADDR: %w", err)
		}
		if err := unix.Bind(fd, &unix.SockaddrInet4{Port: sport}); err != nil {
			return fmt.Errorf("bind port %d: %w", sport, err)
		}
	}
	for _, addr := range addrs {
		if err := unix.Sendto(fd, []byte("dscp"), 0, setDSCPSockaddr(addr, port)); err != nil {
			return fmt.Errorf("send to %s: %w", addr, err)
		}
	}
	return nil
}

func setDSCPSendUDP(t *testing.T, tos, port int, addrs ...string) {
	t.Helper()
	if err := setDSCPUDP(0, tos, port, addrs); err != nil {
		t.Fatalf("local UDP: %v", err)
	}
}

func setDSCPInNetns(t *testing.T, pid int, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		home, err := os.Open("/proc/thread-self/ns/net")
		if err != nil {
			done <- err
			return
		}
		defer home.Close()
		f, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
		if err != nil {
			done <- err
			return
		}
		defer f.Close()
		if err := unix.Setns(int(f.Fd()), unix.CLONE_NEWNET); err != nil {
			done <- err
			return
		}
		defer func() {
			if unix.Setns(int(home.Fd()), unix.CLONE_NEWNET) == nil {
				runtime.UnlockOSThread()
			}
		}()
		done <- fn()
	}()
	if err := <-done; err != nil {
		t.Fatalf("in netns %d: %v", pid, err)
	}
}

func setDSCPClientUDP(t *testing.T, pid, sport, tos, port int, addrs ...string) {
	t.Helper()
	setDSCPInNetns(t, pid, func() error { return setDSCPUDP(sport, tos, port, addrs) })
}

func setDSCPClientDial(t *testing.T, pid int, sport uint16, addr string) {
	t.Helper()
	setDSCPInNetns(t, pid, func() error {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			return fmt.Errorf("set SO_REUSEADDR: %w", err)
		}
		if err := unix.Bind(fd, setDSCPSockaddr(dscpClientIP, int(sport))); err != nil {
			return fmt.Errorf("bind port %d: %w", sport, err)
		}
		if err := unix.Connect(fd, setDSCPSockaddr(addr, 443)); err != nil && !errors.Is(err, unix.EINPROGRESS) {
			return fmt.Errorf("connect to %s: %w", addr, err)
		}
		time.Sleep(300 * time.Millisecond)
		return nil
	})
}

func setDSCPSendRaw(t *testing.T, mark uint32, pkt []byte) {
	t.Helper()
	s, err := sock.NewSenderWithMark(int(mark))
	if err != nil {
		t.Fatalf("raw socket with mark 0x%x: %v", mark, err)
	}
	defer s.Close()
	dst := net.IP(pkt[16:20])
	if err := s.SendIPv4(pkt, dst); err != nil {
		t.Fatalf("send with mark 0x%x to %s: %v", mark, dst, err)
	}
}

func setDSCPSendMarked(t *testing.T, mark uint32, sport uint16, addrs ...string) {
	t.Helper()
	for _, addr := range addrs {
		setDSCPSendRaw(t, mark, netnsTCPPacket(net.ParseIP(netnsPrimaryIP), net.ParseIP(addr), sport, 443))
	}
}

func setDSCPFakeACK(src, dst net.IP, sport, dport uint16) []byte {
	pkt := netnsTCPPacket(src, dst, sport, dport)
	binary.BigEndian.PutUint32(pkt[24:], 0x7f000000)
	binary.BigEndian.PutUint32(pkt[28:], 1)
	pkt[33] = 0x10
	sock.FixTCPChecksum(pkt)
	return pkt
}

func setDSCPRuleCount(t *testing.T, bin, table, chain, needle string) int {
	t.Helper()
	out := netnsRun(t, bin, "-w", "-t", table, "-L", chain, "-v", "-x", "-n")
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		f := strings.Fields(line)
		n, err := strconv.Atoi(f[0])
		if err != nil {
			t.Fatalf("unparsable counter in %q", line)
		}
		return n
	}
	t.Fatalf("no rule matching %q in %s %s:\n%s", needle, table, chain, out)
	return 0
}

func setDSCPNotrack(t *testing.T, engine string) func() int {
	t.Helper()
	bin := backendIPTables
	if engine == backendIPTablesLegacy {
		bin = backendIPTablesLegacy
	}
	spec := []string{"-m", "mark", "--mark", fmt.Sprintf("0x%x/0x%x", b4engine.ReinjectMarkBit, b4engine.ReinjectMarkBit), "-j", "CT", "--notrack"}
	netnsRun(t, append([]string{bin, "-w", "-t", "raw", "-A", "OUTPUT"}, spec...)...)
	t.Cleanup(func() { _, _ = run(append([]string{bin, "-w", "-t", "raw", "-D", "OUTPUT"}, spec...)...) })
	return func() int { return setDSCPRuleCount(t, bin, "raw", "OUTPUT", "notrack") }
}

func setDSCPConntrackProbe(t *testing.T, mark uint32) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\nadd table inet %s\n", dscpTestCounters, dscpTestCounters, dscpTestCounters)
	fmt.Fprintf(&b, "add chain inet %s post { type filter hook postrouting priority 140 ; policy accept ; }\n", dscpTestCounters)
	for _, state := range []string{"invalid", "untracked"} {
		fmt.Fprintf(&b, "add counter inet %s %s\n", dscpTestCounters, state)
		fmt.Fprintf(&b, "add rule inet %s post ip daddr 10.201.9.0/24 meta mark & 0x%x == 0x%x ct state %s counter name \"%s\"\n", dscpTestCounters, mark, mark, state, state)
	}
	dscpNftIn(t, 0, b.String())
	t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", dscpTestCounters) })
}

func setDSCPSysctl(t *testing.T, key string) func(value string) {
	t.Helper()
	path := "/proc/sys/" + key
	if _, err := os.Stat(path); err != nil {
		t.Skipf("cannot read %s: %v", path, err)
	}
	t.Cleanup(setDSCPKeepSysctl(key))
	return func(value string) {
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatalf("set %s to %s: %v", key, value, err)
		}
	}
}

func setDSCPRouteVia(t *testing.T, dev, gw string, addrs ...string) {
	t.Helper()
	for _, addr := range addrs {
		netnsRun(t, "ip", "route", "replace", addr+"/32", "via", gw, "dev", dev)
		t.Cleanup(func() { _, _ = run("ip", "route", "del", addr+"/32", "via", gw, "dev", dev) })
	}
}

func setDSCPNeighbor(t *testing.T, dev, addr string) {
	t.Helper()
	netnsRun(t, "ip", "neigh", "replace", addr, "lladdr", netnsLinkMAC(t, dev+"p"), "dev", dev, "nud", "permanent")
	t.Cleanup(func() { _, _ = run("ip", "neigh", "del", addr, "dev", dev) })
}

func setDSCPListing(t *testing.T, engine string) string {
	t.Helper()
	if engine == backendNFTables {
		return netnsRun(t, "nft", "-a", "list", "chain", "inet", dscpNftTable, dscpNftChain)
	}
	var b strings.Builder
	for _, bin := range setDSCPBinaries(engine) {
		b.WriteString(netnsRun(t, bin, "-w", "-t", "mangle", "-S", dscpChainName))
		b.WriteString(netnsRun(t, bin, "-w", "-t", "mangle", "-S", "POSTROUTING"))
	}
	return b.String()
}

func setDSCPAssertShape(t *testing.T, why, engine string, st *dscpState) {
	t.Helper()
	if engine == backendNFTables {
		if !dscpNftLayoutIntact(st.nft) {
			t.Errorf("%s: the shape check rejects the live %s chain (stamps %d, vmaps %d):\n%s", why, dscpNftTable, st.nft.stamps, st.nft.vmaps, setDSCPListing(t, engine))
		}
		return
	}
	for _, bin := range setDSCPBinaries(engine) {
		specs := st.ipt.chains[bin]
		listing := netnsRun(t, bin, "-w", "-t", "mangle", "-S", dscpChainName)
		if got, ok := dscpIptListedShape(listing); !ok || got != dscpIptShapeOf(specs) {
			t.Errorf("%s: %s lists a chain whose shape %+v (readable=%t) is not the installed %+v:\n%s", why, bin, got, ok, dscpIptShapeOf(specs), listing)
		}
		if !dscpIptPlanIntact(bin, specs) {
			t.Errorf("%s: the shape check rejects the live chain of %s:\n%s", why, bin, listing)
		}
	}
}

type setDSCPRecorder struct {
	mu   sync.Mutex
	cmds []string
}

func setDSCPRecord(t *testing.T) *setDSCPRecorder {
	t.Helper()
	r := &setDSCPRecorder{}
	origRun, origStdin, origNft := run, runStdin, runNftStdin
	run = func(args ...string) (string, error) {
		r.note(strings.Join(args, " "))
		return origRun(args...)
	}
	runStdin = func(stdin string, args ...string) error {
		r.note(strings.Join(args, " ") + "\n" + stdin)
		return origStdin(stdin, args...)
	}
	runNftStdin = func(script string) (string, error) {
		r.note("nft -f -\n" + script)
		return origNft(script)
	}
	t.Cleanup(func() { run, runStdin, runNftStdin = origRun, origStdin, origNft })
	return r
}

func (r *setDSCPRecorder) note(cmd string) {
	r.mu.Lock()
	r.cmds = append(r.cmds, cmd)
	r.mu.Unlock()
}

func (r *setDSCPRecorder) mark() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cmds)
}

func (r *setDSCPRecorder) since(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.cmds[n:])
}

func setDSCPMutations(cmds []string) []string {
	var out []string
	for _, cmd := range cmds {
		f := strings.Fields(cmd)
		if len(f) < 2 {
			continue
		}
		mutates := false
		switch {
		case f[0] == "ipset":
			mutates = !slices.Contains([]string{"list", "test", "save", "version", "--version", "-v"}, f[1])
		case f[0] == "nft":
			mutates = slices.ContainsFunc(f[1:], func(word string) bool {
				return slices.Contains([]string{"-f", "add", "delete", "flush", "insert", "replace", "create", "destroy", "reset"}, word)
			})
		case isIPTablesBinary(f[0]):
			mutates = slices.ContainsFunc(f[1:], func(word string) bool {
				return slices.Contains([]string{"-A", "-I", "-D", "-F", "-N", "-X", "-R", "-Z", "-E", "-P"}, word)
			})
		}
		if mutates {
			out = append(out, cmd)
		}
	}
	return out
}

func setDSCPAssertQuiet(t *testing.T, rec *setDSCPRecorder, cfg *config.Config, why string) {
	t.Helper()
	mark := 0
	if rec != nil {
		mark = rec.mark()
	}
	if setDSCPMonitor(cfg) {
		t.Errorf("%s: the monitor found the DSCP rules broken and rebuilt them", why)
	}
	if rec == nil {
		return
	}
	if changed := setDSCPMutations(rec.since(mark)); len(changed) > 0 {
		t.Errorf("%s: the monitor check changed the firewall:\n%s", why, strings.Join(changed, "\n"))
	}
}

type setDSCPFlood struct {
	stop chan struct{}
	done chan error
	once sync.Once
	err  error
	sent atomic.Int64
}

func setDSCPStartFlood(t *testing.T, addrs ...string) *setDSCPFlood {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("flood socket: %v", err)
	}
	targets := make([]*unix.SockaddrInet4, len(addrs))
	for i, addr := range addrs {
		targets[i] = setDSCPSockaddr(addr, setDSCPDataPort)
	}
	f := &setDSCPFlood{stop: make(chan struct{}), done: make(chan error, 1)}
	go func() {
		defer unix.Close(fd)
		payload := []byte("dscp")
		for {
			select {
			case <-f.stop:
				f.done <- nil
				return
			default:
			}
			for _, sa := range targets {
				if err := unix.Sendto(fd, payload, 0, sa); err != nil && !errors.Is(err, unix.ENOBUFS) && !errors.Is(err, unix.EAGAIN) {
					f.done <- err
					return
				}
			}
			f.sent.Add(1)
		}
	}()
	t.Cleanup(func() { _ = f.halt() })
	return f
}

func (f *setDSCPFlood) halt() error {
	f.once.Do(func() {
		close(f.stop)
		f.err = <-f.done
	})
	return f.err
}

func TestNetnsSetDSCPPerDestination(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		client := setDSCPClient(t)
		setDSCPObserve(t)
		setDSCPClientObserve(t, client)

		cfg := setDSCPConfig(engine, true,
			dscpPlanTestSet("dscp-c", 25, "0.0.0.0/0"),
			dscpPlanTestSet("dscp-a", 31, setDSCPAddrA, "10.201.9.0/27"),
			dscpPlanTestSet("dscp-b", 6, "10.201.9.0/28"),
		)
		setDSCPStart(t, cfg)
		setDSCPAssertPerSet(t, engine)
		setDSCPLearn(t, cfg, "dscp-b", setDSCPAddrLearn)

		want := map[string]int{
			setDSCPAddrA:     31,
			setDSCPAddrB:     6,
			setDSCPAddrNest:  31,
			setDSCPAddrLearn: 6,
			setDSCPAddrOther: 25,
			netnsTarget:      25,
		}
		addrs := setDSCPAddrs(want)
		q := uint32(cfg.Queue.Mark)

		setDSCPCheck(t, "a forwarded TCP SYN", want, func() {
			for _, addr := range addrs {
				dscpDialFromNetns(t, client, net.JoinHostPort(addr, "443"))
			}
		})
		setDSCPCheck(t, "forwarded UDP", want, func() { setDSCPClientUDP(t, client, 0, 0, setDSCPDataPort, addrs...) })
		setDSCPCheck(t, "local UDP to a queued port", want, func() { setDSCPSendUDP(t, 0, 443, addrs...) })
		setDSCPCheck(t, "a raw packet injected with the queue mark", want, func() { setDSCPSendMarked(t, q, 40443, addrs...) })
		setDSCPCheck(t, "a raw packet b4 sends toward a client (client mark)", map[string]int{setDSCPAddrA: 0, setDSCPAddrOther: 0}, func() {
			setDSCPSendMarked(t, q|b4engine.ClientMark, 40444, setDSCPAddrA, setDSCPAddrOther)
		})

		loopNsExec(t, client, "ping -c 1 -W 1 "+dscpClientGW+" >/dev/null")
		loopRun(t, "ping", "-c", "1", "-W", "1", dscpClientIP)
		time.Sleep(150 * time.Millisecond)
		setDSCPExpect(t, "this host's echo reply to a client that pinged it", setDSCPClientWire(t, client, "echo_reply"), map[string]int{dscpClientIP: 0})
		setDSCPExpect(t, "an echo request this host starts towards the client, an address of the 0.0.0.0/0 set", setDSCPClientWire(t, client, "echo_request"), map[string]int{dscpClientIP: 25})
	})
}

func setDSCPV6Link(t *testing.T) {
	t.Helper()
	if _, err := net.InterfaceByName(setDSCPV6Dev); err == nil {
		return
	}
	for _, key := range []string{"net/ipv6/conf/all/disable_ipv6", "net/ipv6/conf/default/disable_ipv6"} {
		t.Cleanup(setDSCPKeepSysctl(key))
		if err := os.WriteFile("/proc/sys/"+key, []byte("0"), 0o644); err != nil {
			t.Fatalf("turn IPv6 on (%s): %v", key, err)
		}
	}
	t.Cleanup(func() { _, _ = run("ip", "link", "del", setDSCPV6Dev) })
	netnsRun(t, "ip", "link", "add", setDSCPV6Dev, "type", "veth", "peer", "name", setDSCPV6Dev+"p")
	netnsRun(t, "ip", "link", "set", setDSCPV6Dev, "up")
	netnsRun(t, "ip", "link", "set", setDSCPV6Dev+"p", "up")
	netnsRun(t, "ip", "-6", "addr", "add", "2001:db8:ff::1/64", "dev", setDSCPV6Dev, "nodad")
	netnsRun(t, "ip", "-6", "neigh", "replace", "2001:db8:ff::2", "lladdr", netnsLinkMAC(t, setDSCPV6Dev+"p"), "dev", setDSCPV6Dev, "nud", "permanent")
	netnsRun(t, "ip", "-6", "route", "add", "2001:db8::/32", "via", "2001:db8:ff::2", "dev", setDSCPV6Dev)
}

func setDSCPV6Observe(t *testing.T) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\nadd table inet %s\n", setDSCPV6Observer, setDSCPV6Observer, setDSCPV6Observer)
	fmt.Fprintf(&b, "add chain inet %s in { type filter hook prerouting priority -300 ; policy accept ; }\n", setDSCPV6Observer)
	fmt.Fprintf(&b, "add set inet %s seen { type ipv6_addr . dscp ; flags dynamic ; size 65535 ; counter ; }\n", setDSCPV6Observer)
	fmt.Fprintf(&b, "add rule inet %s in iifname \"%sp\" meta nfproto ipv6 update @seen { ip6 daddr . ip6 dscp }\n", setDSCPV6Observer, setDSCPV6Dev)
	fmt.Fprintf(&b, "add rule inet %s in iifname \"%sp\" drop\n", setDSCPV6Observer, setDSCPV6Dev)
	dscpNftIn(t, 0, b.String())
	t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", setDSCPV6Observer) })
}

func setDSCPV6Send(t *testing.T, addrs ...string) setDSCPSeen {
	t.Helper()
	netnsRun(t, "nft", "flush", "set", "inet", setDSCPV6Observer, "seen")
	for _, addr := range addrs {
		conn, err := net.DialUDP("udp6", nil, &net.UDPAddr{IP: net.ParseIP(addr), Port: setDSCPDataPort})
		if err != nil {
			t.Fatalf("dial %s: %v", addr, err)
		}
		_, err = conn.Write([]byte("dscp"))
		_ = conn.Close()
		if err != nil {
			t.Fatalf("send to %s: %v", addr, err)
		}
	}
	time.Sleep(150 * time.Millisecond)
	return setDSCPParse(t, netnsRun(t, "nft", "-n", "list", "set", "inet", setDSCPV6Observer, "seen"))
}

func TestNetnsSetDSCPIPv6(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		setDSCPV6Link(t)
		setDSCPV6Observe(t)
		cfg := setDSCPConfig(engine, true,
			dscpPlanTestSet("dscp-a6", 31, setDSCPAddrA6),
			dscpPlanTestSet("dscp-b6", 6, "2001:db8::/64"),
		)
		setDSCPStart(t, cfg)
		setDSCPAssertPerSet(t, engine)
		setDSCPLearn(t, cfg, "dscp-b6", setDSCPAddrLearn6)

		want := map[string]int{
			setDSCPAddrA6:     31,
			setDSCPAddrB6:     6,
			setDSCPAddrLearn6: 6,
			setDSCPAddrOther6: dscpTestValue,
		}
		setDSCPExpect(t, "local IPv6 UDP", setDSCPV6Send(t, setDSCPAddrs(want)...), want)
	})
}

func TestNetnsSetDSCPOnlyPerSet(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		client := setDSCPClient(t)
		setDSCPObserve(t)
		rec := setDSCPRecord(t)

		cfg := setDSCPConfig(engine, false,
			dscpPlanTestSet("dscp-a", 31, setDSCPAddrA),
			dscpPlanTestSet("dscp-b", 6, "10.201.9.0/28"),
		)
		setDSCPStart(t, cfg)
		setDSCPAssertPerSet(t, engine)
		setDSCPLearn(t, cfg, "dscp-b", setDSCPAddrLearn)

		if engine == backendNFTables {
			if out := setDSCPListing(t, engine); strings.Contains(out, "meta nfproto") {
				t.Errorf("with Set DSCP off the chain must hold no rule for every packet:\n%s", out)
			}
		} else {
			for _, bin := range setDSCPBinaries(engine) {
				listing := netnsRun(t, bin, "-w", "-t", "mangle", "-S", dscpChainName)
				if shape, ok := dscpIptListedShape(listing); !ok || shape.globals != 0 || !shape.guard {
					t.Errorf("with Set DSCP off %s must hold the three exemptions, the guard and the set rules only (shape %+v, readable=%t):\n%s", bin, shape, ok, listing)
				}
			}
		}

		setDSCPCheck(t, "forwarded UDP a client marked with DSCP 46", map[string]int{
			setDSCPAddrA: 31, setDSCPAddrB: 6, setDSCPAddrLearn: 6, setDSCPAddrOther: 46,
		}, func() {
			setDSCPClientUDP(t, client, 0, 46<<2, setDSCPDataPort, setDSCPAddrA, setDSCPAddrB, setDSCPAddrLearn, setDSCPAddrOther)
		})
		setDSCPCheck(t, "local UDP sent with DSCP 10", map[string]int{setDSCPAddrA: 31, setDSCPAddrOther: 10}, func() {
			setDSCPSendUDP(t, 10<<2, setDSCPDataPort, setDSCPAddrA, setDSCPAddrOther)
		})
		setDSCPCheck(t, "a forwarded TCP SYN", map[string]int{setDSCPAddrA: 31, setDSCPAddrOther: 0}, func() {
			dscpDialFromNetns(t, client, net.JoinHostPort(setDSCPAddrA, "443"))
			dscpDialFromNetns(t, client, net.JoinHostPort(setDSCPAddrOther, "443"))
		})
		setDSCPCheck(t, "a raw packet injected with the queue mark", map[string]int{setDSCPAddrA: 31, setDSCPAddrB: 6, setDSCPAddrOther: 0}, func() {
			setDSCPSendMarked(t, uint32(cfg.Queue.Mark), 40445, setDSCPAddrA, setDSCPAddrB, setDSCPAddrOther)
		})
		setDSCPAssertQuiet(t, rec, cfg, "with Set DSCP off")
	})
}

func TestNetnsSetDSCPReplaceNoGap(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		for _, global := range []bool{true, false} {
			name := "set DSCP off"
			if global {
				name = "set DSCP on"
			}
			t.Run(name, func(t *testing.T) { setDSCPReplaceNoGap(t, engine, global) })
		}
	})
}

func setDSCPReplaceNoGap(t *testing.T, engine string, global bool) {
	const (
		addrX    = "10.201.9.70"
		addrTie  = "10.201.9.128"
		floorPkt = 1000
	)
	type step struct {
		name     string
		a        int
		x        bool
		reversed bool
		ifaces   []string
	}
	steps := []step{
		{name: "start", a: 31},
		{name: "a set's value changed", a: 26},
		{name: "a set was added", a: 26, x: true},
		{name: "two sets holding one address swapped places", a: 26, x: true, reversed: true},
		{name: "the interface list narrowed to b4t0", a: 26, x: true, reversed: true, ifaces: []string{netnsPrimary}},
		{name: "a set was removed", a: 26, reversed: true, ifaces: []string{netnsPrimary}},
		{name: "the interface list widened to two interfaces", a: 26, reversed: true, ifaces: []string{netnsPrimary, netnsSecondary}},
		{name: "back to the start", a: 31},
	}
	build := func(s step) *config.Config {
		p := dscpPlanTestSet("dscp-p", 46, addrTie)
		q := dscpPlanTestSet("dscp-q", 10, addrTie)
		sets := []*config.SetConfig{dscpPlanTestSet("dscp-a", s.a, setDSCPAddrA), dscpPlanTestSet("dscp-b", 6, "10.201.9.0/28"), p, q}
		if s.reversed {
			sets[2], sets[3] = q, p
		}
		if s.x {
			sets = append(sets, dscpPlanTestSet("dscp-x", 25, "10.201.9.64/26"))
		}
		cfg := setDSCPConfig(engine, global, sets...)
		cfg.System.Tables.DSCP.Interfaces = s.ifaces
		return cfg
	}

	setDSCPIsolate(t, engine)
	setDSCPObserve(t)
	rec := setDSCPRecord(t)
	first := build(steps[0])
	setDSCPStart(t, first)
	setDSCPAssertPerSet(t, engine)
	setDSCPLearn(t, first, "dscp-b", setDSCPAddrLearn)

	unset := 0
	if global {
		unset = dscpTestValue
	}
	setDSCPCheck(t, "before any change", map[string]int{
		setDSCPAddrA: 31, setDSCPAddrB: 6, setDSCPAddrLearn: 6, addrTie: 46, addrX: unset, setDSCPAddrOther: unset,
	}, func() {
		setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrA, setDSCPAddrB, setDSCPAddrLearn, addrTie, addrX, setDSCPAddrOther)
	})

	setDSCPFlush(t)
	flood := setDSCPStartFlood(t, setDSCPAddrA, setDSCPAddrB, setDSCPAddrLearn, addrTie, addrX, setDSCPAddrOther)
	mark := rec.mark()
	var last *config.Config
	for _, s := range steps[1:] {
		last = build(s)
		setDSCPSync(t, last)
		setDSCPAssertPerSet(t, engine)
		time.Sleep(100 * time.Millisecond)
	}
	if err := flood.halt(); err != nil {
		t.Fatalf("the sender stopped early: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	t.Logf("%d rounds of UDP sent to 6 addresses across %d changes", flood.sent.Load(), len(steps)-1)

	during := map[string][]int{
		setDSCPAddrA:     {31, 26},
		setDSCPAddrB:     {6},
		setDSCPAddrLearn: {6},
		addrTie:          {46, 10},
		addrX:            {unset, 25},
		setDSCPAddrOther: {unset},
	}
	seen := setDSCPWire(t, netnsPrimary)
	setDSCPExpectOnly(t, "while the sets changed", seen, during, floorPkt)
	for _, addr := range slices.Sorted(maps.Keys(during)) {
		for _, value := range during[addr] {
			if seen[addr][value] == 0 {
				t.Errorf("while the sets changed no packet to %s carried DSCP %d, so the sender never saw that change: %v", addr, value, seen[addr])
			}
		}
	}
	rebuilt := slices.DeleteFunc(rec.since(mark), func(cmd string) bool {
		return !strings.Contains(cmd, " -F "+dscpChainName) && !strings.Contains(cmd, " -X "+dscpChainName) && !strings.Contains(cmd, "delete table inet "+dscpNftTable)
	})
	if len(rebuilt) > 0 {
		t.Errorf("%d commands rebuilt the DSCP rules from scratch instead of replacing them in place, which leaves a moment without them; the first:\n%s", len(rebuilt), rebuilt[0])
	}

	setDSCPCheck(t, "after the last change", map[string]int{
		setDSCPAddrA: 31, setDSCPAddrB: 6, setDSCPAddrLearn: 6, addrTie: 46, addrX: unset, setDSCPAddrOther: unset,
	}, func() {
		setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrA, setDSCPAddrB, setDSCPAddrLearn, addrTie, addrX, setDSCPAddrOther)
	})
	setDSCPAssertQuiet(t, rec, last, "after the last change")
}

func TestNetnsSetDSCPInterfaceScope(t *testing.T) {
	const (
		routedSet   = "10.201.9.200"
		routedOther = "10.201.9.201"
	)
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		setDSCPRouteVia(t, netnsSecondary, netnsSecondGW, routedSet, routedOther)
		setDSCPObserve(t)
		rec := setDSCPRecord(t)
		build := func(global bool, ifaces ...string) *config.Config {
			cfg := setDSCPConfig(engine, global, dscpPlanTestSet("dscp-a", 31, setDSCPAddrA, routedSet))
			cfg.System.Tables.DSCP.Interfaces = ifaces
			return cfg
		}

		phases := []struct {
			name    string
			cfg     *config.Config
			refresh bool
			primary map[string]int
			second  map[string]int
		}{
			{"Set DSCP on, interface list b4t1", build(true, netnsSecondary), false,
				map[string]int{setDSCPAddrA: 0}, map[string]int{routedSet: 31, routedOther: dscpTestValue}},
			{"Set DSCP off, interface list b4t1", build(false, netnsSecondary), true,
				map[string]int{setDSCPAddrA: 0}, map[string]int{routedSet: 31, routedOther: 0}},
			{"Set DSCP off, interface list b4t0", build(false, netnsPrimary), false,
				map[string]int{setDSCPAddrA: 31}, map[string]int{routedSet: 0, routedOther: 0}},
			{"Set DSCP on, every interface", build(true), true,
				map[string]int{setDSCPAddrA: 31}, map[string]int{routedSet: 31, routedOther: dscpTestValue}},
		}
		for i, p := range phases {
			if i == 0 {
				setDSCPStart(t, p.cfg)
			} else {
				if p.refresh {
					if err := RefreshRules(p.cfg); err != nil {
						t.Fatalf("%s: RefreshRules: %v", p.name, err)
					}
				}
				setDSCPSync(t, p.cfg)
			}
			setDSCPAssertPerSet(t, engine)
			setDSCPFlush(t)
			setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrA, routedSet, routedOther)
			time.Sleep(150 * time.Millisecond)
			primary, second := setDSCPWire(t, netnsPrimary), setDSCPWire(t, netnsSecondary)
			setDSCPExpect(t, p.name+", leaving by b4t0", primary, p.primary)
			setDSCPExpect(t, p.name+", leaving by b4t1", second, p.second)
			setDSCPExpectNone(t, p.name+", b4t0", primary, routedSet, routedOther)
			setDSCPExpectNone(t, p.name+", b4t1", second, setDSCPAddrA)
			setDSCPAssertQuiet(t, rec, p.cfg, p.name)
		}
	})
}

func TestNetnsSetDSCPInjectedParity(t *testing.T) {
	const late = "10.201.9.33"
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		client := setDSCPClient(t)
		setDSCPObserve(t)
		cfg := setDSCPConfig(engine, true,
			dscpPlanTestSet("dscp-a", 31, setDSCPAddrA),
			dscpPlanTestSet("dscp-b", 6, "10.201.9.0/28"),
		)
		q := uint32(cfg.Queue.Mark)
		setDSCPConntrackProbe(t, q)
		setDSCPStart(t, cfg)
		setDSCPAssertPerSet(t, engine)
		notrack := setDSCPNotrack(t, engine)
		liberal := setDSCPSysctl(t, "net/netfilter/nf_conntrack_tcp_be_liberal")
		src, dst := net.ParseIP(dscpClientIP), net.ParseIP(setDSCPAddrA)
		want := map[string]int{setDSCPAddrA: 31}

		for i, mode := range []string{"1", "0"} {
			liberal(mode)
			sport := uint16(41200 + i)
			what := "be_liberal " + mode + ": "
			setDSCPCheck(t, what+"the client's own SYN", want, func() { setDSCPClientDial(t, client, sport, setDSCPAddrA) })
			setDSCPCheck(t, what+"the SYN b4 sends again from this host", want, func() {
				setDSCPSendRaw(t, q, netnsTCPPacket(src, dst, sport, 443))
			})

			invalid := dscpCounter(t, 0, "invalid")
			setDSCPCheck(t, what+"a fake b4 sends into the flow that conntrack cannot place", want, func() {
				setDSCPSendRaw(t, q, setDSCPFakeACK(src, dst, sport, 443))
			})
			if got := dscpCounter(t, 0, "invalid") - invalid; got != 1 {
				t.Errorf("%sconntrack marked %d of 1 fake packets invalid, so the test did not send one it cannot place", what, got)
			}

			untracked, matched := dscpCounter(t, 0, "untracked"), notrack()
			setDSCPCheck(t, what+"a packet sent on untracked with the queue mark and 0x10000000", want, func() {
				setDSCPSendRaw(t, q|b4engine.ReinjectMarkBit, netnsTCPPacket(src, dst, sport, 443))
			})
			if got := dscpCounter(t, 0, "untracked") - untracked; got != 1 || notrack()-matched != 1 {
				t.Errorf("%sthe re-sent packet was not untracked (untracked %d, NOTRACK rule %d), so the test proves nothing about it", what, got, notrack()-matched)
			}
		}

		const sport = 41300
		setDSCPCheck(t, "a client's UDP flow before b4 learned its address", map[string]int{late: dscpTestValue}, func() {
			setDSCPClientUDP(t, client, sport, 0, setDSCPDataPort, late)
		})
		setDSCPCheck(t, "a packet b4 injects toward that address before it learned it", map[string]int{late: dscpTestValue}, func() {
			setDSCPSendMarked(t, q, 41301, late)
		})
		setDSCPLearn(t, cfg, "dscp-b", late)
		setDSCPCheck(t, "the same UDP flow after b4 learned the address for a set", map[string]int{late: 6}, func() {
			setDSCPClientUDP(t, client, sport, 0, setDSCPDataPort, late)
		})
		setDSCPCheck(t, "a packet b4 injects toward that address after it learned it", map[string]int{late: 6}, func() {
			setDSCPSendMarked(t, q, 41302, late)
		})
	})
}

func TestNetnsSetDSCPLearnedSurvives(t *testing.T) {
	const learned = "10.201.9.11"
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		setDSCPObserve(t)
		build := func(global, value int) *config.Config {
			cfg := setDSCPConfig(engine, true, dscpPlanTestSet("dscp-a", value, setDSCPAddrA))
			cfg.System.Tables.DSCP.Value = global
			return cfg
		}
		sid := routeSanitizeSetID("dscp-a")
		check := func(what string, want map[string]int) {
			setDSCPCheck(t, what, want, func() { setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrs(want)...) })
		}

		cfg := build(dscpTestValue, 31)
		setDSCPStart(t, cfg)
		setDSCPAssertPerSet(t, engine)
		setDSCPLearn(t, cfg, "dscp-a", learned)
		check("after the address was learned", map[string]int{learned: 31, setDSCPAddrA: 31, setDSCPAddrOther: dscpTestValue})

		bins := setDSCPBinaries(engine)
		needle := "match-set " + dscpIptLearnedSet(sid, false) + " dst"
		counted := 0
		if len(bins) > 0 {
			counted = setDSCPRuleCount(t, bins[0], "mangle", dscpChainName, needle)
			if counted == 0 {
				t.Fatalf("the learned address's rule counted no packets, so the refresh check proves nothing")
			}
		}
		if err := RefreshRules(cfg); err != nil {
			t.Fatalf("RefreshRules: %v", err)
		}
		if len(bins) > 0 {
			if got := setDSCPRuleCount(t, bins[0], "mangle", dscpChainName, needle); got < counted {
				t.Errorf("a refresh with unchanged settings rebuilt the per-set rules (counter %d -> %d)", counted, got)
			}
		}
		check("after a refresh with unchanged settings", map[string]int{learned: 31, setDSCPAddrA: 31})

		global := build(12, 31)
		if err := RefreshRules(global); err != nil {
			t.Fatalf("RefreshRules with a new Set DSCP value: %v", err)
		}
		setDSCPSync(t, global)
		check("after the Set DSCP value changed", map[string]int{learned: 31, setDSCPAddrA: 31, setDSCPAddrOther: 12})

		value := build(12, 26)
		setDSCPSync(t, value)
		check("after the set's value changed", map[string]int{learned: 26, setDSCPAddrA: 26, setDSCPAddrOther: 12})

		if engine == backendNFTables {
			setDSCPDropNftRule(t, "@"+dscpNftLearnedSet(sid, false)+" ")
		} else {
			netnsRun(t, append([]string{bins[0], "-w", "-t", "mangle", "-D", dscpChainName}, dscpIptStampSpec(nil, dscpIptLearnedSet(sid, false), 26)...)...)
		}
		if !setDSCPMonitor(value) {
			t.Fatalf("the monitor check did not notice the deleted rule")
		}
		if setDSCPMonitor(value) {
			t.Errorf("a second monitor check rebuilt the rules again although they were in place")
		}
		check("after the monitor put the deleted rule back", map[string]int{learned: 26, setDSCPAddrA: 26, setDSCPAddrOther: 12})
	})
}

func setDSCPDropNftRule(t *testing.T, needle string) {
	t.Helper()
	out := netnsRun(t, "nft", "-a", "list", "chain", "inet", dscpNftTable, dscpNftChain)
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		_, handle, ok := strings.Cut(line, "# handle ")
		if !ok {
			break
		}
		netnsRun(t, "nft", "delete", "rule", "inet", dscpNftTable, dscpNftChain, "handle", strings.TrimSpace(handle))
		return
	}
	t.Fatalf("no rule with %q in the %s chain:\n%s", needle, dscpNftTable, out)
}

func TestNetnsSetDSCPShapeOnRealListings(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		rec := setDSCPRecord(t)
		build := func(global bool, ifaces ...string) *config.Config {
			v4only := dscpPlanTestSet("dscp-f", 10, "10.201.10.0/24")
			v4only.Targets.IPVersion = "4"
			static := dscpPlanTestSet("dscp-g", 25, "10.201.11.0/24")
			static.Targets.DomainOnly = true
			cfg := setDSCPConfig(engine, global,
				dscpPlanTestSet("dscp-a", 31, setDSCPAddrA, "2001:db8::10"),
				dscpPlanTestSet("dscp-b", 6, "10.201.9.0/28", "2001:db8::/64"),
				dscpPlanTestSet("dscp-e", 46, "2001:db8:1::/48"),
				v4only,
				static,
			)
			cfg.System.Tables.DSCP.Interfaces = ifaces
			return cfg
		}
		variants := []struct {
			name string
			cfg  *config.Config
		}{
			{"Set DSCP on, every interface", build(true)},
			{"Set DSCP off, two interfaces", build(false, netnsPrimary, netnsSecondary)},
			{"Set DSCP on, two interfaces", build(true, netnsPrimary, netnsSecondary)},
			{"Set DSCP off, every interface", build(false)},
		}
		for i, v := range variants {
			if i == 0 {
				setDSCPStart(t, v.cfg)
				setDSCPLearn(t, v.cfg, "dscp-a", "10.201.9.11", "2001:db8::11")
			} else {
				setDSCPSync(t, v.cfg)
			}
			st := setDSCPAssertPerSet(t, engine)
			setDSCPAssertShape(t, v.name, engine, st)
			before := setDSCPListing(t, engine)
			setDSCPAssertQuiet(t, rec, v.cfg, v.name)
			setDSCPAssertQuiet(t, rec, v.cfg, v.name+", second check")
			if after := setDSCPListing(t, engine); after != before {
				t.Errorf("%s: the monitor checks changed the listing:\nbefore:\n%s\nafter:\n%s", v.name, before, after)
			}
		}

		last := variants[len(variants)-1].cfg
		if engine == backendNFTables {
			setDSCPDropNftRule(t, "daddr vmap @s4_")
		} else {
			bin := setDSCPBinaries(engine)[0]
			netnsRun(t, append([]string{bin, "-w", "-t", "mangle", "-D", dscpChainName}, dscpIptGuardSpec(false)...)...)
		}
		if !setDSCPMonitor(last) {
			t.Fatalf("the monitor check accepted a chain with a rule missing, so the shape check proves nothing")
		}
		st := setDSCPAssertPerSet(t, engine)
		setDSCPAssertShape(t, "after the monitor rebuilt the chain", engine, st)
		setDSCPAssertQuiet(t, rec, last, "after the monitor rebuilt the chain")
	})
}

func TestNetnsSetDSCPRoutingSetSameAddress(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		setDSCPObserve(t)
		setDSCPNeighbor(t, netnsSecondary, netnsTarget)
		build := func(own int, shared bool) *config.Config {
			cfg := netnsConfig(engine)
			if own > 0 {
				cfg.Sets[0].DSCP = config.SetDSCPConfig{Enabled: true, Value: own}
			}
			if shared {
				cfg.Sets = append(cfg.Sets, dscpPlanTestSet("netns-dscp-set", 31, netnsTarget))
			}
			return cfg
		}
		routed := func() routeState {
			routeMu.Lock()
			defer routeMu.Unlock()
			st, ok := routeRuleCache["netns-egress-set"]
			if !ok {
				t.Fatal("the interface set built no routing state")
			}
			return st
		}
		send := func(what string, want map[string]int, link string, quiet string, fn func()) {
			t.Helper()
			setDSCPFlush(t)
			fn()
			time.Sleep(150 * time.Millisecond)
			setDSCPExpect(t, what, setDSCPWire(t, link), want)
			setDSCPExpectNone(t, what, setDSCPWire(t, quiet), netnsTarget)
		}

		cfg := build(0, true)
		setDSCPStart(t, cfg)
		RoutingSyncConfig(cfg)
		t.Cleanup(RoutingClearAll)
		setDSCPAssertPerSet(t, engine)
		setName := netnsRoutingSetName(t, engine)
		if engine == backendNFTables {
			netnsRun(t, "nft", "add", "element", "inet", routeNftTable, setName, "{", netnsTarget, "}")
		} else {
			netnsRun(t, "ipset", "add", setName, netnsTarget, "-exist")
		}
		before := routed()
		rules := netnsRuleLines(t)

		q := uint32(cfg.Queue.Mark)
		want := map[string]int{netnsTarget: 31}
		send("local UDP to an address an interface set routes and a DSCP set lists", want, netnsSecondary, netnsPrimary, func() {
			setDSCPSendUDP(t, 0, setDSCPDataPort, netnsTarget)
		})
		send("a packet b4 injects toward that address", want, netnsSecondary, netnsPrimary, func() {
			netnsSendMarked(t, q, 41401, 443)
		})
		send("a connection b4 opens for itself to that address", want, netnsPrimary, netnsSecondary, func() {
			netnsSendMarked(t, config.SelfDialMark, 41402, 443)
		})

		own := build(46, false)
		RoutingSyncConfig(own)
		setDSCPSync(t, own)
		setDSCPAssertPerSet(t, engine)
		send("local UDP once the interface set carries its own value", map[string]int{netnsTarget: 46}, netnsSecondary, netnsPrimary, func() {
			setDSCPSendUDP(t, 0, setDSCPDataPort, netnsTarget)
		})

		after := routed()
		if after.mark != before.mark || after.table != before.table {
			t.Errorf("the set's DSCP value changed its routing: mark 0x%x table %d became mark 0x%x table %d", before.mark, before.table, after.mark, after.table)
		}
		if got := netnsRuleLines(t); got != rules {
			t.Errorf("the set's DSCP value changed the policy rules:\nbefore:\n%s\nafter:\n%s", rules, got)
		}
	})
}

func TestNetnsSetDSCPTUNResend(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		setDSCPObserve(t)
		build := func(global bool, value int) *config.Config {
			cfg := setDSCPConfig(engine, global, dscpPlanTestSet("dscp-a", value, setDSCPAddrA))
			cfg.Queue.Mode = "tun"
			cfg.System.Tables.DSCP.Value = 12
			return cfg
		}
		cfg := build(false, 31)
		if err := ApplyDSCPOnly(cfg); err != nil {
			t.Fatalf("ApplyDSCPOnly: %v", err)
		}
		t.Cleanup(func() { ClearDSCPOnly(cfg) })
		setDSCPAssertPerSet(t, engine)
		notrack := setDSCPNotrack(t, engine)
		StartDSCPSync()
		t.Cleanup(StopDSCPSync)

		resend := uint32(cfg.Queue.Mark) | b4engine.ReinjectMarkBit
		matched := notrack()
		setDSCPCheck(t, "a packet the TUN engine sends on, untracked, with only a set's value configured", map[string]int{setDSCPAddrA: 31, setDSCPAddrOther: 0}, func() {
			setDSCPSendMarked(t, resend, 41501, setDSCPAddrA, setDSCPAddrOther)
		})
		if got := notrack() - matched; got != 2 {
			t.Errorf("the NOTRACK rule matched %d of the 2 packets sent on, so the test did not send them untracked", got)
		}
		setDSCPCheck(t, "a packet the TUN engine sends back toward a client", map[string]int{setDSCPAddrA: 0}, func() {
			setDSCPSendMarked(t, resend|b4engine.ClientMark, 41502, setDSCPAddrA)
		})

		value := build(false, 6)
		setDSCPSync(t, value)
		setDSCPCheck(t, "a packet sent on after the set's value changed", map[string]int{setDSCPAddrA: 6, setDSCPAddrOther: 0}, func() {
			setDSCPSendMarked(t, resend, 41503, setDSCPAddrA, setDSCPAddrOther)
		})

		global := build(true, 6)
		t.Cleanup(func() {
			masqLast.Store(nil)
			mssLast.Store(nil)
		})
		if err := RefreshTUNFirewall(global); err != nil {
			t.Fatalf("RefreshTUNFirewall: %v", err)
		}
		setDSCPSync(t, global)
		setDSCPCheck(t, "a packet sent on after Set DSCP was turned on", map[string]int{setDSCPAddrA: 6, setDSCPAddrOther: 12}, func() {
			setDSCPSendMarked(t, resend, 41504, setDSCPAddrA, setDSCPAddrOther)
		})

		ClearTUNFirewall(global)
		setDSCPAssertGone(t, engine, "after the TUN firewall was cleared")
		SyncDSCP(value)
		time.Sleep(300 * time.Millisecond)
		setDSCPAssertGone(t, engine, "after a save that arrived once the TUN firewall was cleared")
	})
}

func TestNetnsSetDSCPTeardownAndSweep(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		plan := func() *config.Config {
			return setDSCPConfig(engine, true,
				dscpPlanTestSet("dscp-a", 31, setDSCPAddrA),
				dscpPlanTestSet("dscp-x", 25, "10.201.9.64/26"),
			)
		}

		t.Run("b4 stops", func(t *testing.T) {
			cfg := plan()
			setDSCPStart(t, cfg)
			setDSCPAssertPerSet(t, engine)
			setDSCPLearn(t, cfg, "dscp-a", "10.201.9.11")
			if err := ClearAppliedRules(cfg); err != nil {
				t.Fatalf("ClearAppliedRules: %v", err)
			}
			SyncDSCP(cfg)
			time.Sleep(300 * time.Millisecond)
			StopDSCPSync()
			setDSCPAssertGone(t, engine, "after b4 stopped while a save was still on its way")
			if dscpLearnCur.Load() != nil {
				t.Errorf("after b4 stopped the learner still writes addresses for the sets")
			}
			if dscpStale.Load() != nil {
				t.Errorf("after b4 stopped DSCP objects are still parked for a later removal")
			}
		})

		t.Run("a crashed run left its rules behind", func(t *testing.T) {
			cfg := plan()
			setDSCPStart(t, cfg)
			setDSCPAssertPerSet(t, engine)
			setDSCPLearn(t, cfg, "dscp-a", "10.201.9.11")
			setDSCPForget()

			const orphan = "b4d_l_gone_v4"
			if engine != backendNFTables {
				netnsRun(t, "ipset", "create", orphan, "hash:net", "family", "inet", "timeout", "3600")
				netnsRun(t, "ipset", "add", orphan, "10.201.9.99", "timeout", "3600")
			}
			referenced := setDSCPIpsetNames()
			referenced = slices.DeleteFunc(referenced, func(name string) bool { return name == orphan })

			RoutingClearAll()
			left := setDSCPIpsetNames()
			if slices.Contains(left, orphan) {
				t.Errorf("the start-up sweep left %s behind, an ipset no rule uses", orphan)
			}
			if !slices.Equal(left, referenced) {
				t.Errorf("the start-up sweep must leave the ipsets the old chain still uses for the chain's teardown, got %v, want %v", left, referenced)
			}
			setDSCPAssertNoDestroyWarning(t, "the start-up sweep")

			next := setDSCPConfig(engine, false)
			if err := ClearRules(next); err != nil {
				t.Fatalf("ClearRules at start-up: %v", err)
			}
			setDSCPAssertGone(t, engine, "after the start-up clear")
			if err := AddRules(next); err != nil {
				t.Fatalf("AddRules without DSCP: %v", err)
			}
			setDSCPAssertGone(t, engine, "after a start without DSCP")
			if err := ClearRules(next); err != nil {
				t.Fatalf("ClearRules: %v", err)
			}
		})

		if engine == backendNFTables {
			return
		}
		t.Run("an ipset another rule still uses", func(t *testing.T) {
			cfg := plan()
			setDSCPStart(t, cfg)
			setDSCPAssertPerSet(t, engine)

			bin := setDSCPBinaries(engine)[0]
			held := dscpIptValueSet(25, false)
			hold := []string{"-m", "set", "--match-set", held, "dst", "-j", "ACCEPT"}
			netnsRun(t, append([]string{bin, "-w", "-t", "filter", "-A", "OUTPUT"}, hold...)...)
			release := func() { _, _ = run(append([]string{bin, "-w", "-t", "filter", "-D", "OUTPUT"}, hold...)...) }
			t.Cleanup(release)

			smaller := setDSCPConfig(engine, true, dscpPlanTestSet("dscp-a", 31, setDSCPAddrA))
			setDSCPSync(t, smaller)
			st := setDSCPAssertPerSet(t, engine)
			if !slices.Contains(st.ipt.pending, held) {
				t.Errorf("the ipset %s another rule still uses is not queued for a later destroy: %v", held, st.ipt.pending)
			}
			if !slices.Contains(setDSCPIpsetNames(), held) {
				t.Fatalf("the ipset %s disappeared although another rule uses it, so the test proves nothing", held)
			}
			setDSCPAssertNoDestroyWarning(t, "a destroy of an ipset in use")

			release()
			if setDSCPMonitor(smaller) {
				t.Errorf("the monitor rebuilt the DSCP rules while it only had an ipset to destroy")
			}
			if slices.Contains(setDSCPIpsetNames(), held) {
				t.Errorf("the monitor did not destroy %s once the other rule let go of it", held)
			}
			if st := dscpApplied.Load(); st == nil || st.ipt == nil || len(st.ipt.pending) > 0 {
				t.Errorf("the destroyed ipset is still queued for a later destroy")
			}
			if err := ClearRules(smaller); err != nil {
				t.Fatalf("ClearRules: %v", err)
			}
			setDSCPAssertGone(t, engine, "after ClearRules")
		})
	})
}

func setDSCPForget() {
	StopDSCPSync()
	dscpApplied.Store(nil)
	dscpStale.Store(nil)
	dscpIptRecordMu.Lock()
	clear(dscpIptRecord)
	dscpIptRecordMu.Unlock()
	dscpLearnResetState()
	rulesMu.Lock()
	rulesAppliedCfg, rulesAppliedBackend = nil, ""
	rulesMu.Unlock()
	routeEngine = nil
}

func setDSCPAssertNoDestroyWarning(t *testing.T, what string) {
	t.Helper()
	dscpWarned.Range(func(k, _ any) bool {
		if msg, _ := k.(string); strings.Contains(msg, "could not destroy the ipset") {
			t.Errorf("%s warned: %s", what, msg)
		}
		return true
	})
}

func setDSCPBigTargets(n int, odd bool) []string {
	var out []string
	for i := 0; i < n; i++ {
		if (i%2 == 1) == odd {
			out = append(out, fmt.Sprintf("10.%d.%d.0/24", 100+i/256, i%256))
		}
	}
	return out
}

func setDSCPBigAddr(i int) string {
	return fmt.Sprintf("10.%d.%d.1", 100+i/256, i%256)
}

func setDSCPMapElements(t *testing.T, generation int) int {
	t.Helper()
	return strings.Count(netnsRun(t, "nft", "list", "map", "inet", dscpNftTable, dscpNftMap(generation, false)), " : jump ")
}

func setDSCPScriptElements(t *testing.T, what string, cmds []string, limit int) int {
	t.Helper()
	total := 0
	for _, cmd := range cmds {
		if !strings.HasPrefix(cmd, "nft -f -\n") {
			continue
		}
		n := dscpNftTestElements(strings.TrimPrefix(cmd, "nft -f -\n"))
		total += n
		if n > limit {
			t.Errorf("%s: one nft transaction carried %d map elements, more than %d", what, n, limit)
		}
	}
	return total
}

func TestNetnsSetDSCPNftBigStatic(t *testing.T) {
	const (
		big       = 6000
		refused   = 1500
		refuseMax = 1200
	)
	setDSCPEngines(t, []string{backendNFTables}, func(t *testing.T, engine string) {
		setDSCPObserve(t)
		origNft := runNftStdin
		runNftStdin = func(script string) (string, error) {
			if dscpNftTestElements(script) > refuseMax {
				out := "netlink: Error: Could not process rule: Message too long"
				return out, fmt.Errorf("command [nft -f -] failed: exit status 1 (%s)", out)
			}
			return origNft(script)
		}
		t.Cleanup(func() { runNftStdin = origNft })
		rec := setDSCPRecord(t)

		build := func(n int, evenValue, oddValue int) *config.Config {
			return setDSCPConfig(engine, true,
				dscpPlanTestSet("dscp-even", evenValue, setDSCPBigTargets(n, false)...),
				dscpPlanTestSet("dscp-odd", oddValue, setDSCPBigTargets(n, true)...),
			)
		}
		spots := []int{0, 1, 2999, 5998, 5999}
		expect := func(evenValue, oddValue int) map[string]int {
			want := map[string]int{setDSCPBigAddr(big): dscpTestValue}
			for _, i := range spots {
				want[setDSCPBigAddr(i)] = evenValue
				if i%2 == 1 {
					want[setDSCPBigAddr(i)] = oddValue
				}
			}
			return want
		}

		cfg := build(big, 31, 6)
		mark := rec.mark()
		if err := ApplyDSCPOnly(cfg); err != nil {
			t.Fatalf("ApplyDSCPOnly with %d ranges: %v", big, err)
		}
		t.Cleanup(func() { ClearDSCPOnly(cfg) })
		st := setDSCPAssertPerSet(t, engine)
		if total := setDSCPScriptElements(t, "the first load", rec.since(mark), dscpNftFillChunk); total != big {
			t.Errorf("the first load sent %d map elements, want %d", total, big)
		}
		if got := setDSCPMapElements(t, st.nft.generation); got != big {
			t.Errorf("the kernel holds %d ranges in %s, want %d", got, dscpNftMap(st.nft.generation, false), big)
		}
		want := expect(31, 6)
		setDSCPCheck(t, "local UDP into a 6,000-range table", want, func() { setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrs(want)...) })

		StartDSCPSync()
		t.Cleanup(StopDSCPSync)
		swapped := build(big, 6, 31)
		setDSCPFlush(t)
		flood := setDSCPStartFlood(t, setDSCPAddrs(want)...)
		mark = rec.mark()
		setDSCPSync(t, swapped)
		time.Sleep(100 * time.Millisecond)
		if err := flood.halt(); err != nil {
			t.Fatalf("the sender stopped early: %v", err)
		}
		time.Sleep(150 * time.Millisecond)
		allowed := map[string][]int{setDSCPBigAddr(big): {dscpTestValue}}
		for _, i := range spots {
			allowed[setDSCPBigAddr(i)] = []int{31, 6}
		}
		setDSCPExpectOnly(t, "while 6,000 ranges changed their values", setDSCPWire(t, netnsPrimary), allowed, 100)
		if total := setDSCPScriptElements(t, "the change of 6,000 ranges", rec.since(mark), dscpNftFillChunk); total != big {
			t.Errorf("the change sent %d map elements, want %d", total, big)
		}
		next := setDSCPAssertPerSet(t, engine)
		if next.nft.generation == st.nft.generation {
			t.Fatalf("the change did not build a new generation of maps")
		}
		if got := setDSCPMapElements(t, next.nft.generation); got != big {
			t.Errorf("the kernel holds %d ranges in %s, want %d", got, dscpNftMap(next.nft.generation, false), big)
		}
		if _, err := run("nft", "list", "map", "inet", dscpNftTable, dscpNftMap(st.nft.generation, false)); err == nil {
			t.Errorf("the replaced map %s is still in the table", dscpNftMap(st.nft.generation, false))
		}
		want = expect(6, 31)
		setDSCPCheck(t, "local UDP after 6,000 ranges changed their values", want, func() { setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrs(want)...) })

		small := build(refused, 31, 6)
		mark = rec.mark()
		setDSCPSync(t, small)
		scripts := rec.since(mark)
		first := slices.IndexFunc(scripts, func(cmd string) bool { return strings.HasPrefix(cmd, "nft -f -\n") })
		if first < 0 || dscpNftTestElements(scripts[first]) != refused {
			t.Fatalf("the change to %d ranges did not try one transaction first, so the refusal never happened", refused)
		}
		if total := setDSCPScriptElements(t, "after nftables refused one transaction", scripts[first+1:], dscpNftFillChunk); total != refused {
			t.Errorf("after the refusal %d map elements were sent, want %d", total, refused)
		}
		last := setDSCPAssertPerSet(t, engine)
		if got := setDSCPMapElements(t, last.nft.generation); got != refused {
			t.Errorf("the kernel holds %d ranges in %s, want %d", got, dscpNftMap(last.nft.generation, false), refused)
		}
		want = map[string]int{setDSCPBigAddr(0): 31, setDSCPBigAddr(1): 6, setDSCPBigAddr(refused - 1): 6, setDSCPBigAddr(refused): dscpTestValue}
		setDSCPCheck(t, "local UDP after the ranges were loaded in batches", want, func() { setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrs(want)...) })
	})
}
