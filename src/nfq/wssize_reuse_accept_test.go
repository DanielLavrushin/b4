package nfq

import (
	"encoding/binary"
	"net/netip"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/warmtuple"
	"github.com/florianl/go-nfqueue"
)

func reuseSetSYNRewritten(t *testing.T, dport uint16, edit func(*config.SetConfig)) bool {
	t.Helper()
	cfg, set := wsFixture()
	set.Id = "reuse-accept"
	set.TCP.WSSize.Cutoff = ""
	set.TCP.DPortFilter = "80,443"
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.TCP.WSSizeReuse.Enabled = true
	if edit != nil {
		edit(set)
	}
	cfg.BuildSetPortRanges()
	cfg.BuildTCPPortMap()
	w := newTestWorker(t, cfg)
	p := synWithOptions(IPv4, []byte{3, 3, 7, 0}, nil, 100)
	binary.BigEndian.PutUint16(p[22:24], dport)
	sock.FixTCPChecksum(p)
	w.dispatch(&verdictCtx{q: newFakeModQueue()}, p)
	return p[42] != 7
}

func TestReuseSetPassesDivertedTLSUntouched(t *testing.T) {
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	wsReuseDiverts = func(id string) bool { return id == "reuse-accept" }
	if reuseSetSYNRewritten(t, 443, nil) {
		t.Error("a SYN to port 443 of a set whose TPROXY diversion is active was rewritten; the reuse relay owns that connection and its warm carries the strategy")
	}
	if !reuseSetSYNRewritten(t, 80, nil) {
		t.Error("a SYN to port 80 of a diverted reuse set was passed untouched, though the diversion takes only port 443")
	}
	if !reuseSetSYNRewritten(t, 443, func(s *config.SetConfig) { s.TCP.WSSizeReuse.Enabled = false }) {
		t.Error("a SYN to port 443 of a set with reuse off was passed untouched because a diversion is published under its id")
	}
	if !reuseSetSYNRewritten(t, 443, func(s *config.SetConfig) { s.Id = "other" }) {
		t.Error("a SYN to port 443 of a reuse set whose own diversion is not active was passed untouched because another set's is")
	}
}

func TestARegisteredReuseFallbackGetsTheSetsStrategyWhileTheSetIsDiverted(t *testing.T) {
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	wsReuseDiverts = func(id string) bool { return id == "reuse-fallback" }
	cfg, set := wsFixture()
	set.Id = "reuse-fallback"
	set.TCP.WSSize.Cutoff = ""
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.TCP.WSSizeReuse.Enabled = true
	cfg.BuildSetPortRanges()
	cfg.BuildTCPPortMap()
	w := newTestWorker(t, cfg)
	w.warmFlows = warmtuple.NewFlows()
	syn := func(sport uint16) []byte {
		p := synWithOptions(IPv4, []byte{3, 3, 7, 0}, nil, 100)
		binary.BigEndian.PutUint16(p[20:22], sport)
		binary.BigEndian.PutUint16(p[22:24], 443)
		sock.FixTCPChecksum(p)
		return p
	}
	device := syn(41000)
	w.dispatch(&verdictCtx{q: newFakeModQueue()}, device)
	if device[42] != 7 {
		t.Fatal("a diverted connection of the reuse set was rewritten, so this test proves nothing")
	}
	fallback := syn(41001)
	local := netip.AddrPortFrom(netip.AddrFrom4([4]byte(fallback[12:16])), 41001)
	remote := netip.AddrPortFrom(netip.AddrFrom4([4]byte(fallback[16:20])), 443)
	release, err := w.warmFlows.Register(local, remote, cfg, set, "video.example.org")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	w.dispatch(&verdictCtx{q: newFakeModQueue()}, fallback)
	if binary.BigEndian.Uint16(fallback[34:36]) != 1 || fallback[42] != 1 || !sock.TCPChecksumValid(fallback) {
		t.Fatalf("a registered fallback SYN to port 443 of a diverted reuse set left with window %d and scale %d, want the set's 1 and 1", binary.BigEndian.Uint16(fallback[34:36]), fallback[42])
	}
}

func TestReuseSetKeepsItsStrategyWithoutDiversion(t *testing.T) {
	for _, dport := range []uint16{443, 80} {
		if !reuseSetSYNRewritten(t, dport, nil) {
			t.Errorf("a SYN to port %d of a set that reuses WSSIZE kept window scale 7 while nothing diverts the set to the reuse relay, so the connection got no strategy at all", dport)
		}
	}
}

func TestADivertedReuseSetPassesOnlyItsOwnFamiliesUntouched(t *testing.T) {
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	wsReuseDiverts = func(id string) bool { return id == "reuse-family" }
	for _, tc := range []struct {
		name      string
		version   string
		ver       byte
		untouched bool
	}{
		{"an IPv4-only set, an IPv6 SYN to an IPv6 address it lists", "4", IPv6, false},
		{"an IPv6-only set, an IPv4 SYN to an IPv4 address it lists", "6", IPv4, false},
		{"an IPv4-only set, an IPv4 SYN", "4", IPv4, true},
		{"a set of both families, an IPv6 SYN", "", IPv6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, set := wsFixture()
			set.Id = "reuse-family"
			set.TCP.WSSize.Cutoff = ""
			set.Targets.IPVersion = tc.version
			set.Targets.IpsToMatch = []string{"1.2.3.4", "2001:db8::2"}
			set.TCP.WSSizeReuse.Enabled = true
			cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled = true, true
			cfg.BuildSetPortRanges()
			cfg.BuildTCPPortMap()
			w := newTestWorker(t, cfg)
			p := synWithOptions(tc.ver, []byte{3, 3, 7, 0}, nil, 100)
			l3 := 20
			if tc.ver == IPv6 {
				l3 = IPv6HeaderLen
			}
			binary.BigEndian.PutUint16(p[l3+2:l3+4], 443)
			if tc.ver == IPv6 {
				sock.FixTCPChecksumV6(p)
			} else {
				sock.FixTCPChecksum(p)
			}
			w.dispatch(&verdictCtx{q: newFakeModQueue()}, p)
			if untouched := p[l3+22] == 7; untouched != tc.untouched {
				t.Errorf("%s: passed untouched=%v, want %v; the set's firewall sets hold no address of a family its ip_version leaves out, so such a connection is never diverted to the relay and must get the set's strategy", tc.name, untouched, tc.untouched)
			}
		})
	}
}

func reuseSetOwnDialVerdict(t *testing.T, telegram bool, mark *uint32, dst [4]byte) int {
	t.Helper()
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	set := newTelegramSet()
	if !telegram {
		set.Id, set.Name = "reuse-own", "reuse-own"
	}
	wsReuseDiverts = func(id string) bool { return id == set.Id }
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse.Enabled = true
	w := newInjectingWorker(t, &set)
	if telegram {
		w.getConfig().System.MTProto.UpstreamSet = set.Id
	}
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)
	hello := ytimgHello()
	copy(hello[16:20], dst[:])
	sock.FixIPv4Checksum(hello[:20])
	sock.FixTCPChecksum(hello)
	id, out := uint32(1), idx["eth7"]
	a := nfqueue.Attribute{PacketID: &id, Payload: &hello, Mark: mark, OutDev: &out}
	if mark != nil {
		uid := uint32(0)
		a.UID = &uid
	}
	q := newFakeQueue()
	w.handleQueued(&verdictCtx{id: id, q: q}, a, uint(w.getConfig().Queue.Mark))
	return q.now(t)
}

func TestBsOwnDialsToADivertedReuseSetGetItsStrategy(t *testing.T) {
	telegramMark, selfMark := config.TelegramSetDialMark, config.SelfDialMark
	listed, unlisted := [4]byte{1, 2, 3, 4}, [4]byte{5, 6, 7, 8}
	for _, c := range []struct {
		name     string
		telegram bool
		mark     *uint32
		dst      [4]byte
		want     int
	}{
		{"b4's own Telegram connection to an address the Telegram set lists", true, &telegramMark, listed, nfqueue.NfDrop},
		{"b4's own Telegram connection to an address the Telegram set does not list", true, &telegramMark, unlisted, nfqueue.NfDrop},
		{"another of b4's own connections to an address the set lists", false, &selfMark, listed, nfqueue.NfDrop},
		{"a device connection to an address the set lists", false, nil, listed, nfqueue.NfAccept},
		{"a device connection to an address the Telegram set lists", true, nil, listed, nfqueue.NfAccept},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := reuseSetOwnDialVerdict(t, c.telegram, c.mark, c.dst); got != c.want {
				t.Errorf("%s: verdict %d, want %d; every reuse rule returns on b4's own dial bit 0x%x, so such a connection never reaches the reuse relay and only the queue can give it the set's strategy, while a device connection is the relay's", c.name, got, c.want, config.SelfDialMark)
			}
		})
	}
}
