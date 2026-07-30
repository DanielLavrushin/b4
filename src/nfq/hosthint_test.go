package nfq

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

func encodeDNSName(name string) []byte {
	var out []byte
	for _, label := range strings.Split(name, ".") {
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	return append(out, 0)
}

func buildDNSResponse(txid uint16, domain string, ips []net.IP) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], txid)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], uint16(len(ips)))

	msg = append(msg, encodeDNSName(domain)...)
	msg = append(msg, 0x00, 0x01, 0x00, 0x01)

	for _, ip := range ips {
		msg = append(msg, 0xC0, 0x0C)
		msg = append(msg, 0x00, 0x01, 0x00, 0x01)
		msg = append(msg, 0x00, 0x00, 0x00, 0x3C)
		msg = append(msg, 0x00, 0x04)
		msg = append(msg, ip.To4()...)
	}
	return msg
}

func makeV4UDPPacket(payload []byte, srcIP, dstIP net.IP, sport, dport uint16) []byte {
	const ipHL = 20
	pkt := make([]byte, ipHL+8+len(payload))

	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[8] = 64
	pkt[9] = 17
	copy(pkt[12:16], srcIP.To4())
	copy(pkt[16:20], dstIP.To4())

	binary.BigEndian.PutUint16(pkt[ipHL:ipHL+2], sport)
	binary.BigEndian.PutUint16(pkt[ipHL+2:ipHL+4], dport)
	binary.BigEndian.PutUint16(pkt[ipHL+4:ipHL+6], uint16(8+len(payload)))
	copy(pkt[ipHL+8:], payload)
	return pkt
}

func makeV4TCPPacketFlags(payload []byte, seq uint32, flags byte) []byte {
	pkt := makeV4TCPPacket(payload, seq)
	pkt[20+13] = flags
	return pkt
}

func newHintSet() config.SetConfig {
	set := newPassiveSet()
	set.Targets.DomainOnly = false
	return set
}

func TestHostHintStoreAndLookup(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "video", "rr1.googlevideo.com")

	setId, host, ok := c.Lookup("10.0.0.1", "1.2.3.4")
	if !ok {
		t.Fatal("stored hint should be found")
	}
	if setId != "video" || host != "rr1.googlevideo.com" {
		t.Fatalf("hint: got setId=%q host=%q", setId, host)
	}
}

func TestHostHintIsSourceScoped(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "video", "rr1.googlevideo.com")

	if _, _, ok := c.Lookup("10.0.0.2", "1.2.3.4"); ok {
		t.Fatal("another client must not see the first client's hint")
	}
	if _, _, ok := c.Lookup("10.0.0.1", "5.6.7.8"); ok {
		t.Fatal("another destination must not reuse the hint")
	}
}

func TestHostHintSameSetSeveralHosts(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "yt", "i.ytimg.com")
	c.Store("10.0.0.1", "1.2.3.4", "yt", "s.ytimg.com")

	setId, _, ok := c.Lookup("10.0.0.1", "1.2.3.4")
	if !ok || setId != "yt" {
		t.Fatalf("hostnames agreeing on one set should resolve, got setId=%q ok=%v", setId, ok)
	}
}

func TestHostHintAmbiguousBetweenSets(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "images", "i.ytimg.com")
	c.Store("10.0.0.1", "1.2.3.4", "video", "rr1.googlevideo.com")

	if setId, _, ok := c.Lookup("10.0.0.1", "1.2.3.4"); ok {
		t.Fatalf("hostnames pointing at different sets must not resolve, got %q", setId)
	}
}

func TestHostHintRepeatedStoreIsNotAmbiguous(t *testing.T) {
	c := newHostHintCache()
	for i := 0; i < 10; i++ {
		c.Store("10.0.0.1", "1.2.3.4", "yt", "i.ytimg.com")
	}

	if _, _, ok := c.Lookup("10.0.0.1", "1.2.3.4"); !ok {
		t.Fatal("repeating the same evidence must stay resolvable")
	}
}

func TestHostHintExpiry(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "yt", "i.ytimg.com")

	c.mu.Lock()
	entry := c.keys[hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}]
	entry.candidates[0].expires = time.Now().Add(-time.Second)
	c.mu.Unlock()

	if _, _, ok := c.Lookup("10.0.0.1", "1.2.3.4"); ok {
		t.Fatal("expired hint must not resolve")
	}
	if c.Len() != 1 {
		t.Fatalf("Lookup must not mutate the cache, entries=%d", c.Len())
	}

	c.Cleanup()
	if c.Len() != 0 {
		t.Fatalf("Cleanup should drop the expired key, entries=%d", c.Len())
	}
}

func TestHostHintCleanup(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "yt", "i.ytimg.com")
	c.Store("10.0.0.2", "1.2.3.4", "yt", "i.ytimg.com")

	c.mu.Lock()
	for _, entry := range c.keys {
		entry.candidates[0].expires = time.Now().Add(-time.Second)
	}
	c.mu.Unlock()

	c.Cleanup()
	if c.Len() != 0 {
		t.Fatalf("Cleanup should drop expired keys, entries=%d", c.Len())
	}
}

func TestHostHintCandidateCap(t *testing.T) {
	c := newHostHintCache()
	for i := 0; i < maxHostHintCandidates*3; i++ {
		c.Store("10.0.0.1", "1.2.3.4", "yt", fmt.Sprintf("host%d.ytimg.com", i))
	}

	c.mu.Lock()
	got := len(c.keys[hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}].candidates)
	c.mu.Unlock()

	if got > maxHostHintCandidates {
		t.Fatalf("candidate cap exceeded: %d", got)
	}
}

func TestHostHintEntryCap(t *testing.T) {
	c := newHostHintCache()
	for i := 0; i < maxHostHintEntries+128; i++ {
		c.Store(fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff), "1.2.3.4", "yt", "i.ytimg.com")
	}
	if c.Len() > maxHostHintEntries {
		t.Fatalf("entry cap exceeded: %d", c.Len())
	}
}

func TestHostHintNilReceiver(t *testing.T) {
	var c *hostHintCache
	c.Store("10.0.0.1", "1.2.3.4", "yt", "i.ytimg.com")
	if _, _, ok := c.Lookup("10.0.0.1", "1.2.3.4"); ok {
		t.Fatal("nil cache must not resolve")
	}
	c.Cleanup()
	if c.Len() != 0 {
		t.Fatal("nil cache must report zero entries")
	}
}

func TestHostHintConcurrentReadersAndWriters(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "yt", "i.ytimg.com")

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				c.Store(fmt.Sprintf("10.0.%d.%d", id, i&0xff), "1.2.3.4", "yt", "i.ytimg.com")
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				c.Lookup("10.0.0.1", "1.2.3.4")
				c.Lookup("10.0.0.1", "9.9.9.9")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			c.Cleanup()
		}
	}()
	wg.Wait()

	if _, _, ok := c.Lookup("10.0.0.1", "1.2.3.4"); !ok {
		t.Fatal("the original hint should have survived concurrent access")
	}
}

func BenchmarkHostHintLookupMiss(b *testing.B) {
	c := newHostHintCache()
	c.Store("10.0.0.9", "9.9.9.9", "yt", "i.ytimg.com")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Lookup("10.0.0.1", "1.2.3.4")
	}
}

func BenchmarkHostHintLookupMissIPv6(b *testing.B) {
	c := newHostHintCache()
	c.Store("2001:4860:4860::8888", "2606:4700:4700::1111", "yt", "i.ytimg.com")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Lookup("2001:db8:85a3::8a2e:370:7334", "2606:4700:4700::1001")
	}
}

func BenchmarkHostHintLookupMissParallel(b *testing.B) {
	c := newHostHintCache()
	c.Store("10.0.0.9", "9.9.9.9", "yt", "i.ytimg.com")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Lookup("10.0.0.1", "1.2.3.4")
		}
	})
}

func TestLookupHostHintRefusesDomainOnlySet(t *testing.T) {
	set := newHintSet()
	set.Targets.DomainOnly = true
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)
	w.hostHints.Store("10.0.0.1", "1.2.3.4", set.Id, "i.ytimg.com")

	if got, _ := w.lookupHostHint(&cfg, "10.0.0.1", "1.2.3.4", ""); got != nil {
		t.Fatalf("a domain-only set must not be selected from a DNS hint, got %q", got.Name)
	}
}

func TestLookupHostHintRefusesDisabledSet(t *testing.T) {
	set := newHintSet()
	set.Enabled = false
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)
	w.hostHints.Store("10.0.0.1", "1.2.3.4", set.Id, "i.ytimg.com")

	if got, _ := w.lookupHostHint(&cfg, "10.0.0.1", "1.2.3.4", ""); got != nil {
		t.Fatalf("a disabled set must not be selected, got %q", got.Name)
	}
}

func TestLookupHostHintRefusesStaleSetId(t *testing.T) {
	set := newHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)
	w.hostHints.Store("10.0.0.1", "1.2.3.4", "set-removed-by-a-config-reload", "i.ytimg.com")

	if got, _ := w.lookupHostHint(&cfg, "10.0.0.1", "1.2.3.4", ""); got != nil {
		t.Fatalf("a hint naming a set that no longer exists must not resolve, got %q", got.Name)
	}
}

func TestLookupHostHintChecksSourceDevices(t *testing.T) {
	set := newHintSet()
	set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)
	w.hostHints.Store("10.0.0.1", "1.2.3.4", set.Id, "i.ytimg.com")

	if got, _ := w.lookupHostHint(&cfg, "10.0.0.1", "1.2.3.4", "11:22:33:44:55:66"); got != nil {
		t.Fatalf("a set restricted to other devices must not be selected, got %q", got.Name)
	}
	if got, _ := w.lookupHostHint(&cfg, "10.0.0.1", "1.2.3.4", "aa:bb:cc:dd:ee:ff"); got == nil {
		t.Fatal("the permitted device should be selected")
	}
}

func TestDNSResponseFeedsHostHint(t *testing.T) {
	set := newHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	resp := buildDNSResponse(0x1234, "i.ytimg.com", []net.IP{net.ParseIP("1.2.3.4")})
	pkt := makeV4UDPPacket(resp, net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1"), 53, 5353)
	w.ProcessPacket(pkt)

	setId, host, ok := w.hostHints.Lookup("10.0.0.1", "1.2.3.4")
	if !ok {
		t.Fatal("a DNS answer for a matched domain should leave a hint for the client")
	}
	if setId != set.Id || host != "i.ytimg.com" {
		t.Fatalf("hint: got setId=%q host=%q", setId, host)
	}
}

func TestFirstFlowClassifiesFromDNSHint(t *testing.T) {
	set := newHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	resp := buildDNSResponse(0x1234, "i.ytimg.com", []net.IP{net.ParseIP("1.2.3.4")})
	w.ProcessPacket(makeV4UDPPacket(resp, net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1"), 53, 5353))

	w.ProcessPacket(makeV4TCPPacket([]byte("GET / HTTP/1.1\r\n\r\n"), 1000))

	bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443)
	if bound == nil {
		t.Fatal("the first flow to a resolved address should inherit the set from the DNS hint")
	}
	if bound.Id != set.Id {
		t.Fatalf("bound set: want %q, got %q", set.Id, bound.Id)
	}
}

func TestClearSNIForAnotherDomainCancelsHint(t *testing.T) {
	set := newHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	resp := buildDNSResponse(0x1234, "i.ytimg.com", []net.IP{net.ParseIP("1.2.3.4")})
	w.ProcessPacket(makeV4UDPPacket(resp, net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1"), 53, 5353))

	unrelated := buildClientHello("maps.example.org", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(unrelated, 1000))

	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
		t.Fatalf("a clear SNI matching no set must cancel the hint, got %q", bound.Name)
	}
}

func TestClearSNIForHintedDomainKeepsSet(t *testing.T) {
	set := newHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	resp := buildDNSResponse(0x1234, "i.ytimg.com", []net.IP{net.ParseIP("1.2.3.4")})
	w.ProcessPacket(makeV4UDPPacket(resp, net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1"), 53, 5353))

	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))

	bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443)
	if bound == nil || bound.Id != set.Id {
		t.Fatal("a clear SNI matching the set must keep it bound")
	}
}

func TestFirstFlowUnclassifiedWithoutHint(t *testing.T) {
	set := newHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)
	w.ProcessPacket(makeV4TCPPacket([]byte("GET / HTTP/1.1\r\n\r\n"), 1000))

	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
		t.Fatalf("without DNS evidence the flow must stay unclassified, got %q", bound.Name)
	}
}

func TestNeedsPayloadlessInjection(t *testing.T) {
	set := config.NewSetConfig()
	set.TCP.DropSACK = false
	if needsPayloadlessInjection(&set) {
		t.Fatal("a set without SACK stripping has no work to do on a payload-less packet")
	}

	set.TCP.DropSACK = true
	if !needsPayloadlessInjection(&set) {
		t.Fatal("SACK stripping must still process payload-less packets")
	}

	if needsPayloadlessInjection(nil) {
		t.Fatal("nil set must not request injection")
	}
}

func TestCleanSynIsAcceptedWithoutSynTechnique(t *testing.T) {
	set := config.NewSetConfig()
	set.Id = "yt-video"
	set.Name = "YT video"
	set.Enabled = true
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.TCP.SynFake = false
	set.Faking.TCPMD5 = false
	set.TCP.DropSACK = false

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	if needsTCPSynInjection(&set) {
		t.Fatal("fixture should not request an explicit SYN technique")
	}
	if !needsTCPInjection(&set) {
		t.Fatal("fixture should request payload injection so the clean-SYN path is reached")
	}

	syn := makeV4TCPPacketFlags(nil, 1000, 0x02)
	if v := w.ProcessPacket(syn); v != engine.VerdictAccept {
		t.Fatalf("a clean SYN must pass through untouched, got %v", v)
	}

	fin := makeV4TCPPacketFlags(nil, 2000, 0x11)
	if v := w.ProcessPacket(fin); v != engine.VerdictAccept {
		t.Fatalf("a payload-less FIN must pass through untouched, got %v", v)
	}
}
