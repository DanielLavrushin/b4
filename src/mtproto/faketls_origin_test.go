package mtproto

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/crypto/cryptobyte"
)

func testOriginCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

type recordingConn struct {
	net.Conn
	mu  *sync.Mutex
	out *bytes.Buffer
}

func (c recordingConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.out.Write(p)
	c.mu.Unlock()
	return c.Conn.Write(p)
}

type testOrigin struct {
	mu     sync.Mutex
	writes []*bytes.Buffer
}

func startTestOrigin(t *testing.T, cfg *tls.Config) *testOrigin {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	o := serveTestOrigin(t, ln, cfg)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	prevPort := originProbePort
	originProbePort = port
	t.Cleanup(func() { originProbePort = prevPort })
	return o
}

func serveTestOrigin(t *testing.T, ln net.Listener, cfg *tls.Config) *testOrigin {
	t.Helper()
	cfg.Certificates = []tls.Certificate{testOriginCertificate(t)}
	o := &testOrigin{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			out := &bytes.Buffer{}
			o.mu.Lock()
			o.writes = append(o.writes, out)
			o.mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				_ = tls.Server(recordingConn{Conn: c, mu: &o.mu, out: out}, cfg).Handshake()
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	return o
}

func (o *testOrigin) firstReply(t *testing.T) []byte {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.writes) == 0 {
		t.Fatalf("the origin saw no connection")
	}
	return bytes.Clone(o.writes[0].Bytes())
}

func firstEncryptedRecord(t *testing.T, reply []byte) int {
	t.Helper()
	for off := 0; off+5 <= len(reply); off += 5 + int(binary.BigEndian.Uint16(reply[off+3:])) {
		if reply[off] == tlsRecordAppData {
			return int(binary.BigEndian.Uint16(reply[off+3:]))
		}
	}
	t.Fatalf("the origin sent no encrypted record")
	return 0
}

func dropSelfDialMark(t *testing.T) {
	prev := selfDialMark
	selfDialMark = func() uint { return 0 }
	t.Cleanup(func() { selfDialMark = prev })
}

func TestTheProbeCopiesATLS13Origin(t *testing.T) {
	cases := []struct {
		name  string
		cfg   *tls.Config
		group uint16
	}{
		{"post-quantum origin", &tls.Config{}, groupX25519MLKEM768},
		{"X25519-only origin", &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519}}, groupX25519},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origin := startTestOrigin(t, tc.cfg)
			p, err := profileOrigin(context.Background(), "localhost", 0)
			if err != nil {
				t.Fatalf("profileOrigin: %v", err)
			}
			reply := origin.firstReply(t)
			sh := parseServerHelloFields(t, reply)
			if p.group != tc.group || p.group != sh.group {
				t.Fatalf("profile group 0x%04x, origin answered 0x%04x, want 0x%04x", p.group, sh.group, tc.group)
			}
			if p.cipher != sh.cipher {
				t.Fatalf("profile cipher 0x%04x, origin answered 0x%04x", p.cipher, sh.cipher)
			}
			if p.versionsFirst != (sh.extensions[0] == 0x002b) {
				t.Fatalf("profile versionsFirst %v, origin extension order %04x", p.versionsFirst, sh.extensions)
			}
			raw := firstEncryptedRecord(t, reply)
			if raw >= 64 {
				t.Fatalf("the test origin's first record is %d bytes, too long to check the 64-byte floor", raw)
			}
			if p.appData != [2]int{64, 64} {
				t.Fatalf("profile record %v for a %d-byte first record, want [64 64]", p.appData, raw)
			}
		})
	}
}

func TestTheProbeRefusesOriginsItCannotCopy(t *testing.T) {
	t.Run("TLS 1.2 only", func(t *testing.T) {
		startTestOrigin(t, &tls.Config{MaxVersion: tls.VersionTLS12})
		if _, err := profileOrigin(context.Background(), "localhost", 0); !errors.Is(err, errOriginNotTLS13) {
			t.Fatalf("err %v, want errOriginNotTLS13", err)
		}
	})
	t.Run("asks for another key share", func(t *testing.T) {
		startTestOrigin(t, &tls.Config{CurvePreferences: []tls.CurveID{tls.CurveP256}})
		if _, err := profileOrigin(context.Background(), "localhost", 0); !errors.Is(err, errOriginRetry) {
			t.Fatalf("err %v, want errOriginRetry", err)
		}
	})
	t.Run("nothing listening", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		_ = ln.Close()
		prevPort := originProbePort
		originProbePort = port
		t.Cleanup(func() { originProbePort = prevPort })
		_, err = profileOrigin(context.Background(), "localhost", 0)
		if err == nil || errors.Is(err, errOriginNotTLS13) || errors.Is(err, errOriginRetry) {
			t.Fatalf("err %v, want a connection error", err)
		}
	})
}

func craftedServerHello(cipher, group uint16, keyLen int, versionsFirst, tls13 bool, random []byte) []byte {
	var b cryptobyte.Builder
	b.AddUint8(tlsRecordHandshake)
	b.AddUint16(0x0303)
	b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint8(handshakeServerHello)
		b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddUint16(0x0303)
			b.AddBytes(random)
			b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(make([]byte, 32)) })
			b.AddUint16(cipher)
			b.AddUint8(0)
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				versions := func() {
					if tls13 {
						b.AddUint16(0x002b)
						b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint16(0x0304) })
					}
				}
				keyShare := func() {
					b.AddUint16(extKeyShare)
					b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
						b.AddUint16(group)
						b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(make([]byte, keyLen)) })
					})
				}
				if versionsFirst {
					versions()
					keyShare()
				} else {
					keyShare()
					versions()
				}
			})
		})
	})
	return b.BytesOrPanic()
}

func record(typ byte, n int) []byte {
	return append([]byte{typ, 0x03, 0x03, byte(n >> 8), byte(n)}, make([]byte, n)...)
}

func TestTheProbeReadsTheFirstEncryptedRecordOfTheReply(t *testing.T) {
	random := make([]byte, 32)
	random[0] = 1
	hybrid := craftedServerHello(0x1302, groupX25519MLKEM768, 1120, false, true, random)
	cases := []struct {
		name  string
		reply []byte
		want  originReply
		err   error
	}{
		{"one record after CCS", slices.Concat(hybrid, record(tlsRecordChangeCipher, 1), record(tlsRecordAppData, 2700), record(tlsRecordAppData, 100)),
			originReply{cipher: 0x1302, group: groupX25519MLKEM768, appData: 2700}, nil},
		{"no CCS", slices.Concat(hybrid, record(tlsRecordAppData, 36), record(tlsRecordAppData, 3033)),
			originReply{cipher: 0x1302, group: groupX25519MLKEM768, appData: 36}, nil},
		{"supported_versions first", slices.Concat(craftedServerHello(0x1301, groupX25519, 32, true, true, random), record(tlsRecordChangeCipher, 1), record(tlsRecordAppData, 23)),
			originReply{cipher: 0x1301, group: groupX25519, versionsFirst: true, appData: 23}, nil},
		{"alert", record(tlsRecordAlert, 2), originReply{}, errOriginNotTLS13},
		{"TLS 1.2 ServerHello", slices.Concat(craftedServerHello(0xc02f, groupX25519, 32, false, false, random), record(tlsRecordHandshake, 900)), originReply{}, errOriginNotTLS13},
		{"TLS 1.2 ServerHello before an encrypted record", slices.Concat(craftedServerHello(0xc02f, groupX25519, 32, false, false, random), record(tlsRecordAppData, 900)), originReply{}, errOriginNotTLS13},
		{"plaintext handshake after the ServerHello", slices.Concat(hybrid, record(tlsRecordHandshake, 900)), originReply{}, errOriginNotTLS13},
		{"HelloRetryRequest", craftedServerHello(0x1301, uint16(tls.CurveP256), 0, false, true, helloRetryRandom[:]), originReply{}, errOriginRetry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readOriginReply(bytes.NewReader(tc.reply))
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestTheProbeOffersCiphersInTelegramsOrder(t *testing.T) {
	hello, err := originClientHello("localhost", true)
	if err != nil {
		t.Fatalf("originClientHello: %v", err)
	}
	p := 5 + 4 + 2 + 32
	p += 1 + int(hello[p])
	if got := hello[p : p+8]; !bytes.Equal(got, []byte{0x00, 0x06, 0x13, 0x01, 0x13, 0x02, 0x13, 0x03}) {
		t.Fatalf("cipher suites % x, want 1301 1302 1303", got)
	}

	chachaFirst := bytes.Clone(hello)
	copy(chachaFirst[p+2:], []byte{0x13, 0x03, 0x13, 0x01, 0x13, 0x02})
	telegramSuiteOrder(chachaFirst)
	if !bytes.Equal(chachaFirst, hello) {
		t.Fatalf("a ChaCha-first hello was not put into Telegram's order")
	}

	other := bytes.Clone(hello)
	copy(other[p+2:], []byte{0xc0, 0x2b, 0x13, 0x01, 0x13, 0x02})
	untouched := bytes.Clone(other)
	telegramSuiteOrder(other)
	if !bytes.Equal(other, untouched) {
		t.Fatalf("a hello with other suites was rewritten")
	}
}

func hybridTelegramHello(t *testing.T, sec *Secret) []byte {
	t.Helper()
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		t.Fatalf("ML-KEM key: %v", err)
	}
	return signClientHello(sec, buildTelegramHello(randomBytes(32), []testKeyShare{
		{group: 0x4a4a, data: []byte{0}},
		{group: groupX25519MLKEM768, data: append(dk.EncapsulationKey().Bytes(), clientX25519Share(t)...)},
		{group: groupX25519, data: clientX25519Share(t)},
	}, nil))
}

func appDataLength(flight []byte) int {
	rest := flight[5+int(binary.BigEndian.Uint16(flight[3:5])):]
	return int(binary.BigEndian.Uint16(rest[9:11]))
}

func TestTheFlightFollowsTheOriginProfile(t *testing.T) {
	sec := newFakeSecret(t)
	cases := []struct {
		name       string
		profile    originProfile
		cipher     uint16
		group      uint16
		extensions []uint16
	}{
		{"X25519 origin with OpenSSL order", originProfile{appData: [2]int{3000, 3000}, cipher: 0x1302, group: groupX25519, versionsFirst: true},
			0x1302, groupX25519, []uint16{0x002b, extKeyShare}},
		{"post-quantum origin", originProfile{appData: [2]int{2653, 2655}, cipher: 0x1302, group: groupX25519MLKEM768},
			0x1302, groupX25519MLKEM768, []uint16{extKeyShare, 0x002b}},
		{"cipher the app did not offer", originProfile{appData: [2]int{500, 500}, cipher: 0x1304},
			0x1301, groupX25519MLKEM768, []uint16{extKeyShare, 0x002b}},
		{"largest record", originProfile{appData: [2]int{maxAppDataRecord, maxAppDataRecord}, cipher: 0x1301},
			0x1301, groupX25519MLKEM768, []uint16{extKeyShare, 0x002b}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hello := hybridTelegramHello(t, sec)
			var asked []string
			res := runFakeTLSHandshakeFor(t, []*Secret{sec}, hello, func(host string) originProfile {
				asked = append(asked, host)
				return tc.profile
			})
			checkTelegramFlight(t, sec, hello, res.flight)
			if !slices.Equal(asked, []string{sec.Host}) {
				t.Fatalf("profile looked up for %q, want the secret's domain %q", asked, sec.Host)
			}
			f := parseServerHelloFields(t, res.flight)
			if f.cipher != tc.cipher || f.group != tc.group || !slices.Equal(f.extensions, tc.extensions) {
				t.Fatalf("cipher 0x%04x group 0x%04x extensions %04x, want 0x%04x 0x%04x %04x", f.cipher, f.group, f.extensions, tc.cipher, tc.group, tc.extensions)
			}
			if !isX25519PublicKey(f.key[len(f.key)-x25519KeySize:]) {
				t.Fatalf("the key share does not end in an X25519 public key")
			}
			if n := appDataLength(res.flight); n < tc.profile.appData[0] || n > tc.profile.appData[1] {
				t.Fatalf("AppData record of %d bytes, want %d..%d", n, tc.profile.appData[0], tc.profile.appData[1])
			}
		})
	}
}

func TestTheFlightWithoutAProfileUsesTheFallbackSize(t *testing.T) {
	if fallbackAppData < minAppDataRecord || fallbackAppData > 160 {
		t.Fatalf("fallback record of %d bytes, want 64..160", fallbackAppData)
	}
	sec := newFakeSecret(t)
	hello := hybridTelegramHello(t, sec)
	res := runFakeTLSHandshake(t, []*Secret{sec}, hello)
	checkTelegramFlight(t, sec, hello, res.flight)
	if n := appDataLength(res.flight); n != fallbackAppData {
		t.Fatalf("AppData record of %d bytes, want the fallback %d", n, fallbackAppData)
	}
}

func originTestConfig(t *testing.T, hosts ...string) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.System.MTProto = config.MTProtoConfig{
		Enabled:      true,
		BindAddress:  "127.0.0.1",
		FakeSNI:      "localhost",
		UpstreamMode: "tcp",
	}
	for i, host := range hosts {
		sec, err := GenerateSecret(host)
		if err != nil {
			t.Fatalf("GenerateSecret: %v", err)
		}
		id := string(rune('a' + i))
		cfg.System.MTProto.Secrets = append(cfg.System.MTProto.Secrets, config.MTProtoSecret{ID: id, Name: id, Secret: sec.Hex(), Enabled: true})
	}
	return cfg
}

func waitForOrigin(t *testing.T, s *Server, host string) originProfile {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := s.origins.Load(); m != nil {
			if p, ok := (*m)[host]; ok {
				return p
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no profile for %s", host)
	return originProfile{}
}

func TestTheServerFollowsItsFakeDomainAndNewSecretDomains(t *testing.T) {
	dropSelfDialMark(t)
	startTestOrigin(t, &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519}})
	cfg := originTestConfig(t, "localhost")
	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	if p := waitForOrigin(t, s, "localhost"); p.group != groupX25519 || p.appData[0] < minAppDataRecord {
		t.Fatalf("profile for the fake domain %+v", p)
	}
	if p := s.originFor("unknown.example"); p.group != groupX25519 {
		t.Fatalf("a domain without a profile got %+v, want the fake domain's profile", p)
	}

	s.UpdateConfig(originTestConfig(t, "localhost", "127.0.0.1"))
	if p := waitForOrigin(t, s, "127.0.0.1"); p.group != groupX25519 {
		t.Fatalf("profile for the new secret's domain %+v", p)
	}
}

func TestTheProbeFallsBackToX25519WhenThePostQuantumHelloIsDropped(t *testing.T) {
	startTestOrigin(t, &tls.Config{})
	originPort := originProbePort
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var dropped atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				hdr := make([]byte, 5)
				if _, err := io.ReadFull(c, hdr); err != nil {
					return
				}
				if binary.BigEndian.Uint16(hdr[3:5]) >= 1000 {
					dropped.Add(1)
					return
				}
				up, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", originPort))
				if err != nil {
					return
				}
				_ = up.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := up.Write(hdr); err != nil {
					_ = up.Close()
					return
				}
				done := make(chan struct{})
				go func() {
					_, _ = io.Copy(up, c)
					_ = up.Close()
					close(done)
				}()
				_, _ = io.Copy(c, up)
				_ = c.Close()
				<-done
			}()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	originProbePort = port
	t.Cleanup(func() {
		originProbePort = originPort
		_ = ln.Close()
		wg.Wait()
	})

	p, err := profileOrigin(context.Background(), "localhost", 0)
	if err != nil {
		t.Fatalf("profileOrigin: %v", err)
	}
	if dropped.Load() == 0 {
		t.Fatalf("no post-quantum hello reached the dropping path")
	}
	if p.group != 0 {
		t.Fatalf("group 0x%04x learned from X25519-only hellos, want unknown", p.group)
	}
	if p.appData[0] < 64 || p.cipher == 0 {
		t.Fatalf("profile %+v", p)
	}
}

func TestAFailedRefreshKeepsTheLastProfile(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	prevPort := originProbePort
	originProbePort = port
	t.Cleanup(func() { originProbePort = prevPort })

	s := NewServer(originTestConfig(t, "localhost"))
	last := originProfile{appData: [2]int{2654, 2655}, cipher: 0x1302, group: groupX25519MLKEM768}
	s.origins.Store(&map[string]originProfile{"localhost": last})
	if retry := s.profileOrigins(context.Background(), true, map[string]string{}); !retry {
		t.Fatalf("an unreachable fake domain did not ask for an early retry")
	}
	if got := s.originFor("localhost"); got != last {
		t.Fatalf("profile after a failed refresh %+v, want the last one %+v", got, last)
	}
}

func TestTheServerRetriesAnUnreachableFakeDomainSoon(t *testing.T) {
	dropSelfDialMark(t)
	prevRetry := originRetryEvery
	originRetryEvery = 50 * time.Millisecond
	t.Cleanup(func() { originRetryEvery = prevRetry })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	prevPort := originProbePort
	originProbePort = port
	t.Cleanup(func() { originProbePort = prevPort })

	s := NewServer(originTestConfig(t, "localhost"))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	time.Sleep(200 * time.Millisecond)

	again, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		t.Skipf("port %s was taken in between: %v", port, err)
	}
	serveTestOrigin(t, again, &tls.Config{})
	waitForOrigin(t, s, "localhost")
}
