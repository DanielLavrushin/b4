package nfq

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

const (
	bannedHost = "rr2---sn-ab5l6nr6.googlevideo.com"
	bannedSet  = "1.googlevideo"
)

func newTestIPBans(t *testing.T, probe banProber) *ipBans {
	t.Helper()
	if probe == nil {
		probe = func(context.Context, net.IP, int, int) iphealth.TLSResult { return iphealth.TLSReplied }
	}
	b := newIPBans(iphealth.NewBanRegistry(""), iphealth.NewKnownGood(), probe)
	b.enabled.Store(true)
	t.Cleanup(b.stop)
	return b
}

func banAddress(t *testing.T, b *ipBans, ip string, port int) {
	t.Helper()
	if _, _, ok := b.reg.Strike(net.ParseIP(ip), port, bannedHost, bannedSet, time.Hour, 2*time.Hour); !ok {
		t.Fatalf("could not ban %s:%d", ip, port)
	}
}

func banningAddressSet() config.SetConfig {
	set := newStaticAddressSet(false)
	set.Targets.IpsToMatch = []string{"1.2.3.4", "2001:db8::2"}
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	return set
}

func bareSyn(ver byte) []byte {
	if ver == IPv4 {
		return makeV4TCPPacketFlags(nil, 1000, 0x02)
	}
	pkt := makeV6TCPPacket(nil, 1000)
	pkt[IPv6HeaderLen+13] = 0x02
	return pkt
}

func resetsIn(sent [][]byte) int {
	n := 0
	for _, p := range sent {
		l3 := IPv6HeaderLen
		if p[0]>>4 == IPv4 {
			l3 = int(p[0]&0x0f) * 4
		}
		if len(p) > l3+13 && p[l3+13]&0x04 != 0 {
			n++
		}
	}
	return n
}

func connectionReasons(ch chan string) []string {
	var reasons []string
	for {
		select {
		case line := <-ch:
			f := strings.Split(line, ",")
			reason := ""
			if len(f) > 9 {
				reason = f[9]
			}
			reasons = append(reasons, reason)
		default:
			return reasons
		}
	}
}

func answerTTLs(msg []byte) []uint32 {
	off := 12
	skipName := func() {
		for off < len(msg) {
			l := int(msg[off])
			if l == 0 {
				off++
				return
			}
			if l&0xC0 == 0xC0 {
				off += 2
				return
			}
			off += 1 + l
		}
	}
	skipName()
	off += 4
	n := int(binary.BigEndian.Uint16(msg[6:8]))
	ttls := make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		skipName()
		if off+10 > len(msg) {
			break
		}
		ttls = append(ttls, binary.BigEndian.Uint32(msg[off+4:off+8]))
		off += 10 + int(binary.BigEndian.Uint16(msg[off+8:off+10]))
	}
	return ttls
}

func ipStrings(ips []net.IP) string {
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return strings.Join(out, ",")
}

func TestABannedAddressIsResetAtTheSyn(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		dst, l3 := "1.2.3.4", 20
		if ver == IPv6 {
			dst, l3 = "2001:db8::2", IPv6HeaderLen
		}
		t.Run(dst, func(t *testing.T) {
			set := banningAddressSet()
			w := newDualStackInjectingWorker(t, &set)
			w.bans = newTestIPBans(t, nil)
			banAddress(t, w.bans, dst, 443)
			sent := captureInjected(t, w)

			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			if v := w.ProcessPacket(bareSyn(ver)); v != engine.VerdictDrop {
				t.Fatalf("the SYN to a banned address must be dropped, got %v", v)
			}
			got := sent()
			if len(got) != 1 || resetsIn(got) != 1 {
				t.Fatalf("want one reset to the client, got %d packets", len(got))
			}
			if got[0][l3+13] != 0x14 || binary.BigEndian.Uint16(got[0][l3+2:l3+4]) != 12345 {
				t.Errorf("the reset must be a RST+ACK back to the client's port: flags %#x port %d", got[0][l3+13], binary.BigEndian.Uint16(got[0][l3+2:l3+4]))
			}
			if reasons := connectionReasons(ch); !slices.Contains(reasons, "ipblock-ban") {
				t.Errorf("the connection must be logged as ipblock-ban, got %v", reasons)
			}
		})
	}
}

func TestTheSynBanLeavesRoutedAndUnwatchedSetsAlone(t *testing.T) {
	cases := map[string]func(*config.SetConfig, *Worker) int{
		"routed through a proxy": func(s *config.SetConfig, _ *Worker) int {
			s.Routing.Enabled = true
			s.Routing.Mode = config.RoutingModeProxy
			return 443
		},
		"without the blocked-address cache": func(s *config.SetConfig, _ *Worker) int {
			s.TCP.IPBlockDetect.CacheBlockedIPs = false
			return 443
		},
		"without IP block detection": func(s *config.SetConfig, _ *Worker) int {
			s.TCP.IPBlockDetect.Enabled = false
			return 443
		},
		"with bans switched off": func(_ *config.SetConfig, w *Worker) int {
			w.bans.enabled.Store(false)
			return 443
		},
		"in a discovery pool": func(_ *config.SetConfig, w *Worker) int {
			w.getConfig().Queue.IsDiscovery = true
			return 443
		},
		"banned on another port": func(*config.SetConfig, *Worker) int { return 8443 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			set := banningAddressSet()
			w := newDualStackInjectingWorker(t, &set)
			w.bans = newTestIPBans(t, nil)
			banAddress(t, w.bans, "1.2.3.4", mutate(&set, w))
			sent := captureInjected(t, w)

			if v := w.ProcessPacket(bareSyn(IPv4)); v == engine.VerdictDrop {
				t.Errorf("the SYN must not be dropped, got %v", v)
			}
			if n := resetsIn(sent()); n != 0 {
				t.Errorf("no reset may be sent, got %d", n)
			}
		})
	}
}

func TestClientHelloResendsBanTheAddressWhenTheNeutralProbeIsSilent(t *testing.T) {
	set := newPassiveSet()
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	set.TCP.IPBlockDetect.RetransmitThreshold = 2
	w := newInjectingWorker(t, &set)
	probes := make(chan string, 4)
	w.bans = newTestIPBans(t, func(_ context.Context, ip net.IP, port, mark int) iphealth.TLSResult {
		probes <- iphealth.BanKey(ip, port)
		if mark != int(w.getConfig().MainInjectedMark()) {
			t.Errorf("the probe must carry the main injected mark, got %#x", mark)
		}
		return iphealth.TLSSilent
	})
	sent := captureInjected(t, w)

	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	if v := w.ProcessPacket(makeV4TCPPacket(hello, 1000)); v != engine.VerdictAccept {
		t.Fatalf("the first ClientHello passes, got %v", v)
	}
	if v := w.ProcessPacket(makeV4TCPPacket(hello, 1000)); v != engine.VerdictDrop {
		t.Fatalf("the resent ClientHello trips IP block detection, got %v", v)
	}
	select {
	case key := <-probes:
		if key != "1.2.3.4:443" {
			t.Fatalf("probed %s, want the blocked destination", key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the resend trigger never confirmed the address with the neutral-name probe")
	}
	w.bans.wg.Wait()

	if !w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
		t.Fatal("a silent neutral-name probe must ban the address")
	}
	ban := w.bans.reg.List()[0]
	if ban.Host != "i.ytimg.com" || ban.Set != set.Name || ban.Strikes != 1 || ban.Repeat {
		t.Errorf("ban record = %+v, want the triggering host and set", ban)
	}
	if got := resetsIn(sent()); got != 1 {
		t.Errorf("the trigger resets the client once, got %d resets", got)
	}

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)
	if v := w.ProcessPacket(makeV4TCPPacketFromPort(hello, 5000, 23456, 0x18)); v != engine.VerdictDrop {
		t.Fatalf("a ClientHello to the banned address on a new connection must be dropped, got %v", v)
	}
	if got := resetsIn(sent()); got != 1 {
		t.Errorf("the new connection gets an immediate reset, got %d", got)
	}
	if reasons := connectionReasons(ch); !slices.Contains(reasons, "ipblock-ban") {
		t.Errorf("the ClientHello must be logged as ipblock-ban, got %v", reasons)
	}
	select {
	case key := <-probes:
		t.Errorf("a ban already in place must not be probed again from the ClientHello path, probed %s", key)
	default:
	}
}

func TestTheShortCacheKeepsItsOwnReason(t *testing.T) {
	set := newPassiveSet()
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	w := newInjectingWorker(t, &set)
	w.bans = newTestIPBans(t, nil)
	w.destState.AddBlocked("1.2.3.4:443")
	sent := captureInjected(t, w)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)
	if v := w.ProcessPacket(makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000)); v != engine.VerdictDrop {
		t.Fatalf("an address in the short cache is reset at the ClientHello, got %v", v)
	}
	if resetsIn(sent()) != 1 {
		t.Error("want one reset")
	}
	reasons := connectionReasons(ch)
	if !slices.Contains(reasons, "ipblock-cached") || slices.Contains(reasons, "ipblock-ban") {
		t.Errorf("an address that is only in the short cache is logged as ipblock-cached, got %v", reasons)
	}
}

func TestAStrikeBansOnlyWhenTheNeutralProbeIsSilent(t *testing.T) {
	ip := net.ParseIP("74.125.172.167")
	for _, c := range []struct {
		res    iphealth.TLSResult
		banned bool
	}{
		{iphealth.TLSSilent, true},
		{iphealth.TLSReplied, false},
		{iphealth.TLSUnreachable, false},
	} {
		t.Run(c.res.String(), func(t *testing.T) {
			var probed atomic.Int32
			b := newTestIPBans(t, func(_ context.Context, got net.IP, port, _ int) iphealth.TLSResult {
				if !got.Equal(ip) || port != 443 {
					t.Errorf("probed %s:%d, want %s:443", got, port, ip)
				}
				probed.Add(1)
				return c.res
			})
			b.good.Remember(bannedHost, ip)
			var published atomic.Int32
			b.changed = func() { published.Add(1) }
			cfg := config.NewConfig()

			b.confirm(&cfg, ip, 443, bannedHost, bannedSet)
			b.wg.Wait()

			if probed.Load() != 1 {
				t.Fatalf("want one probe, got %d", probed.Load())
			}
			if got := b.reg.Banned(ip, 443); got != c.banned {
				t.Fatalf("banned = %v, want %v", got, c.banned)
			}
			if got := published.Load() == 1; got != c.banned {
				t.Errorf("list published %d times, a new ban publishes once and nothing else does", published.Load())
			}
			known := slices.ContainsFunc(b.good.Lookup(bannedHost, false), ip.Equal)
			if known == c.banned {
				t.Errorf("known-good for the host = %v; a banned address must be forgotten, any other kept", known)
			}
			if !c.banned {
				return
			}
			ban := b.reg.List()[0]
			if ban.Host != bannedHost || ban.Set != bannedSet || ban.Repeat {
				t.Errorf("ban record = %+v", ban)
			}
			if d := time.Until(ban.Until); d < 23*time.Hour || d > 24*time.Hour {
				t.Errorf("a first ban lasts system.ip_health.ban_ttl_sec (24h by default), ends in %s", d)
			}
		})
	}
}

func TestStrikeConfirmationsAreDedupedPerAddress(t *testing.T) {
	release := make(chan struct{})
	var probed atomic.Int32
	b := newTestIPBans(t, func(ctx context.Context, _ net.IP, _, _ int) iphealth.TLSResult {
		probed.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return iphealth.TLSSilent
	})
	cfg := config.NewConfig()
	ip := net.ParseIP("74.125.172.167")
	for i := 0; i < 5; i++ {
		b.confirm(&cfg, ip, 443, bannedHost, bannedSet)
	}
	b.confirm(&cfg, ip, 80, bannedHost, bannedSet)
	close(release)
	b.wg.Wait()

	if got := probed.Load(); got != 2 {
		t.Fatalf("one probe per address and port while it runs, got %d", got)
	}
	if !b.reg.Banned(ip, 443) || !b.reg.Banned(ip, 80) {
		t.Error("both addresses are banned after their probes")
	}

	b.applyConfig(&config.Config{})
	cfg.System.IPHealth.BansOff = true
	b.applyConfig(&cfg)
	b.confirm(&cfg, net.ParseIP("64.233.161.198"), 443, "yt3.ggpht.com", bannedSet)
	b.wg.Wait()
	if probed.Load() != 2 {
		t.Error("with bans off no confirmation probe runs")
	}
}

func TestRetestsLiftABanAfterThreeAnsweredChecks(t *testing.T) {
	b := newTestIPBans(t, func(context.Context, net.IP, int, int) iphealth.TLSResult { return iphealth.TLSReplied })
	ip := net.ParseIP("74.125.172.167")
	banAddress(t, b, "74.125.172.167", 443)
	var published atomic.Int32
	b.changed = func() { published.Add(1) }

	for i := 0; i < 3; i++ {
		if !b.reg.Banned(ip, 443) {
			t.Fatalf("the ban was lifted after %d answered checks, want 3", i)
		}
		b.retest(0, 0)
		b.wg.Wait()
	}
	if b.reg.Banned(ip, 443) {
		t.Fatal("three answered re-tests in a row must lift the ban")
	}
	if published.Load() != 1 {
		t.Errorf("the lift publishes the list once, published %d times", published.Load())
	}
}

func TestRetestsAreBoundedAndFailuresKeepTheBan(t *testing.T) {
	release := make(chan struct{})
	var running, peak atomic.Int32
	b := newTestIPBans(t, func(ctx context.Context, _ net.IP, _, _ int) iphealth.TLSResult {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		running.Add(-1)
		return iphealth.TLSSilent
	})
	for i := 0; i < 12; i++ {
		banAddress(t, b, fmt.Sprintf("10.0.0.%d", i+1), 443)
	}

	b.retest(0, 0)
	b.mu.Lock()
	started := len(b.inflight)
	b.mu.Unlock()
	if started != banRetestsPerTick {
		t.Fatalf("re-tests started in one tick = %d, want %d", started, banRetestsPerTick)
	}
	if !waitFor(t, func() bool { return running.Load() == banProbeParallel }) {
		t.Fatalf("want %d probes running at once, have %d", banProbeParallel, running.Load())
	}
	b.retest(0, 0)
	b.mu.Lock()
	started = len(b.inflight)
	b.mu.Unlock()
	if started > banRetestsPerTick+4 {
		t.Fatalf("a second tick must not start the running re-tests twice, %d in flight", started)
	}
	close(release)
	b.wg.Wait()

	if p := peak.Load(); p > banProbeParallel {
		t.Errorf("at most %d probes may run at once, saw %d", banProbeParallel, p)
	}
	for i := 0; i < 12; i++ {
		if !b.reg.Banned(net.ParseIP(fmt.Sprintf("10.0.0.%d", i+1)), 443) {
			t.Errorf("a failed re-test must keep the ban on 10.0.0.%d", i+1)
		}
	}
}

func TestStopCancelsProbesAndSavesTheBans(t *testing.T) {
	path := filepath.Join(t.TempDir(), iphealth.BanFileName)
	started := make(chan struct{}, 1)
	b := newIPBans(iphealth.NewBanRegistry(path), iphealth.NewKnownGood(), func(ctx context.Context, _ net.IP, _, _ int) iphealth.TLSResult {
		started <- struct{}{}
		<-ctx.Done()
		return iphealth.TLSSilent
	})
	b.enabled.Store(true)
	banAddress(t, b, "74.125.172.167", 443)
	cfg := config.NewConfig()
	b.confirm(&cfg, net.ParseIP("64.233.161.198"), 443, "yt3.ggpht.com", bannedSet)
	<-started

	done := make(chan struct{})
	go func() {
		b.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop must cancel an in-flight probe instead of waiting for it")
	}
	if b.reg.Banned(net.ParseIP("64.233.161.198"), 443) {
		t.Error("a probe cut short by stop must not ban")
	}

	loaded := iphealth.NewBanRegistry(path)
	if err := loaded.Load(); err != nil || !loaded.Banned(net.ParseIP("74.125.172.167"), 443) {
		t.Fatalf("stop must save the bans: err %v, banned %v", err, loaded.Banned(net.ParseIP("74.125.172.167"), 443))
	}
	b.confirm(&cfg, net.ParseIP("64.233.161.199"), 443, "yt3.ggpht.com", bannedSet)
	b.wg.Wait()
	select {
	case <-started:
		t.Error("no probe may start after stop")
	default:
	}
}

func TestApplyConfigFollowsTheSwitchAndSkipsDiscovery(t *testing.T) {
	b := newTestIPBans(t, nil)
	cfg := config.NewConfig()
	b.applyConfig(&cfg)
	if !b.on() {
		t.Error("bans are on by default")
	}
	cfg.System.IPHealth.BansOff = true
	b.applyConfig(&cfg)
	if b.on() {
		t.Error("bans_off turns them off")
	}
	cfg.System.IPHealth.BansOff = false
	cfg.Queue.IsDiscovery = true
	b.applyConfig(&cfg)
	if b.on() {
		t.Error("a discovery pool never enforces bans")
	}
	b.applyConfig(nil)
	if b.on() {
		t.Error("no config, no bans")
	}
	var none *ipBans
	none.applyConfig(&cfg)
	if none.on() || none.banned(net.ParseIP("1.2.3.4"), 443) || none.bannedIP(net.ParseIP("1.2.3.4")) || none.bannedKey("1.2.3.4:443") {
		t.Error("a pool without a registry bans nothing")
	}
	none.confirm(&cfg, net.ParseIP("1.2.3.4"), 443, "", "")
	none.tick(&cfg)
	none.stop()
}

func TestHealDropsBannedAddressesWithoutHealDNS(t *testing.T) {
	w := healWorker(t, "3.3.3.3")
	w.bans = newTestIPBans(t, nil)
	banAddress(t, w.bans, "2.2.2.2", 443)
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2", "3.3.3.3")

	healed := w.healDNSResponse(&config.Config{}, healSetWith(false), "example.com", resp, false)
	if got := ipStrings(dns.ParseResponseIPs(healed)); got != "1.1.1.1,3.3.3.3" {
		t.Fatalf("without heal_dns a banned address is still dropped and an unreachable one kept, got %q", got)
	}
	for _, ttl := range answerTTLs(healed) {
		if ttl > config.DefaultIPBlockHealTTLSec {
			t.Errorf("a rewritten answer keeps the heal TTL cap, got %d", ttl)
		}
	}

	healed = w.healDNSResponse(&config.Config{}, healSetWith(true), "example.com", resp, false)
	if got := ipStrings(dns.ParseResponseIPs(healed)); got != "1.1.1.1" {
		t.Fatalf("with heal_dns both the banned and the unreachable address go, got %q", got)
	}

	plain := config.NewSetConfig()
	plain.Name = "plain"
	healed = w.healDNSResponse(&config.Config{}, &plain, "example.com", resp, false)
	if got := ipStrings(dns.ParseResponseIPs(healed)); got != "1.1.1.1,3.3.3.3" {
		t.Fatalf("a set without IP block detection still drops banned addresses, got %q", got)
	}

	discovery := &config.Config{}
	discovery.Queue.IsDiscovery = true
	if healed := w.healDNSResponse(discovery, healSetWith(false), "example.com", resp, false); healed != nil {
		t.Error("a discovery run must not rewrite DNS answers")
	}
	if healed := w.healDNSResponse(&config.Config{}, nil, "example.com", resp, false); healed != nil {
		t.Error("an answer for a name no set matches is left alone")
	}
	w.bans.enabled.Store(false)
	if healed := w.healDNSResponse(&config.Config{}, healSetWith(false), "example.com", resp, false); healed != nil {
		t.Error("with bans off the answer is left alone")
	}
}

func TestHealFallbackNeverHandsOutABannedAddress(t *testing.T) {
	w := healWorker(t)
	w.bans = newTestIPBans(t, nil)
	w.goodIPs.Remember("example.com", net.ParseIP("4.4.4.4"))
	w.goodIPs.Remember("example.com", net.ParseIP("3.3.3.3"))
	for _, ip := range []string{"1.1.1.1", "2.2.2.2", "4.4.4.4"} {
		banAddress(t, w.bans, ip, 443)
	}
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	healed := w.healDNSResponse(&config.Config{}, healSetWith(false), "example.com", resp, false)
	if got := ipStrings(dns.ParseResponseIPs(healed)); got != "3.3.3.3" {
		t.Fatalf("every answer address is banned: want the known-good one that is not, got %q", got)
	}

	banAddress(t, w.bans, "3.3.3.3", 443)
	if healed := w.healDNSResponse(&config.Config{}, healSetWith(false), "example.com", resp, false); healed != nil {
		t.Errorf("with no usable known-good address the answer passes through unchanged, got %v", dns.ParseResponseIPs(healed))
	}
}

func TestPinnedAnswerSkipsBannedPins(t *testing.T) {
	w := healWorker(t)
	w.bans = newTestIPBans(t, nil)
	banAddress(t, w.bans, "157.240.0.174", 443)
	query := dns.BuildQuery("www.instagram.com", 0x1234, 1)

	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174", "157.240.253.174"}})
	if got := ipStrings(dns.ParseResponseIPs(w.pinnedAnswer(set, query, "www.instagram.com"))); got != "157.240.253.174" {
		t.Fatalf("a banned pin is left out like an unreachable one, got %q", got)
	}
	only := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})
	if got := w.pinnedAnswer(only, query, "www.instagram.com"); got != nil {
		t.Error("when every pin is banned the query passes through to the resolver")
	}
	w.bans.enabled.Store(false)
	if got := ipStrings(dns.ParseResponseIPs(w.pinnedAnswer(set, query, "www.instagram.com"))); got != "157.240.0.174,157.240.253.174" {
		t.Errorf("with bans off every pin is answered, got %q", got)
	}
}

func TestRecordDestAliveNeverRemembersABannedAddress(t *testing.T) {
	w := healWorker(t)
	w.bans = newTestIPBans(t, nil)
	client := "192.168.1.10"
	w.hostHints.Store(client, "185.199.110.133", "github", "raw.githubusercontent.com")
	w.hostHints.Store(client, "185.199.111.133", "github", "raw.githubusercontent.com")
	banAddress(t, w.bans, "185.199.110.133", 443)

	w.recordDestAlive("185.199.110.133", client, true)
	w.recordDestAlive("185.199.111.133", client, true)

	if got := ipStrings(w.goodIPs.Lookup("raw.githubusercontent.com", false)); got != "185.199.111.133" {
		t.Fatalf("a SYN-ACK from a banned address must not make it known-good, known: %q", got)
	}
}

func TestIPBlockCacheReportsBannedAddresses(t *testing.T) {
	p := &Pool{state: newRuntimeState()}
	p.state.bans = newTestIPBans(t, nil)
	banAddress(t, p.state.bans, "74.125.172.167", 443)
	banAddress(t, p.state.bans, "2a00:1450:4010:c05::c6", 443)
	cache := p.GetIPBlockCache()

	for dest, want := range map[string]bool{
		"74.125.172.167:443":           true,
		"74.125.172.167:80":            false,
		"[2a00:1450:4010:c05::c6]:443": true,
		bannedHost + ":443":            false,
	} {
		if got := cache.IsBlocked(dest); got != want {
			t.Errorf("IsBlocked(%s) = %v, want %v", dest, got, want)
		}
	}
	cache.AddBlocked("1.1.1.1:443")
	if !cache.IsBlocked("1.1.1.1:443") {
		t.Error("the short cache is still reported")
	}
	p.state.bans.enabled.Store(false)
	if cache.IsBlocked("74.125.172.167:443") {
		t.Error("with bans off a banned address is not reported")
	}
	if (&Pool{state: newRuntimeState()}).GetIPBlockCache().IsBlocked("74.125.172.167:443") {
		t.Error("a pool without bans reports only its short cache")
	}
}

func TestPoolListsAndClearsBans(t *testing.T) {
	p := &Pool{state: newRuntimeState()}
	p.state.bans = newTestIPBans(t, nil)
	banAddress(t, p.state.bans, "74.125.172.167", 443)
	banAddress(t, p.state.bans, "2a00:1450:4010:c05::c6", 443)
	p.state.destState.AddBlocked("74.125.172.167:443")

	bans := p.GetBans()
	if len(bans) != 2 {
		t.Fatalf("GetBans = %+v, want 2 entries", bans)
	}
	for _, b := range bans {
		if b.Host != bannedHost || b.Set != bannedSet || b.Strikes != 1 || b.Port != 443 || b.Since <= 0 || b.Until <= b.Since {
			t.Errorf("entry %+v", b)
		}
	}
	if !slices.ContainsFunc(bans, func(b metrics.BanEntry) bool {
		return b.Key == "[2a00:1450:4010:c05::c6]:443" && b.IP == "2a00:1450:4010:c05::c6"
	}) {
		t.Errorf("IPv6 bans are keyed with brackets: %+v", bans)
	}

	if n := p.ClearBans("74.125.172.167:443"); n != 1 {
		t.Fatalf("unbanning one address cleared %d", n)
	}
	if p.state.destState.IsBlocked("74.125.172.167:443") {
		t.Error("an unban also drops the address from the short cache, so it works at once")
	}
	if got := p.GetBans(); len(got) != 1 {
		t.Fatalf("one ban left, got %+v", got)
	}
	if n := p.ClearBans(""); n != 1 || len(p.GetBans()) != 0 {
		t.Fatalf("clearing all removed %d, left %+v", n, p.GetBans())
	}
	if n := p.ClearBans(""); n != 0 {
		t.Errorf("nothing left to clear, got %d", n)
	}

	banAddress(t, p.state.bans, "74.125.172.167", 443)
	p.state.bans.enabled.Store(false)
	if got := p.GetBans(); got == nil || len(got) != 0 {
		t.Errorf("with bans off the list is empty, got %+v", got)
	}
	if n := p.ClearBans(""); n != 1 {
		t.Errorf("bans can be cleared while switched off, cleared %d", n)
	}

	empty := &Pool{}
	if got := empty.GetBans(); got == nil || len(got) != 0 || empty.ClearBans("") != 0 {
		t.Error("a pool without state lists and clears nothing")
	}
}
