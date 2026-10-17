package nfq

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

const (
	directionSetName  = "YT images"
	directionConnHost = "1.2.3.4:443"
)

func hookAt(hook uint8) *uint8 {
	return &hook
}

func directionConfig(filter string) config.Config {
	cfg := config.NewConfig()
	set := newFragmentingYTImagesSet()
	set.TCP.DPortFilter = filter
	cfg.Sets = []*config.SetConfig{&set}
	return cfg
}

func directionWorker(t *testing.T, cfg *config.Config) *Worker {
	t.Helper()
	cfg.BuildTCPPortMap()
	cfg.BuildSetPortRanges()
	w := newTestWorker(t, cfg)
	w.sock = &sock.Sender{}
	w.direction = newQueueDirectionState(time.Now())
	t.Cleanup(w.wg.Wait)
	return w
}

func queueAt(w *Worker, raw []byte, hook *uint8) engine.PacketVerdict {
	id := uint32(1)
	vc := &verdictCtx{id: id}
	w.handleQueued(vc, nfqueue.Attribute{PacketID: &id, Payload: &raw, Hook: hook}, uint(w.getConfig().Queue.Mark))
	return vc.verdict
}

func clientHelloFrom(port uint16) []byte {
	return makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, port, 0x18)
}

func clientHelloV6From(port uint16) []byte {
	raw := makeV6TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000)
	binary.BigEndian.PutUint16(raw[40:42], port)
	return raw
}

func processedAsOutgoing(t *testing.T, ch chan string, v engine.PacketVerdict, port uint16) {
	t.Helper()
	if v != engine.VerdictDrop {
		t.Errorf("the ClientHello names a domain of the fragmenting set, so the outgoing path drops it and sends the fragments, got verdict %v", v)
	}
	lines := drainLines(ch)
	if len(lines) != 1 {
		t.Fatalf("one connection row for the ClientHello, got %q", lines)
	}
	f := strings.Split(lines[0], ",")
	if len(f) < 7 {
		t.Fatalf("malformed connection row: %q", lines[0])
	}
	set := f[5]
	if set == "" {
		set = f[2]
	}
	if set != directionSetName || !strings.HasSuffix(f[4], fmt.Sprintf(":%d", port)) || f[6] != directionConnHost {
		t.Errorf("the row must name the set and the client's connection, got %q", lines[0])
	}
}

func passedAsIncoming(t *testing.T, ch chan string, v engine.PacketVerdict) {
	t.Helper()
	if v != engine.VerdictAccept {
		t.Errorf("the incoming path finds no connection of its own for a client's packet and lets it through, got verdict %v", v)
	}
	if lines := drainLines(ch); len(lines) != 0 {
		t.Errorf("the incoming path logs no connection row, got %q", lines)
	}
}

func TestHookDirection(t *testing.T) {
	cases := []struct {
		hook uint8
		want engine.Direction
	}{
		{nfHookPreRouting, engine.DirReply},
		{1, engine.DirUnknown},
		{nfHookForward, engine.DirOriginal},
		{nfHookLocalOut, engine.DirOriginal},
		{nfHookPostRouting, engine.DirOriginal},
		{5, engine.DirUnknown},
	}
	for _, c := range cases {
		if got := hookDirection(c.hook); got != c.want {
			t.Errorf("hook %d: got direction %d, want %d", c.hook, got, c.want)
		}
	}
}

func TestATCPPortFilterOverClientSourcePortsNoLongerSkipsOutgoingTCP(t *testing.T) {
	const (
		outgoing = "outgoing"
		incoming = "incoming"
		today    = "today"
	)
	arrivals := []struct {
		name    string
		hook    *uint8
		queued  bool
		exact   bool
		outcome string
	}{
		{name: "postrouting with rules selected by conntrack direction", hook: hookAt(nfHookPostRouting), queued: true, exact: true, outcome: outgoing},
		{name: "prerouting with rules selected by conntrack direction", hook: hookAt(nfHookPreRouting), queued: true, exact: true, outcome: incoming},
		{name: "postrouting with rules selected by port", hook: hookAt(nfHookPostRouting), queued: true, outcome: today},
		{name: "queued without a hook", queued: true, exact: true, outcome: today},
		{name: "read from the tun", outcome: today},
	}
	rows := []struct {
		filter    string
		port      uint16
		processed bool
	}{
		{"443", 12345, true},
		{"443", 40000, true},
		{"443,10000-20000", 12345, false},
		{"443,10000-20000", 40000, true},
		{"1-65535", 12345, false},
		{"1-65535", 40000, false},
	}
	for _, row := range rows {
		for _, a := range arrivals {
			t.Run(fmt.Sprintf("%s/%d/%s", row.filter, row.port, a.name), func(t *testing.T) {
				cfg := directionConfig(row.filter)
				w := directionWorker(t, &cfg)
				w.direction.set(a.exact, a.exact, time.Now())
				ch, _ := log.GetConnectionHub().Subscribe()
				defer log.GetConnectionHub().Unsubscribe(ch)

				var v engine.PacketVerdict
				if a.queued {
					v = queueAt(w, clientHelloFrom(row.port), a.hook)
				} else {
					v = w.ProcessPacket(clientHelloFrom(row.port))
				}

				processed := row.processed
				switch a.outcome {
				case outgoing:
					processed = true
				case incoming:
					processed = false
				}
				if processed {
					processedAsOutgoing(t, ch, v, row.port)
				} else {
					passedAsIncoming(t, ch, v)
				}
			})
		}
	}
}

func TestTheDirectionOfOneFamilyDoesNotSpeakForTheOther(t *testing.T) {
	cfg := directionConfig("1-65535")
	cfg.Queue.IPv6Enabled = true
	w := directionWorker(t, &cfg)
	state := &runtimeState{direction: newQueueDirectionState(time.Now())}
	w.direction = state.direction
	pool := &Pool{Workers: []*Worker{w}, state: state}
	pool.SetQueueDirectionExact(true, false)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	processedAsOutgoing(t, ch, queueAt(w, clientHelloFrom(40000), hookAt(nfHookPostRouting)), 40000)

	v := queueAt(w, clientHelloV6From(40000), hookAt(nfHookPostRouting))
	if v != engine.VerdictAccept {
		t.Errorf("ip6tables without the conntrack match queues TCP by port alone, so an IPv6 client's packet keeps today's port test, got verdict %v", v)
	}
	if lines := drainLines(ch); len(lines) != 0 {
		t.Errorf("the IPv6 packet must keep today's port test and log nothing, got %q", lines)
	}

	pool.SetQueueDirectionExact(true, true)
	if v := queueAt(w, clientHelloV6From(40001), hookAt(nfHookPostRouting)); v != engine.VerdictDrop {
		t.Errorf("once the IPv6 rules select by conntrack direction the hook decides, got verdict %v", v)
	}
	if lines := drainLines(ch); len(lines) != 1 {
		t.Errorf("the IPv6 ClientHello is processed and logged once its rules select by direction, got %q", lines)
	}

	pool.SetQueueDirectionExact(false, false)
	passedAsIncoming(t, ch, queueAt(w, clientHelloFrom(40002), hookAt(nfHookPostRouting)))
}

func TestSkipSetupKeepsThePortTestWhateverTheLastApplyPublished(t *testing.T) {
	cfg := directionConfig("1-65535")
	cfg.System.Tables.SkipSetup = true
	w := directionWorker(t, &cfg)
	w.direction.set(true, true, time.Now())

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	passedAsIncoming(t, ch, queueAt(w, clientHelloFrom(40000), hookAt(nfHookPostRouting)))
}

func TestDiscoveryTakesTheDirectionFromTheHook(t *testing.T) {
	cfg := directionConfig("1-65535")
	cfg.Queue.IsDiscovery = true
	cfg.System.Tables.SkipSetup = true
	w := directionWorker(t, &cfg)
	const key = "10.0.0.1:40000->1.2.3.4:443"

	if v := queueAt(w, clientHelloFrom(40000), nil); v != engine.VerdictAccept {
		t.Fatalf("without a hook Discovery keeps the port test, got verdict %v", v)
	}
	if _, _, found := w.tlsCache.Lookup(key); found {
		t.Fatal("a packet sent to the incoming path must not reach the ClientHello parser")
	}

	if v := queueAt(w, clientHelloFrom(40000), hookAt(nfHookPreRouting)); v != engine.VerdictAccept {
		t.Fatalf("Discovery restores replies in prerouting, so the hook makes the packet a reply, got verdict %v", v)
	}
	if _, _, found := w.tlsCache.Lookup(key); found {
		t.Fatal("a packet queued in prerouting must not reach the ClientHello parser")
	}

	if v := queueAt(w, clientHelloFrom(40000), hookAt(nfHookLocalOut)); v != engine.VerdictDrop {
		t.Fatalf("Discovery queues its probe's packets in output, so the hook makes the ClientHello outgoing although the filter covers its source port and no apply published the families, got verdict %v", v)
	}
	if host, _, found := w.tlsCache.Lookup(key); !found || host != "i.ytimg.com" {
		t.Fatalf("the outgoing path must parse the ClientHello, got host %q found %v", host, found)
	}
}

func TestATunPacketCarriesItsOwnDirection(t *testing.T) {
	cases := []struct {
		dir       engine.Direction
		processed bool
	}{
		{engine.DirOriginal, true},
		{engine.DirReply, false},
		{engine.DirUnknown, false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("direction %d", c.dir), func(t *testing.T) {
			cfg := directionConfig("1-65535")
			w := directionWorker(t, &cfg)
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			v := w.ProcessPacketDir(clientHelloFrom(40000), c.dir)
			if c.processed {
				processedAsOutgoing(t, ch, v, 40000)
			} else {
				passedAsIncoming(t, ch, v)
			}
		})
	}
}

func TestAQueuedPacketsDirectionComesFromTheHookOnlyForTCP(t *testing.T) {
	cfg := directionConfig("443")
	w := directionWorker(t, &cfg)
	w.direction.set(true, true, time.Now())

	tcp := &verdictCtx{hook: nfHookPostRouting, hooked: true}
	pkt, ok := w.parseIPHeaders(clientHelloFrom(40000))
	if !ok {
		t.Fatal("fixture does not parse")
	}
	if got := w.packetDirection(&cfg, tcp, pkt); got != engine.DirOriginal {
		t.Errorf("a TCP packet queued in postrouting by direction-exact rules: got %d, want original", got)
	}

	udp := &verdictCtx{hook: nfHookPostRouting, hooked: true}
	datagram, ok := w.parseIPHeaders(makeV4UDPPacket([]byte("x"), []byte{10, 0, 0, 1}, []byte{1, 2, 3, 4}, 40000, 443))
	if !ok {
		t.Fatal("UDP fixture does not parse")
	}
	if got := w.packetDirection(&cfg, udp, datagram); got != engine.DirUnknown {
		t.Errorf("the UDP window rule selects no conntrack direction, so a datagram's hook says nothing: got %d", got)
	}

	tun := &verdictCtx{dir: engine.DirReply}
	if got := w.packetDirection(&cfg, tun, pkt); got != engine.DirReply {
		t.Errorf("a packet without a hook keeps the direction its reader gave it: got %d", got)
	}

	var nilState *queueDirectionState
	if nilState.exactFor(false) || nilState.exactFor(true) {
		t.Error("a worker without the shared state must fall back to the port test")
	}
}

func TestSetQueueDirectionExactToleratesAPoolWithoutState(t *testing.T) {
	var none *Pool
	none.SetQueueDirectionExact(true, true)
	none.reportQueueDirection(time.Now())
	bare := &Pool{}
	bare.SetQueueDirectionExact(true, true)
	bare.reportQueueDirection(time.Now())
	partial := &Pool{state: &runtimeState{}}
	partial.SetQueueDirectionExact(true, true)
	partial.reportQueueDirection(time.Now().Add(queueDirectionSettle))
}

func TestPortClassifierWarning(t *testing.T) {
	overlapping := func(mutate func(*config.Config)) *config.Config {
		cfg := directionConfig("443,1-65535")
		if mutate != nil {
			mutate(&cfg)
		}
		return &cfg
	}
	cases := []struct {
		name    string
		cfg     *config.Config
		v4, v6  bool
		want    []string
		without []string
	}{
		{name: "no config", cfg: nil},
		{name: "a filter clear of the client source ports", cfg: func() *config.Config { c := directionConfig("443,10000-20000"); return &c }()},
		{name: "IPv4 selected by direction", cfg: overlapping(nil), v4: true},
		{
			name:    "IPv4 selected by port",
			cfg:     overlapping(nil),
			want:    []string{"IPv4 TCP:", "32768-65535", "conntrack direction", "cannot tell them apart", "pass through unprocessed", "below 32768", "xt_conntrack", "nft_ct"},
			without: []string{"IPv6", "skip_setup"},
		},
		{
			name:    "IPv6 selected by port while IPv4 is exact",
			cfg:     overlapping(func(c *config.Config) { c.Queue.IPv6Enabled = true }),
			v4:      true,
			want:    []string{"IPv6 TCP:"},
			without: []string{"IPv4"},
		},
		{
			name: "both families selected by port",
			cfg:  overlapping(func(c *config.Config) { c.Queue.IPv6Enabled = true }),
			want: []string{"IPv4 and IPv6 TCP:"},
		},
		{
			name:    "skip_setup classifies by port whatever was published",
			cfg:     overlapping(func(c *config.Config) { c.System.Tables.SkipSetup = true }),
			v4:      true,
			v6:      true,
			want:    []string{"IPv4 TCP:", "skip_setup", "below 32768"},
			without: []string{"conntrack direction", "xt_conntrack"},
		},
		{name: "Discovery takes the direction from the hook", cfg: overlapping(func(c *config.Config) { c.Queue.IsDiscovery = true })},
		{
			name: "the TUN engine reads no IPv6",
			cfg: overlapping(func(c *config.Config) {
				c.Queue.Mode = "tun"
				c.Queue.IPv6Enabled = true
			}),
			v4: true,
		},
		{name: "a family that is off", cfg: overlapping(func(c *config.Config) { c.Queue.IPv4Enabled = false })},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := queueDirectionWarning(c.cfg, c.v4, c.v6)
			if len(c.want) == 0 && got != "" {
				t.Fatalf("no warning expected, got %q", got)
			}
			for _, s := range c.want {
				if !strings.Contains(got, s) {
					t.Errorf("warning %q lacks %q", got, s)
				}
			}
			for _, s := range c.without {
				if strings.Contains(got, s) {
					t.Errorf("warning %q must not mention %q", got, s)
				}
			}
		})
	}
}

func newestMetricsEventID() uint64 {
	if items := metrics.GetMetricsCollector().Hello().Events.Items; len(items) > 0 {
		return items[0].ID
	}
	return 0
}

func directionWarnings(since uint64) []metrics.Event {
	var out []metrics.Event
	for _, ev := range metrics.GetMetricsCollector().Hello().Events.Items {
		if ev.ID > since && ev.Code == metrics.EventDirectionByPort && strings.Contains(ev.Args["error"], "TCP port filters") {
			out = append(out, ev)
		}
	}
	return out
}

func TestThePortClassifierWarningIsRaisedOncePerSettledChange(t *testing.T) {
	cfg := directionConfig("1-65535")
	w := directionWorker(t, &cfg)
	state := &runtimeState{direction: newQueueDirectionState(time.Now())}
	w.direction = state.direction
	pool := &Pool{Workers: []*Worker{w}, state: state}
	since := newestMetricsEventID()

	t0 := time.Now()
	state.direction.noteChange(t0)
	pool.reportQueueDirection(t0.Add(queueDirectionSettle - time.Second))
	if got := directionWarnings(since); len(got) != 0 {
		t.Fatalf("a state younger than the settle time can be the gap between a clear and the next apply, got %+v", got)
	}

	pool.reportQueueDirection(t0.Add(queueDirectionSettle))
	got := directionWarnings(since)
	if len(got) != 1 {
		t.Fatalf("one warning once the state has settled, got %+v", got)
	}
	if got[0].Level != metrics.LevelWarning || !strings.Contains(got[0].Message, "32768-65535") {
		t.Fatalf("the warning names the overlapping ports at warning level, got %+v", got[0])
	}

	pool.reportQueueDirection(t0.Add(3 * queueDirectionSettle))
	state.direction.noteChange(t0.Add(4 * queueDirectionSettle))
	pool.reportQueueDirection(t0.Add(6 * queueDirectionSettle))
	if got := directionWarnings(since); len(got) != 1 {
		t.Fatalf("an unchanged warning is not raised again, got %+v", got)
	}

	t1 := t0.Add(10 * queueDirectionSettle)
	state.direction.set(true, true, t1)
	pool.reportQueueDirection(t1.Add(queueDirectionSettle))
	state.direction.set(false, false, t1.Add(queueDirectionSettle+time.Second))
	pool.reportQueueDirection(t1.Add(queueDirectionSettle + 2*time.Second))
	state.direction.set(true, true, t1.Add(queueDirectionSettle+5*time.Second))
	pool.reportQueueDirection(t1.Add(2*queueDirectionSettle + 5*time.Second))
	if got := directionWarnings(since); len(got) != 1 {
		t.Fatalf("a refresh clears the rules and applies them again: the cleared state in between must not raise a warning, got %+v", got)
	}

	t2 := t1.Add(10 * queueDirectionSettle)
	state.direction.set(false, false, t2)
	pool.reportQueueDirection(t2.Add(queueDirectionSettle))
	if got := directionWarnings(since); len(got) != 2 {
		t.Fatalf("losing the direction match after it was gone from the warnings raises it again, got %+v", got)
	}
}

func TestAConfigChangeIsWarnedAboutOnceItSettles(t *testing.T) {
	cfg := directionConfig("443")
	w := directionWorker(t, &cfg)
	state := &runtimeState{direction: newQueueDirectionState(time.Now())}
	w.direction = state.direction
	pool := &Pool{Workers: []*Worker{w}, state: state}
	since := newestMetricsEventID()

	t0 := time.Now()
	state.direction.noteChange(t0)
	pool.reportQueueDirection(t0.Add(queueDirectionSettle))
	if got := directionWarnings(since); len(got) != 0 {
		t.Fatalf("a filter of 443 overlaps nothing, got %+v", got)
	}

	wide := directionConfig("40000-41000")
	wide.BuildTCPPortMap()
	wide.BuildSetPortRanges()
	before := time.Now()
	if err := pool.UpdateConfig(&wide); err != nil {
		t.Fatal(err)
	}
	pool.reportQueueDirection(before.Add(queueDirectionSettle - time.Second))
	if got := directionWarnings(since); len(got) != 0 {
		t.Fatalf("a config change waits for the rules refresh it triggers, got %+v", got)
	}
	pool.reportQueueDirection(time.Now().Add(queueDirectionSettle))
	got := directionWarnings(since)
	if len(got) != 1 || !strings.Contains(got[0].Message, "40000-41000") {
		t.Fatalf("the new filter is warned about once, naming its ports, got %+v", got)
	}
}

func TestADiscoveryPoolRaisesNoPortClassifierWarning(t *testing.T) {
	cfg := directionConfig("1-65535")
	cfg.Queue.IsDiscovery = true
	w := directionWorker(t, &cfg)
	state := &runtimeState{direction: newQueueDirectionState(time.Now())}
	w.direction = state.direction
	pool := &Pool{Workers: []*Worker{w}, state: state}
	since := newestMetricsEventID()

	t0 := time.Now()
	state.direction.noteChange(t0)
	pool.reportQueueDirection(t0.Add(queueDirectionSettle))
	if got := directionWarnings(since); len(got) != 0 {
		t.Fatalf("Discovery takes the direction from the hook, got %+v", got)
	}
}

func clientHelloFromTo(src, dst [4]byte, port uint16) []byte {
	raw := clientHelloFrom(port)
	copy(raw[12:16], src[:])
	copy(raw[16:20], dst[:])
	return raw
}

func TestASetThatListsAPrivateAddressStillProcessesIt(t *testing.T) {
	lanClient := [4]byte{192, 168, 1, 20}
	listed := [4]byte{10, 1, 2, 3}
	unlisted := [4]byte{172, 16, 0, 9}
	lanServer := [4]byte{192, 168, 1, 50}
	for _, c := range []struct {
		name      string
		ips       []string
		ports     string
		dst       [4]byte
		want      engine.PacketVerdict
		untouched bool
		why       string
	}{
		{"an address the set lists", []string{"10.0.0.0/8"}, "443", listed, engine.VerdictDrop, false, "a set that lists 10.0.0.0/8 fragments a ClientHello to 10.1.2.3"},
		{"a private address no set lists", []string{"10.0.0.0/8"}, "443", unlisted, engine.VerdictAccept, true, "a private address no set lists passes untouched, although the ClientHello names a domain of the set"},
		{"a listed address on a port the set leaves out", []string{"10.0.0.0/8"}, "8443", listed, engine.VerdictAccept, true, "the set does not take port 443, so the address counts as unlisted there and the ClientHello is not even read"},
		{"a set that matches any address", []string{"0.0.0.0/0"}, "443", lanServer, engine.VerdictAccept, true, "Match any IP address lists no private address, so a port-forwarded LAN server stays untouched"},
		{"a private range next to any address", []string{"0.0.0.0/0", "10.0.0.0/8"}, "443", listed, engine.VerdictDrop, false, "the set's most specific network for 10.1.2.3 is the private range it lists"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.NewConfig()
			set := newFragmentingYTImagesSet()
			set.Targets.DomainOnly = false
			set.Targets.IPs = c.ips
			set.Targets.IpsToMatch = c.ips
			set.TCP.DPortFilter = c.ports
			cfg.Sets = []*config.SetConfig{&set}
			w := directionWorker(t, &cfg)
			w.direction.set(true, true, time.Now())
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			if v := queueAt(w, clientHelloFromTo(lanClient, c.dst, 40010), hookAt(nfHookPostRouting)); v != c.want {
				t.Errorf("verdict %v, want %v: %s", v, c.want, c.why)
			}
			if !c.untouched {
				return
			}
			if lines := drainLines(ch); len(lines) != 0 {
				t.Errorf("%s, yet b4 logged a connection row: %q", c.why, lines)
			}
			key := fmt.Sprintf("192.168.1.20:40010->%d.%d.%d.%d:443", c.dst[0], c.dst[1], c.dst[2], c.dst[3])
			if _, _, found := w.tlsCache.Lookup(key); found {
				t.Errorf("%s, yet b4 read the ClientHello", c.why)
			}
		})
	}
}

func TestAClientHelloToAPrivateAddressPassesUntouched(t *testing.T) {
	wanClient := [4]byte{203, 0, 113, 5}
	lanServer := [4]byte{192, 168, 1, 10}
	publicServer := [4]byte{1, 2, 3, 4}
	portOnly := func() config.SetConfig {
		set := newFragmentingYTImagesSet()
		set.Targets.DomainOnly = false
		set.Targets.DomainsToMatch = nil
		return set
	}
	sets := []struct {
		name string
		make func() config.SetConfig
	}{
		{"set matched by SNI", newFragmentingYTImagesSet},
		{"port-only set", portOnly},
	}
	arrivals := []struct {
		name    string
		exact   bool
		deliver func(w *Worker, raw []byte) engine.PacketVerdict
	}{
		{"queued in postrouting by direction-exact rules", true, func(w *Worker, raw []byte) engine.PacketVerdict {
			return queueAt(w, raw, hookAt(nfHookPostRouting))
		}},
		{"queued by port", false, func(w *Worker, raw []byte) engine.PacketVerdict {
			return queueAt(w, raw, hookAt(nfHookPostRouting))
		}},
		{"read from the tun as the client's", false, func(w *Worker, raw []byte) engine.PacketVerdict {
			return w.ProcessPacketDir(raw, engine.DirOriginal)
		}},
	}
	for _, s := range sets {
		for _, a := range arrivals {
			t.Run(s.name+"/"+a.name, func(t *testing.T) {
				cfg := config.NewConfig()
				set := s.make()
				set.TCP.DPortFilter = "443"
				cfg.Sets = []*config.SetConfig{&set}
				w := directionWorker(t, &cfg)
				w.direction.set(a.exact, a.exact, time.Now())
				ch, _ := log.GetConnectionHub().Subscribe()
				defer log.GetConnectionHub().Unsubscribe(ch)

				if v := a.deliver(w, clientHelloFromTo(wanClient, lanServer, 40000)); v != engine.VerdictAccept {
					t.Errorf("a ClientHello to a port-forwarded LAN server must pass untouched, got verdict %v", v)
				}
				if lines := drainLines(ch); len(lines) != 0 {
					t.Errorf("a ClientHello to a private address logs no connection row, got %q", lines)
				}
				if _, _, found := w.tlsCache.Lookup("203.0.113.5:40000->192.168.1.10:443"); found {
					t.Error("a ClientHello to a private address must not reach the ClientHello parser")
				}

				processedAsOutgoing(t, ch, a.deliver(w, clientHelloFromTo(wanClient, publicServer, 40001)), 40001)
			})
		}
	}
}
