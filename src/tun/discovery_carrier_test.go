package tun

import (
	"errors"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"golang.org/x/sys/unix"
)

type laneProc struct {
	mu       sync.Mutex
	seen     int
	dirs     []engine.Direction
	verdicts []engine.PacketVerdict
}

func (p *laneProc) ProcessPacketDir(_ []byte, dir engine.Direction) engine.PacketVerdict {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen++
	p.dirs = append(p.dirs, dir)
	if len(p.verdicts) == 0 {
		return engine.VerdictAccept
	}
	v := p.verdicts[0]
	p.verdicts = p.verdicts[1:]
	return v
}

type laneSender struct {
	mu     sync.Mutex
	sent   []string
	closed atomic.Bool
}

func (s *laneSender) SendIPv4(_ []byte, dst net.IP) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, dst.String())
	return nil
}

func (s *laneSender) Close() { s.closed.Store(true) }

func (s *laneSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func packetPair(t *testing.T) (*os.File, int) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no seqpacket socketpair: %v", err)
	}
	if err := unix.SetNonblock(fds[0], true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fds[1]) })
	return os.NewFile(uintptr(fds[0]), "carrier-test"), fds[1]
}

func stubCarrierIO(t *testing.T) (*laneSender, *int) {
	t.Helper()
	origOpen, origSend, origV6, origEvery := openCarrierDevice, newCarrierSender, disableCarrierIPv6, carrierReconcileEvery
	t.Cleanup(func() {
		openCarrierDevice, newCarrierSender, disableCarrierIPv6, carrierReconcileEvery = origOpen, origSend, origV6, origEvery
	})
	opened := 0
	openCarrierDevice = func(name string) (*os.File, string, error) {
		opened++
		f, _ := packetPair(t)
		return f, name, nil
	}
	sender := &laneSender{}
	newCarrierSender = func(int) (ipv4Sender, error) { return sender, nil }
	disableCarrierIPv6 = func(string) {}
	carrierReconcileEvery = time.Hour
	return sender, &opened
}

func carrierEngine(capture, src string) *Engine {
	cfg := config.NewConfig()
	e := NewEngine(&cfg, nil)
	e.tunName = "b4tun-test-none"
	e.routes = &routeManager{
		tunName:         e.tunName,
		srcIP:           src,
		resolvedCapture: capture,
		routeTable:      97,
		captureTable:    96,
		outIface:        "b4-test-none0",
		quit:            e.quit,
	}
	return e
}

func adds(calls []string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, "ip rule add ") || strings.HasPrefix(c, "ip route replace ") {
			out = append(out, c)
		}
	}
	return out
}

func TestTheCarrierRoutesTheFlowMarkIntoItsOwnTableAndRemovesExactlyWhatItAdded(t *testing.T) {
	f := stubRules(t, stockRules)
	sender, _ := stubCarrierIO(t)
	e := carrierEngine("ports", "203.0.113.2")

	c, err := e.startDiscoveryCarrier(DiscoveryCarrierRequest{FlowMark: 0x8001, InjectedMark: 0x8002}, &laneProc{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ip route replace default dev b4dsc0 src 203.0.113.2 table 60",
		"ip rule add fwmark 0x8001/0xffffffff lookup 60 priority 81",
	}
	if got := adds(f.calls); !slices.Equal(got, want) {
		t.Fatalf("port capture on a stock rule list needs one rule and one route, got\n%s", strings.Join(got, "\n"))
	}
	if f.index("ip link set b4dsc0 up") < 0 || f.index("ip link set b4dsc0 mtu 1500") < 0 {
		t.Errorf("the device must be brought up with an MTU, calls:\n%s", strings.Join(f.calls, "\n"))
	}

	c.Stop()
	c.Stop()
	del := f.index("ip rule del fwmark 0x8001/0xffffffff lookup 60 priority 81")
	flush := f.index("ip route flush table 60")
	if del < 0 || flush < 0 || del > flush {
		t.Errorf("Stop must delete the rule by its exact spec, then flush the table, calls:\n%s", strings.Join(f.calls, "\n"))
	}
	if n := strings.Count(strings.Join(f.calls, "\n"), "ip rule del fwmark 0x8001/0xffffffff lookup 60 priority 81"); n != 1 {
		t.Errorf("a second Stop must not repeat the teardown, rule deleted %d times", n)
	}
	if !sender.closed.Load() {
		t.Error("Stop must close the carrier's sender")
	}
}

func TestTheCarrierGuardsLocalDestinationsAndKeepsDiscoverysDNSOffTheTunnelInWholeDefault(t *testing.T) {
	f := stubRules(t, rosRules)
	stubCarrierIO(t)
	e := carrierEngine("default", "203.0.113.2")

	c, err := e.startDiscoveryCarrier(DiscoveryCarrierRequest{FlowMark: 0x8001, InjectedMark: 0x8002}, &laneProc{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ip route replace default dev b4dsc0 src 203.0.113.2 table 60",
		"ip rule add fwmark 0x8001/0xffffffff lookup local priority 80",
		"ip rule add fwmark 0x8002/0xffffffff lookup local priority 80",
		"ip rule add fwmark 0x8001/0xffffffff lookup 60 priority 81",
		"ip rule add fwmark 0x8002/0xffffffff lookup main suppress_prefixlength 0 priority 82",
		"ip rule add fwmark 0x8002/0xffffffff lookup 97 priority 83",
	}
	if got := adds(f.calls); !slices.Equal(got, want) {
		t.Fatalf("whole-default with the local table at 200 needs the guards and the injected-mark bypass, got\n%s", strings.Join(got, "\n"))
	}
	c.Stop()
	var dels []string
	for _, call := range f.calls {
		if strings.HasPrefix(call, "ip rule del ") {
			dels = append(dels, call)
		}
	}
	wantDels := []string{
		"ip rule del fwmark 0x8002/0xffffffff lookup 97 priority 83",
		"ip rule del fwmark 0x8002/0xffffffff lookup main suppress_prefixlength 0 priority 82",
		"ip rule del fwmark 0x8001/0xffffffff lookup 60 priority 81",
		"ip rule del fwmark 0x8002/0xffffffff lookup local priority 80",
		"ip rule del fwmark 0x8001/0xffffffff lookup local priority 80",
	}
	if !slices.Equal(dels, wantDels) {
		t.Errorf("Stop must delete every rule it added, once each and in reverse order, got\n%s", strings.Join(dels, "\n"))
	}
}

func TestTheCarrierRefusesWithoutAnUplinkSource(t *testing.T) {
	f := stubRules(t, stockRules)
	_, opened := stubCarrierIO(t)
	e := carrierEngine("ports", "")

	if _, err := e.startDiscoveryCarrier(DiscoveryCarrierRequest{FlowMark: 0x8001, InjectedMark: 0x8002}, &laneProc{}); err == nil {
		t.Fatal("without a source address the probes would leave with an address the uplink does not route back, the carrier must refuse")
	}
	if *opened != 0 || len(adds(f.calls)) != 0 {
		t.Errorf("a refusal must change nothing, opened %d, calls:\n%s", *opened, strings.Join(f.calls, "\n"))
	}
}

func TestTheCarrierRefusesOnAStoppedEngine(t *testing.T) {
	stubRules(t, stockRules)
	stubCarrierIO(t)
	e := carrierEngine("ports", "203.0.113.2")
	close(e.quit)
	if _, err := e.startDiscoveryCarrier(DiscoveryCarrierRequest{FlowMark: 0x8001, InjectedMark: 0x8002}, &laneProc{}); err == nil {
		t.Fatal("a stopping engine must not start a carrier")
	}
}

func TestTheCarrierTearsDownWhatItAddedWhenARuleIsRefused(t *testing.T) {
	f := stubRules(t, stockRules)
	sender, _ := stubCarrierIO(t)
	f.fail = func(cmd string) bool { return strings.HasPrefix(cmd, "ip rule add fwmark 0x8001/0xffffffff lookup 60") }
	e := carrierEngine("ports", "203.0.113.2")

	if _, err := e.startDiscoveryCarrier(DiscoveryCarrierRequest{FlowMark: 0x8001, InjectedMark: 0x8002}, &laneProc{}); err == nil {
		t.Fatal("a refused flow rule must fail the start")
	}
	if f.index("ip route flush table 60") < 0 || !sender.closed.Load() {
		t.Errorf("a failed start must flush its table and close its sender, calls:\n%s", strings.Join(f.calls, "\n"))
	}
	if f.index("ip rule del fwmark 0x8001/0xffffffff lookup 60 priority 81") >= 0 {
		t.Error("a rule that was never added must not be deleted")
	}
}

const staleCarrierRules = "0:\tfrom all lookup local\n" +
	"10:\tfrom all fwmark 0x40000000/0x40000000 lookup 96\n" +
	"80:\tfrom all fwmark 0x8001 lookup local\n" +
	"81:\tfrom all fwmark 0x8001 lookup 58\n" +
	"81:\tfrom all fwmark 0x8003 lookup 57\n" +
	"82:\tfrom all fwmark 0x8002 lookup main suppress_prefixlength 0\n" +
	"83:\tfrom all fwmark 0x8002 lookup 97\n" +
	"88:\tfrom all fwmark 0x20000000/0x20000000 lookup main suppress_prefixlength 0\n" +
	"100:\tfrom all fwmark 0x10000000/0x10000000 lookup 97\n" +
	"5000:\tfrom all fwmark 0x8001 lookup 59\n" +
	"32766:\tfrom all lookup main\n"

func TestTheCarrierSweepRemovesOnlyItsOwnLeftovers(t *testing.T) {
	dels, flush := planCarrierSweep(staleCarrierRules, 0x8001, 0x8002, nil)
	want := [][]string{
		{"ip", "rule", "del", "priority", "80", "fwmark", "0x8001", "lookup", "local"},
		{"ip", "rule", "del", "priority", "81", "fwmark", "0x8001", "lookup", "58"},
		{"ip", "rule", "del", "priority", "82", "fwmark", "0x8002", "lookup", "main", "suppress_prefixlength", "0"},
		{"ip", "rule", "del", "priority", "83", "fwmark", "0x8002", "lookup", "97"},
	}
	if !slices.EqualFunc(dels, want, slices.Equal[[]string]) {
		t.Fatalf("got %v, want %v", dels, want)
	}
	if !slices.Equal(flush, []string{"58"}) {
		t.Errorf("only the carrier's own table may be flushed, never the engine's uplink table, got %v", flush)
	}
	if _, flush := planCarrierSweep(staleCarrierRules, 0x8001, 0x8002, map[int]bool{58: true}); len(flush) != 0 {
		t.Errorf("a table the engine uses must not be flushed, got %v", flush)
	}
	shared := staleCarrierRules + "6000:\tfrom all lookup 58\n"
	if _, flush := planCarrierSweep(shared, 0x8001, 0x8002, nil); len(flush) != 0 {
		t.Errorf("a table another rule still looks up must not be flushed, got %v", flush)
	}
}

func TestTheEngineSweepLeavesTheCarrierRulesAlone(t *testing.T) {
	for _, explicit := range []int{0, 97, 58} {
		dels, flush := planTunRuleSweep(staleCarrierRules, ownedTunTables(explicit), 0x8000)
		for _, d := range dels {
			i := slices.Index(d.args, "priority")
			if i < 0 {
				continue
			}
			if p, _ := strconv.Atoi(d.args[i+1]); isCarrierPriority(p) {
				t.Errorf("explicit table %d: the engine's sweep deletes a carrier rule: %v", explicit, d.args)
			}
		}
		if slices.Contains(flush, "58") {
			t.Errorf("explicit table %d: the engine's sweep flushes the carrier table", explicit)
		}
	}
}

func TestExactMarkMatchesTheWaysIPPrintsAFullMask(t *testing.T) {
	cases := map[string]bool{
		"0x8001":            true,
		"0x8001/0xffffffff": true,
		"0x8001/0x8001":     false,
		"0x18001":           false,
		"0x8002":            false,
		"":                  false,
	}
	for fw, want := range cases {
		if got := exactMarkMatches(fw, 0x8001); got != want {
			t.Errorf("exactMarkMatches(%q) = %v, want %v", fw, got, want)
		}
	}
}

func TestTheCarrierTableAvoidsTheEnginesAndTakenTables(t *testing.T) {
	u := tableUsage{lookups: []string{"60"}, pinned: map[int]bool{59: true}}
	got, err := pickCarrierTable(u, map[int]bool{58: true})
	if err != nil || got != 57 {
		t.Fatalf("got %d, %v; want 57", got, err)
	}
	cr := carrierRouting{routeTable: 61, captureTable: 60, explicit: 50}
	avoid := cr.engineTables()
	for _, tbl := range []int{61, 60, 50, 49, xrayUITproxyTable} {
		if !avoid[tbl] {
			t.Errorf("table %d belongs to the engine and must be avoided", tbl)
		}
	}
	if DiscoveryDeviceName(&config.Config{Queue: config.QueueConfig{TUN: config.TUNConfig{DeviceName: "b4dsc0"}}}) == "b4dsc0" {
		t.Error("the carrier must not take the engine's device name")
	}
}

func ipv4Packet(dst string) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x45
	pkt[3] = 40
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], net.ParseIP("203.0.113.2").To4())
	copy(pkt[16:20], net.ParseIP(dst).To4())
	return pkt
}

func TestTheCarrierLaneProcessesOutboundIPv4AndDropsTheRest(t *testing.T) {
	file, peer := packetPair(t)
	proc := &laneProc{verdicts: []engine.PacketVerdict{engine.VerdictAccept, engine.VerdictDrop, engine.VerdictDropFlow}}
	sender := &laneSender{}
	c := &DiscoveryCarrier{name: "b4dsc-test", file: file, proc: proc, send: sender, quit: make(chan struct{})}
	c.laneWG.Add(1)
	go c.lane()

	v6 := make([]byte, 40)
	v6[0] = 0x60
	for _, pkt := range [][]byte{ipv4Packet("198.51.100.7"), v6, ipv4Packet("198.51.100.8"), ipv4Packet("198.51.100.9"), {0x45}} {
		if _, err := unix.Write(peer, pkt); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for c.processed.Load()+c.nonIPv4.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(c.quit)
	file.Close()
	c.laneWG.Wait()

	if c.processed.Load() != 3 || c.nonIPv4.Load() != 2 || c.dropped.Load() != 2 {
		t.Fatalf("processed %d, non-IPv4 %d, dropped %d; want 3, 2, 2", c.processed.Load(), c.nonIPv4.Load(), c.dropped.Load())
	}
	if !slices.Equal(sender.sent, []string{"198.51.100.7"}) {
		t.Errorf("only the accepted packet goes on, got %v", sender.sent)
	}
	for _, d := range proc.dirs {
		if d != engine.DirOriginal {
			t.Errorf("every carried packet is the router's own outbound packet, got direction %v", d)
		}
	}
}

type fakeKernel struct {
	mu    sync.Mutex
	rules []string
	route map[string]string
}

func (k *fakeKernel) ruleShow() string {
	return stockRules + strings.Join(k.rules, "")
}

func (k *fakeKernel) dropRule(prio string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.rules = slices.DeleteFunc(k.rules, func(r string) bool { return strings.HasPrefix(r, prio+":") })
}

func (k *fakeKernel) dropRoute(table string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.route, table)
}

func stubKernel(t *testing.T) *fakeKernel {
	t.Helper()
	k := &fakeKernel{route: map[string]string{}}
	orig := run
	t.Cleanup(func() { run = orig })
	run = func(args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		k.mu.Lock()
		defer k.mu.Unlock()
		switch {
		case cmd == "ip rule show":
			return k.ruleShow(), nil
		case strings.HasPrefix(cmd, "ip rule add fwmark "):
			k.rules = append(k.rules, args[len(args)-1]+":\tfrom all fwmark "+strings.TrimSuffix(args[4], "/0xffffffff")+" lookup "+args[6]+"\n")
			return "", nil
		case len(args) == 10 && strings.HasPrefix(cmd, "ip route replace default dev "):
			k.route[args[9]] = "default dev " + args[5] + " scope link src " + args[7] + "\n"
			return "", nil
		case strings.HasPrefix(cmd, "ip route show table "):
			return k.route[args[4]], nil
		case strings.HasPrefix(cmd, "ip rule del "), strings.Contains(cmd, " -D "):
			return "", errors.New("No such file or directory")
		}
		return "", nil
	}
	return k
}

func TestTheCarrierReportsALostRuleRouteOrSourceOnceAndNothingWhileIntact(t *testing.T) {
	cases := []struct {
		name    string
		breakIt func(k *fakeKernel, e *Engine)
	}{
		{"rule removed", func(k *fakeKernel, _ *Engine) { k.dropRule("81") }},
		{"route removed", func(k *fakeKernel, _ *Engine) { k.dropRoute("60") }},
		{"uplink source changed", func(_ *fakeKernel, e *Engine) {
			e.routes.mu.Lock()
			e.routes.srcIP = "203.0.113.9"
			e.routes.mu.Unlock()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := stubKernel(t)
			stubCarrierIO(t)
			carrierReconcileEvery = 2 * time.Millisecond
			origExists := carrierDeviceExists
			carrierDeviceExists = func(string) bool { return true }
			t.Cleanup(func() { carrierDeviceExists = origExists })
			e := carrierEngine("ports", "203.0.113.2")

			var lost atomic.Int32
			c, err := e.startDiscoveryCarrier(DiscoveryCarrierRequest{FlowMark: 0x8001, InjectedMark: 0x8002, Lost: func(error) { lost.Add(1) }}, &laneProc{})
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(40 * time.Millisecond)
			if got := lost.Load(); got != 0 {
				c.Stop()
				t.Fatalf("an intact carrier was reported lost %d time(s)", got)
			}
			tc.breakIt(k, e)
			deadline := time.Now().Add(2 * time.Second)
			for lost.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(2 * time.Millisecond)
			}
			time.Sleep(20 * time.Millisecond)
			c.Stop()
			if got := lost.Load(); got != 1 {
				t.Errorf("the loss must be reported exactly once, got %d", got)
			}
		})
	}
}
