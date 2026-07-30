package nfq

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
	"golang.org/x/crypto/hkdf"
)

var quicSaltV1 = []byte{
	0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3, 0x4d, 0x17,
	0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad, 0xcc, 0xbb, 0x7f, 0x0a,
}

func quicExpandLabel(t *testing.T, secret []byte, label string, outLen int) []byte {
	t.Helper()
	full := "tls13 " + label
	info := make([]byte, 2+1+len(full)+1)
	info[0] = byte(outLen >> 8)
	info[1] = byte(outLen)
	info[2] = byte(len(full))
	copy(info[3:], full)

	out := make([]byte, outLen)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, secret, info), out); err != nil {
		t.Fatalf("hkdf expand %q: %v", label, err)
	}
	return out
}

func quicInitialKeys(t *testing.T, dcid []byte) (cipher.AEAD, cipher.Block, []byte) {
	t.Helper()

	extract := hmac.New(sha256.New, quicSaltV1)
	_, _ = extract.Write(dcid)
	clientSecret := quicExpandLabel(t, extract.Sum(nil), "client in", 32)

	block, err := aes.NewCipher(quicExpandLabel(t, clientSecret, "quic key", 16))
	if err != nil {
		t.Fatalf("aes key: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	hp, err := aes.NewCipher(quicExpandLabel(t, clientSecret, "quic hp", 16))
	if err != nil {
		t.Fatalf("aes hp: %v", err)
	}
	return aead, hp, quicExpandLabel(t, clientSecret, "quic iv", 12)
}

func buildClientHelloHandshake(host string) []byte {
	record := buildClientHello(host, 16, 0xAB)
	return record[5:]
}

func encodeQUICVarint(v uint64) []byte {
	switch {
	case v < 64:
		return []byte{byte(v)}
	case v < 16384:
		out := make([]byte, 2)
		binary.BigEndian.PutUint16(out, uint16(v)|0x4000)
		return out
	default:
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, uint32(v)|0x80000000)
		return out
	}
}

func buildQUICInitialWithSNI(t *testing.T, dcid []byte, host string) []byte {
	t.Helper()

	aead, hp, iv := quicInitialKeys(t, dcid)

	hello := buildClientHelloHandshake(host)
	frame := []byte{0x06}
	frame = append(frame, encodeQUICVarint(0)...)
	frame = append(frame, encodeQUICVarint(uint64(len(hello)))...)
	frame = append(frame, hello...)

	header := []byte{0xC0}
	header = append(header, 0x00, 0x00, 0x00, 0x01)
	header = append(header, byte(len(dcid)))
	header = append(header, dcid...)
	header = append(header, 0x00)
	header = append(header, 0x00)
	header = append(header, encodeQUICVarint(uint64(1+len(frame)+aead.Overhead()))...)

	pnOff := len(header)
	aad := append(append([]byte{}, header...), 0x00)

	nonce := append([]byte{}, iv...)
	ciphertext := aead.Seal(nil, nonce, frame, aad)

	packet := append(append([]byte{}, aad...), ciphertext...)
	if pnOff+4+16 > len(packet) {
		t.Fatalf("packet too short for header protection sample: %d", len(packet))
	}

	var mask [16]byte
	hp.Encrypt(mask[:], packet[pnOff+4:pnOff+4+16])
	packet[0] ^= mask[0] & 0x0f
	packet[pnOff] ^= mask[1]

	return packet
}

func TestQUICInitialFixtureIsParseable(t *testing.T) {
	pkt := buildQUICInitialWithSNI(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, "rr1.googlevideo.com")

	host, ok := sni.ParseQUICClientHelloSNI(pkt)
	if !ok {
		t.Fatal("fixture must be a decryptable QUIC Initial carrying a ClientHello")
	}
	if host != "rr1.googlevideo.com" {
		t.Fatalf("fixture SNI: want rr1.googlevideo.com, got %q", host)
	}
}

func newQUICHintSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "yt-video"
	set.Name = "YT video"
	set.Enabled = true
	set.Targets.DomainsToMatch = []string{"googlevideo.com"}
	set.TCP.RSTProtection.Enabled = true
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	set.Faking.SNI = false
	set.Faking.SNIMutation.Mode = config.ConfigOff
	set.TCP.Desync.Mode = config.ConfigOff
	set.TCP.Win.Mode = config.ConfigOff
	set.TCP.DropSACK = false
	set.UDP.Mode = config.ConfigNone
	return set
}

func TestQUICSNILeavesSourceScopedHint(t *testing.T) {
	set := newQUICHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	initial := buildQUICInitialWithSNI(t, []byte{9, 9, 9, 9, 1, 1, 1, 1}, "rr1.googlevideo.com")
	pkt := makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443)
	w.ProcessPacket(pkt)

	setId, host, ok := w.hostHints.Lookup("10.0.0.1", "1.2.3.4")
	if !ok {
		t.Fatal("a QUIC ClientHello with a clear SNI should leave a hint for the client")
	}
	if setId != set.Id || host != "rr1.googlevideo.com" {
		t.Fatalf("hint: got setId=%q host=%q", setId, host)
	}

	if _, _, other := w.hostHints.Lookup("10.0.0.2", "1.2.3.4"); other {
		t.Fatal("the QUIC hint must not leak to another client")
	}
}

func TestSourceScopedHintBeatsGlobalLearnedIP(t *testing.T) {
	images := newHintSet()
	images.Id = "yt-images"
	images.Name = "YT images"
	images.Targets.DomainsToMatch = []string{"ytimg.com"}

	video := newQUICHintSet()

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&images, &video}

	w := newTestWorker(t, &cfg)

	initial := buildQUICInitialWithSNI(t, []byte{3, 3, 3, 3, 4, 4, 4, 4}, "rr1.googlevideo.com")
	w.ProcessPacket(makeV4UDPPacket(initial, net.ParseIP("10.0.0.2"), net.ParseIP("1.2.3.4"), 51000, 443))

	if _, learned, _ := w.getMatcher().MatchLearnedIPWithSource(net.ParseIP("1.2.3.4"), ""); learned == nil || learned.Id != video.Id {
		t.Fatal("the other client's QUIC attempt should have populated the global learned-IP cache")
	}

	resp := buildDNSResponse(0x1234, "i.ytimg.com", []net.IP{net.ParseIP("1.2.3.4")})
	w.ProcessPacket(makeV4UDPPacket(resp, net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1"), 53, 5353))

	w.ProcessPacket(makeV4TCPPacket([]byte("GET / HTTP/1.1\r\n\r\n"), 1000))

	bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443)
	if bound == nil {
		t.Fatal("the flow should have been classified")
	}
	if bound.Id != images.Id {
		t.Fatalf("a client's own domain evidence must win over another client's learned IP: want %q, got %q", images.Id, bound.Id)
	}
}

func TestLearnedIPStillAppliesWithoutHint(t *testing.T) {
	video := newQUICHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&video}

	w := newTestWorker(t, &cfg)

	initial := buildQUICInitialWithSNI(t, []byte{5, 5, 5, 5, 6, 6, 6, 6}, "rr1.googlevideo.com")
	w.ProcessPacket(makeV4UDPPacket(initial, net.ParseIP("10.0.0.2"), net.ParseIP("1.2.3.4"), 51000, 443))

	unrelated := buildClientHello("maps.example.org", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(unrelated, 1000))

	bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443)
	if bound == nil || bound.Id != video.Id {
		t.Fatal("with no hint for this client the learned-IP match must still apply")
	}
}

func TestQUICToTCPHandoff(t *testing.T) {
	set := newQUICHintSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	initial := buildQUICInitialWithSNI(t, []byte{7, 7, 7, 7, 2, 2, 2, 2}, "rr1.googlevideo.com")
	w.ProcessPacket(makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443))

	w.ProcessPacket(makeV4TCPPacket([]byte("GET / HTTP/1.1\r\n\r\n"), 1000))

	bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443)
	if bound == nil {
		t.Fatal("the TCP flow following a rejected QUIC attempt should inherit the set")
	}
	if bound.Id != set.Id {
		t.Fatalf("bound set: want %q, got %q", set.Id, bound.Id)
	}
}
