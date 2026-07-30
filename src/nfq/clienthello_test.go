package nfq

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sni"
)

func newPassiveSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "yt-images"
	set.Name = "YT images"
	set.Enabled = true
	set.Targets.DomainOnly = true
	set.Targets.DomainsToMatch = []string{"ytimg.com"}
	set.TCP.RSTProtection.Enabled = true
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	set.Faking.SNI = false
	set.Faking.SNIMutation.Mode = config.ConfigOff
	set.TCP.Desync.Mode = config.ConfigOff
	set.TCP.Win.Mode = config.ConfigOff
	set.TCP.DropSACK = false
	return set
}

func newTestWorker(t *testing.T, cfg *config.Config) *Worker {
	t.Helper()
	cfg.ConfigPath = t.TempDir() + "/b4.json"
	w := NewWorkerWithQueue(cfg, 0)
	w.matcher.Store(buildMatcher(cfg))
	w.ipToMac.Store(make(map[string]string))
	state := newRuntimeState()
	w.tlsCache = state.tlsCache
	w.connTracker = state.connState
	w.destState = state.destState
	w.pendingHello = state.pendingHello
	w.hostHints = state.hostHints
	return w
}

func buildExtension(extType uint16, data []byte) []byte {
	out := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(out[0:2], extType)
	binary.BigEndian.PutUint16(out[2:4], uint16(len(data)))
	copy(out[4:], data)
	return out
}

func buildSNIExtension(host string) []byte {
	data := make([]byte, 5+len(host))
	binary.BigEndian.PutUint16(data[0:2], uint16(3+len(host)))
	data[2] = 0
	binary.BigEndian.PutUint16(data[3:5], uint16(len(host)))
	copy(data[5:], host)
	return buildExtension(0, data)
}

func buildClientHello(host string, padBefore int, padFiller byte) []byte {
	pad := bytes.Repeat([]byte{padFiller}, padBefore)

	var exts []byte
	exts = append(exts, buildExtension(21, pad)...)
	exts = append(exts, buildSNIExtension(host)...)
	exts = append(exts, buildExtension(43, []byte{0x02, 0x03, 0x04})...)

	body := make([]byte, 0, 64+len(exts))
	body = append(body, 0x03, 0x03)
	body = append(body, bytes.Repeat([]byte{0x11}, 32)...)
	body = append(body, 32)
	body = append(body, bytes.Repeat([]byte{0x22}, 32)...)
	body = append(body, 0x00, 0x02, 0x13, 0x01)
	body = append(body, 0x01, 0x00)
	extLen := make([]byte, 2)
	binary.BigEndian.PutUint16(extLen, uint16(len(exts)))
	body = append(body, extLen...)
	body = append(body, exts...)

	handshake := make([]byte, 4+len(body))
	handshake[0] = 0x01
	handshake[1] = byte(len(body) >> 16)
	handshake[2] = byte(len(body) >> 8)
	handshake[3] = byte(len(body))
	copy(handshake[4:], body)

	record := make([]byte, 5+len(handshake))
	record[0] = 0x16
	record[1] = 0x03
	record[2] = 0x01
	binary.BigEndian.PutUint16(record[3:5], uint16(len(handshake)))
	copy(record[5:], handshake)

	return record
}

func TestTruncatedClientHello(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)

	if truncatedClientHello(full) {
		t.Fatal("complete ClientHello must not be reported as truncated")
	}
	if !truncatedClientHello(full[:1396]) {
		t.Fatal("first segment of a split ClientHello must be reported as truncated")
	}
	if truncatedClientHello(full[1396:]) {
		t.Fatal("continuation segment must not be reported as truncated")
	}
	if truncatedClientHello([]byte{0x16, 0x03, 0x01, 0x00, 0x10, 0x02}) {
		t.Fatal("non-ClientHello handshake record must not be buffered")
	}
	if truncatedClientHello([]byte{0x17, 0x03, 0x03, 0x00, 0x10, 0x01}) {
		t.Fatal("application data record must not be buffered")
	}
}

func TestSplitClientHelloIsNotClassifiableAlone(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	if len(full) <= 1396 {
		t.Fatalf("fixture too small to split: %d bytes", len(full))
	}

	if host, _, _ := sni.ParseTLSClientHelloSNI(full); host != "i.ytimg.com" {
		t.Fatalf("complete ClientHello: want SNI i.ytimg.com, got %q", host)
	}
	if host, _, _ := sni.ParseTLSClientHelloSNI(full[:1396]); host != "" {
		t.Fatalf("first segment should not yield an SNI, got %q", host)
	}
}

func TestPendingHelloCacheJoinsSplitClientHello(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	seg1, seg2 := full[:1396], full[1396:]
	const baseSeq = uint32(5000)

	c := newPendingHelloCache()

	if _, _, ok := c.Feed("flow", baseSeq, seg1); ok {
		t.Fatal("first segment must not report a join")
	}
	if c.Len() != 1 {
		t.Fatalf("first segment should be buffered, entries=%d", c.Len())
	}

	joined, prefix, ok := c.Feed("flow", baseSeq+uint32(len(seg1)), seg2)
	if !ok {
		t.Fatal("continuation segment should join the buffered prefix")
	}
	if prefix != len(seg1) {
		t.Fatalf("prefix length: want %d, got %d", len(seg1), prefix)
	}
	if !bytes.Equal(joined, full) {
		t.Fatalf("joined payload differs from original (%d vs %d bytes)", len(joined), len(full))
	}

	host, tlsVersion, _ := sni.ParseTLSClientHelloSNI(joined)
	if host != "i.ytimg.com" {
		t.Fatalf("recovered SNI: want i.ytimg.com, got %q", host)
	}
	if tlsVersion != 0x0304 {
		t.Fatalf("recovered TLS version: want 0x0304, got 0x%04x", tlsVersion)
	}

	c.Drop("flow")
	if c.Len() != 0 {
		t.Fatalf("Drop should release the flow, entries=%d", c.Len())
	}
}

func TestPendingHelloCacheThreeSegments(t *testing.T) {
	full := buildClientHello("youtubei.googleapis.com", 2900, 0xCD)
	if len(full) <= 2800 {
		t.Fatalf("fixture too small: %d bytes", len(full))
	}
	const baseSeq = uint32(77)

	c := newPendingHelloCache()
	c.Feed("flow", baseSeq, full[:1400])
	if _, _, ok := c.Feed("flow", baseSeq+1400, full[1400:2800]); !ok {
		t.Fatal("second segment should join")
	}

	joined, _, ok := c.Feed("flow", baseSeq+2800, full[2800:])
	if !ok {
		t.Fatal("third segment should join")
	}
	if !bytes.Equal(joined, full) {
		t.Fatalf("three-way join mismatch (%d vs %d bytes)", len(joined), len(full))
	}
	if host, _, _ := sni.ParseTLSClientHelloSNI(joined); host != "youtubei.googleapis.com" {
		t.Fatalf("recovered SNI: got %q", host)
	}
}

func TestPendingHelloCacheRetransmit(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	seg1 := full[:1396]
	const baseSeq = uint32(9000)

	c := newPendingHelloCache()
	c.Feed("flow", baseSeq, seg1)

	if _, _, ok := c.Feed("flow", baseSeq, seg1); ok {
		t.Fatal("retransmitted first segment must not produce a join")
	}
	if c.Len() != 1 {
		t.Fatalf("retransmit must keep the buffered prefix, entries=%d", c.Len())
	}

	joined, _, ok := c.Feed("flow", baseSeq+uint32(len(seg1)), full[1396:])
	if !ok || !bytes.Equal(joined, full) {
		t.Fatal("prefix should still join after a retransmit")
	}
}

func TestPendingHelloCacheOverlap(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	const baseSeq = uint32(400)
	const overlap = 200

	c := newPendingHelloCache()
	c.Feed("flow", baseSeq, full[:1396])

	joined, prefix, ok := c.Feed("flow", baseSeq+1396-overlap, full[1396-overlap:])
	if !ok {
		t.Fatal("overlapping segment should join")
	}
	if prefix != 1396 {
		t.Fatalf("prefix length: want 1396, got %d", prefix)
	}
	if !bytes.Equal(joined, full) {
		t.Fatalf("overlap trimming produced %d bytes, want %d", len(joined), len(full))
	}
}

func TestPendingHelloCacheGapDropsPrefix(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	const baseSeq = uint32(1)

	c := newPendingHelloCache()
	c.Feed("flow", baseSeq, full[:1396])

	if _, _, ok := c.Feed("flow", baseSeq+2000, full[1396:]); ok {
		t.Fatal("out-of-order segment must not join")
	}
	if c.Len() != 0 {
		t.Fatalf("gap should discard the stale prefix, entries=%d", c.Len())
	}
}

func TestPendingHelloCacheExpiry(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	const baseSeq = uint32(1)

	c := newPendingHelloCache()
	c.Feed("flow", baseSeq, full[:1396])

	c.mu.Lock()
	c.flows["flow"].storedAt = time.Now().Add(-2 * pendingHelloTTL)
	c.mu.Unlock()

	if _, _, ok := c.Feed("flow", baseSeq+1396, full[1396:]); ok {
		t.Fatal("expired prefix must not join")
	}

	c.Cleanup()
	if c.Len() != 0 {
		t.Fatalf("Cleanup should drop expired entries, entries=%d", c.Len())
	}
}

func TestPendingHelloCacheRejectsOversizedRecord(t *testing.T) {
	oversized := buildClientHello("i.ytimg.com", maxPendingHelloRecord*2, 0xAB)

	c := newPendingHelloCache()
	c.Feed("flow", 1, oversized[:maxPendingHelloRecord+1])
	if c.Len() != 0 {
		t.Fatalf("payload above the record cap must not be buffered, entries=%d", c.Len())
	}
}

func TestPendingHelloCacheBoundsEntries(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	seg1 := full[:1396]

	c := newPendingHelloCache()
	for i := 0; i < maxPendingHelloEntries+64; i++ {
		c.Feed(string(rune(i))+"-flow", uint32(i), seg1)
	}
	if c.Len() > maxPendingHelloEntries {
		t.Fatalf("entry cap exceeded: %d", c.Len())
	}
	if c.bytes > maxPendingHelloBytes {
		t.Fatalf("byte cap exceeded: %d", c.bytes)
	}
}

func TestPendingHelloCacheNilReceiver(t *testing.T) {
	var c *pendingHelloCache
	if _, _, ok := c.Feed("flow", 1, []byte{0x16}); ok {
		t.Fatal("nil cache must not report a join")
	}
	c.Drop("flow")
	c.Cleanup()
	if c.Len() != 0 {
		t.Fatal("nil cache must report zero entries")
	}
}

func TestLocateSNIInContinuationSegment(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	seg2 := full[1396:]

	if _, _, ok := locateSNIInRecord(seg2); ok {
		t.Fatal("structural parse must not succeed on a mid-record segment")
	}

	start, end, ok := locateSNI(seg2)
	if !ok {
		t.Fatal("locateSNI should find the hostname in the continuation segment")
	}
	if got := string(seg2[start:end]); got != "i.ytimg.com" {
		t.Fatalf("located hostname: want i.ytimg.com, got %q", got)
	}
}

func TestLocateSNIStillPrefersStructuralParse(t *testing.T) {
	full := buildClientHello("i.ytimg.com", 16, 0xAB)

	start, end, ok := locateSNI(full)
	if !ok {
		t.Fatal("locateSNI should parse a complete ClientHello")
	}
	if got := string(full[start:end]); got != "i.ytimg.com" {
		t.Fatalf("located hostname: want i.ytimg.com, got %q", got)
	}
}

func TestHandlerRecoversSplitClientHelloClassification(t *testing.T) {
	set := newPassiveSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	const baseSeq = uint32(1000)
	const cut = 1396
	const connKey = "10.0.0.1:12345->1.2.3.4:443"

	if v := w.ProcessPacket(makeV4TCPPacket(full[:cut], baseSeq)); v != engine.VerdictAccept {
		t.Fatalf("first segment: want accept, got %v", v)
	}
	if host, _, found := w.tlsCache.Lookup(connKey); found {
		t.Fatalf("first segment must not yield a cached host, got %q", host)
	}
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
		t.Fatalf("first segment must not bind a set, got %q", bound.Name)
	}
	if w.pendingHello.Len() != 1 {
		t.Fatalf("first segment should be buffered, entries=%d", w.pendingHello.Len())
	}

	w.ProcessPacket(makeV4TCPPacket(full[cut:], baseSeq+cut))

	host, tlsVersion, found := w.tlsCache.Lookup(connKey)
	if !found || host != "i.ytimg.com" {
		t.Fatalf("continuation segment should recover the SNI, got host=%q found=%v", host, found)
	}
	if tlsVersion != 0x0304 {
		t.Fatalf("recovered TLS version: want 0x0304, got 0x%04x", tlsVersion)
	}

	bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443)
	if bound == nil {
		t.Fatal("continuation segment should bind the matched set to the flow")
	}
	if bound.Id != "yt-images" {
		t.Fatalf("bound set: want yt-images, got %q", bound.Id)
	}
}

func TestHandlerRecoversWhenSplitFallsInsideHostname(t *testing.T) {
	set := newPassiveSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	full := buildClientHello("i.ytimg.com", 1400, 0xAB)
	nameAt := bytes.Index(full, []byte("i.ytimg.com"))
	if nameAt < 0 {
		t.Fatal("fixture does not contain the hostname")
	}
	cut := nameAt + 4

	const baseSeq = uint32(1000)
	w.ProcessPacket(makeV4TCPPacket(full[:cut], baseSeq))
	w.ProcessPacket(makeV4TCPPacket(full[cut:], baseSeq+uint32(cut)))

	host, _, found := w.tlsCache.Lookup("10.0.0.1:12345->1.2.3.4:443")
	if !found || host != "i.ytimg.com" {
		t.Fatalf("a hostname straddling the segment boundary should still be recovered, got %q found=%v", host, found)
	}
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil {
		t.Fatal("straddled hostname should still bind the matched set")
	}

	if _, _, ok := locateSNI(full[cut:]); ok {
		t.Fatal("a partial hostname must not be reported as a located SNI")
	}
}

func TestHandlerClassifiesUnsplitClientHelloOnFirstPacket(t *testing.T) {
	set := newPassiveSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	full := buildClientHello("i.ytimg.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(full, 1000))

	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil {
		t.Fatal("a complete ClientHello must classify on the first packet")
	}
	if w.pendingHello.Len() != 0 {
		t.Fatalf("a classified flow must not be buffered, entries=%d", w.pendingHello.Len())
	}
}

func TestScanSNIExtensionRejectsNonTLS(t *testing.T) {
	if _, _, ok := scanSNIExtension(bytes.Repeat([]byte{0x00}, 2048)); ok {
		t.Fatal("all-zero padding must not yield a hostname")
	}
	if _, _, ok := scanSNIExtension(bytes.Repeat([]byte{0xFF}, 2048)); ok {
		t.Fatal("all-ones payload must not yield a hostname")
	}

	appData := append([]byte{0x17, 0x03, 0x03, 0x05, 0x00}, buildSNIExtension("i.ytimg.com")...)
	if _, _, ok := scanSNIExtension(appData); ok {
		t.Fatal("application data records must not be scanned")
	}
}
