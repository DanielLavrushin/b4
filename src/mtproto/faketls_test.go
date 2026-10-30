package mtproto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net"
	"slices"
	"testing"
	"time"

	"golang.org/x/crypto/cryptobyte"
)

func newFakeSecretWithKey(t *testing.T, keyHex string) *Secret {
	hostHex := hex.EncodeToString([]byte("storage.googleapis.com"))
	sec, err := ParseSecret("ee" + keyHex + hostHex)
	if err != nil {
		t.Fatalf("ParseSecret: %v", err)
	}
	return sec
}

// makeValidClientHello crafts a ClientHello whose client-random field carries
// the HMAC a real fake-TLS client would send for the given secret.
func makeValidClientHello(sec *Secret) []byte {
	const bodyLen = 39
	body := make([]byte, bodyLen)
	body[0] = 0x01
	body[1] = byte((bodyLen - 4) >> 16)
	body[2] = byte((bodyLen - 4) >> 8)
	body[3] = byte(bodyLen - 4)
	body[4] = 0x03
	body[5] = 0x03
	body[38] = 0 // session id length
	hdr := []byte{0x16, 0x03, 0x01, byte(bodyLen >> 8), byte(bodyLen)}
	return signClientHello(sec, append(hdr, body...))
}

func signClientHello(sec *Secret, clientHello []byte) []byte {
	zeroed := make([]byte, len(clientHello))
	copy(zeroed, clientHello)
	clear(zeroed[11:43])
	mac := hmac.New(sha256.New, sec.Key[:])
	mac.Write(zeroed)
	expected := mac.Sum(nil)

	random := make([]byte, 32)
	copy(random, expected[:28])
	tsBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(tsBytes, uint32(time.Now().Unix()))
	for i := 0; i < 4; i++ {
		random[28+i] = expected[28+i] ^ tsBytes[i]
	}
	copy(clientHello[11:43], random)
	return clientHello
}

func TestAcceptFakeTLSMulti_SelectsMatchingSecret(t *testing.T) {
	sec1 := newFakeSecretWithKey(t, "0123456789abcdef0123456789abcdef")
	sec2 := newFakeSecretWithKey(t, "ffffffffffffffffffffffffffffffff")
	sec2.ID = "user-2"
	sec2.Name = "Ivan"

	hello := makeValidClientHello(sec2)

	res := runFakeTLSHandshake(t, []*Secret{sec1, sec2}, hello)
	checkTelegramFlight(t, sec2, hello, res.flight)
	if res.conn == nil {
		t.Fatalf("expected a connection")
	}
	matched := res.matched
	if matched != sec2 {
		t.Fatalf("expected sec2 to match, got %v", matched)
	}
	if matched.Label() != "Ivan" {
		t.Fatalf("expected label Ivan, got %q", matched.Label())
	}
}

func TestAcceptFakeTLSMulti_NoMatchReturnsVerifyError(t *testing.T) {
	sec1 := newFakeSecretWithKey(t, "0123456789abcdef0123456789abcdef")
	other := newFakeSecretWithKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	hello := makeValidClientHello(other)

	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hello)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, _, err := AcceptFakeTLSMulti(srv, []*Secret{sec1})
	if err == nil {
		t.Fatalf("expected error when no secret matches")
	}
	var vErr *FakeTLSVerifyError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected FakeTLSVerifyError for masking fallback, got %T: %v", err, err)
	}
}

func makeBogusClientHello(bodyLen int) []byte {
	hdr := []byte{0x16, 0x03, 0x01, byte(bodyLen >> 8), byte(bodyLen)}
	body := make([]byte, bodyLen)
	body[0] = 0x01
	body[1] = byte((bodyLen - 4) >> 16)
	body[2] = byte((bodyLen - 4) >> 8)
	body[3] = byte(bodyLen - 4)
	body[4] = 0x03
	body[5] = 0x03
	for i := 6; i < 38 && i < bodyLen; i++ {
		body[i] = byte(i)
	}
	return append(hdr, body...)
}

func newFakeSecret(t *testing.T) *Secret {
	keyHex := "0123456789abcdef0123456789abcdef"
	hostHex := hex.EncodeToString([]byte("storage.googleapis.com"))
	sec, err := ParseSecret("ee" + keyHex + hostHex)
	if err != nil {
		t.Fatalf("ParseSecret: %v", err)
	}
	return sec
}

func TestAcceptFakeTLS_HMACFail_ReturnsVerifyErrorWithInitial(t *testing.T) {
	hello := makeBogusClientHello(64)
	sec := newFakeSecret(t)

	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hello)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := AcceptFakeTLS(srv, sec)
	if err == nil {
		t.Fatalf("expected error on bogus ClientHello")
	}
	var vErr *FakeTLSVerifyError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected FakeTLSVerifyError, got %T: %v", err, err)
	}
	if !bytes.Equal(vErr.Initial, hello) {
		t.Fatalf("Initial bytes mismatch: got %d bytes, want %d (hello)", len(vErr.Initial), len(hello))
	}
}

func TestAcceptFakeTLS_NotClientHello_ReturnsVerifyErrorWithInitial(t *testing.T) {
	hello := makeBogusClientHello(64)
	hello[5] = 0x02

	sec := newFakeSecret(t)
	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hello)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := AcceptFakeTLS(srv, sec)
	if err == nil {
		t.Fatalf("expected error when body[0] != ClientHello")
	}
	var vErr *FakeTLSVerifyError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected FakeTLSVerifyError, got %T: %v", err, err)
	}
	if !bytes.Equal(vErr.Initial, hello) {
		t.Fatalf("Initial bytes must contain full read buffer for masking-fallback replay")
	}
}

func TestAcceptFakeTLS_BadRecordLength_ReturnsVerifyErrorWithHeader(t *testing.T) {
	hdr := []byte{0x16, 0x03, 0x01, 0x00, 0x10}

	sec := newFakeSecret(t)
	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hdr)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := AcceptFakeTLS(srv, sec)
	if err == nil {
		t.Fatalf("expected error on too-short record length")
	}
	var vErr *FakeTLSVerifyError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected FakeTLSVerifyError, got %T: %v", err, err)
	}
	if !bytes.Equal(vErr.Initial, hdr) {
		t.Fatalf("Initial bytes should contain the record header, got %d bytes", len(vErr.Initial))
	}
}

func TestAcceptFakeTLS_NonTLSFirstByte_NoVerifyError(t *testing.T) {
	garbage := []byte{0xFF, 0x00, 0x00, 0x00, 0x00}

	sec := newFakeSecret(t)
	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(garbage)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := AcceptFakeTLS(srv, sec)
	if err == nil {
		t.Fatalf("expected error on non-TLS first byte")
	}
	var vErr *FakeTLSVerifyError
	if errors.As(err, &vErr) {
		t.Fatalf("non-TLS first byte should NOT trigger masking-fallback (no FakeTLSVerifyError)")
	}
}

type testKeyShare struct {
	group uint16
	data  []byte
}

func buildTelegramHello(sid []byte, shares []testKeyShare, rawExt []byte) []byte {
	var b cryptobyte.Builder
	b.AddUint8(tlsRecordHandshake)
	b.AddUint16(0x0301)
	b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint8(handshakeClientHello)
		b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddUint16(0x0303)
			b.AddBytes(make([]byte, 32))
			b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(sid) })
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				for _, suite := range []uint16{0x4a4a, 0x1301, 0x1302, 0x1303, 0xc02b, 0xc02f} {
					b.AddUint16(suite)
				}
			})
			b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint8(0) })
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddUint16(0x0000)
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
					b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
						b.AddUint8(0)
						b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes([]byte("storage.googleapis.com")) })
					})
				})
				if shares != nil {
					b.AddUint16(extKeyShare)
					b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
						b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
							for _, share := range shares {
								b.AddUint16(share.group)
								b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(share.data) })
							}
						})
					})
				}
				b.AddUint16(0x002b)
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
					b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) {
						b.AddUint16(0x0304)
						b.AddUint16(0x0303)
					})
				})
				b.AddBytes(rawExt)
			})
		})
	})
	return b.BytesOrPanic()
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func clientX25519Share(t *testing.T) []byte {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("X25519 key: %v", err)
	}
	return key.PublicKey().Bytes()
}

type fakeTLSResult struct {
	flight  []byte
	elapsed time.Duration
	conn    *FakeTLSConn
	matched *Secret
}

func runFakeTLSHandshake(t *testing.T, secrets []*Secret, hello []byte) fakeTLSResult {
	t.Helper()
	srv, cli := net.Pipe()
	defer cli.Close()
	type accepted struct {
		conn    *FakeTLSConn
		matched *Secret
		err     error
	}
	done := make(chan accepted, 1)
	go func() {
		_ = srv.SetDeadline(time.Now().Add(5 * time.Second))
		conn, matched, err := AcceptFakeTLSMulti(srv, secrets)
		_ = srv.Close()
		done <- accepted{conn: conn, matched: matched, err: err}
	}()

	_ = cli.SetDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	if _, err := cli.Write(hello); err != nil {
		t.Fatalf("write ClientHello: %v", err)
	}
	first := make([]byte, 1)
	if _, err := io.ReadFull(cli, first); err != nil {
		t.Fatalf("read server flight: %v", err)
	}
	elapsed := time.Since(start)
	flight := readServerFlight(t, io.MultiReader(bytes.NewReader(first), cli))
	if n, err := cli.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		t.Fatalf("server sent more than the three flight records (n=%d, err=%v)", n, err)
	}
	res := <-done
	if res.err != nil {
		t.Fatalf("AcceptFakeTLSMulti: %v", res.err)
	}
	return fakeTLSResult{flight: flight, elapsed: elapsed, conn: res.conn, matched: res.matched}
}

func readServerFlight(t *testing.T, r io.Reader) []byte {
	t.Helper()
	var flight []byte
	for _, want := range []byte{tlsRecordHandshake, tlsRecordChangeCipher, tlsRecordAppData} {
		hdr := make([]byte, 5)
		if _, err := io.ReadFull(r, hdr); err != nil {
			t.Fatalf("read record header 0x%02x: %v", want, err)
		}
		if hdr[0] != want {
			t.Fatalf("record type 0x%02x, want 0x%02x", hdr[0], want)
		}
		payload := make([]byte, binary.BigEndian.Uint16(hdr[3:5]))
		if _, err := io.ReadFull(r, payload); err != nil {
			t.Fatalf("read record 0x%02x: %v", want, err)
		}
		flight = append(flight, hdr...)
		flight = append(flight, payload...)
	}
	return flight
}

func checkTelegramFlight(t *testing.T, sec *Secret, clientHello, flight []byte) {
	t.Helper()
	if len(flight) > 65536 {
		t.Fatalf("flight is %d bytes, Telegram clients accept at most 65536", len(flight))
	}
	if !bytes.HasPrefix(flight, []byte{0x16, 0x03, 0x03}) {
		t.Fatalf("flight starts % x, want 16 03 03", flight[:3])
	}
	rest := flight[5+int(binary.BigEndian.Uint16(flight[3:5])):]
	if !bytes.HasPrefix(rest, []byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01, 0x17, 0x03, 0x03}) {
		t.Fatalf("ServerHello is not followed by exactly one CCS and an AppData header: % x", rest[:min(len(rest), 11)])
	}
	if appData := int(binary.BigEndian.Uint16(rest[9:11])); appData == 0 || len(rest) != 11+appData {
		t.Fatalf("AppData record of %d bytes in a %d-byte tail", appData, len(rest))
	}
	zeroed := bytes.Clone(flight)
	clear(zeroed[11:43])
	mac := hmac.New(sha256.New, sec.Key[:])
	mac.Write(clientHello[11:43])
	mac.Write(zeroed)
	if !hmac.Equal(mac.Sum(nil), flight[11:43]) {
		t.Fatalf("the digest at offset 11 fails the check Telegram clients run")
	}
}

type serverHelloFields struct {
	sessionID  []byte
	cipher     uint16
	group      uint16
	key        []byte
	extensions []uint16
}

func parseServerHelloFields(t *testing.T, flight []byte) serverHelloFields {
	t.Helper()
	s := cryptobyte.String(flight[5 : 5+int(binary.BigEndian.Uint16(flight[3:5]))])
	var f serverHelloFields
	var hsType, compression uint8
	var version uint16
	var body, sid, exts cryptobyte.String
	if !s.ReadUint8(&hsType) || hsType != handshakeServerHello || !s.ReadUint24LengthPrefixed(&body) || !s.Empty() {
		t.Fatalf("malformed ServerHello handshake header")
	}
	if !body.ReadUint16(&version) || version != 0x0303 || !body.Skip(32) || !body.ReadUint8LengthPrefixed(&sid) ||
		!body.ReadUint16(&f.cipher) || !body.ReadUint8(&compression) || compression != 0 ||
		!body.ReadUint16LengthPrefixed(&exts) || !body.Empty() {
		t.Fatalf("malformed ServerHello body")
	}
	f.sessionID = sid
	for !exts.Empty() {
		var typ uint16
		var data cryptobyte.String
		if !exts.ReadUint16(&typ) || !exts.ReadUint16LengthPrefixed(&data) {
			t.Fatalf("malformed ServerHello extension")
		}
		f.extensions = append(f.extensions, typ)
		if typ != extKeyShare {
			continue
		}
		var key cryptobyte.String
		if !data.ReadUint16(&f.group) || !data.ReadUint16LengthPrefixed(&key) || !data.Empty() {
			t.Fatalf("malformed ServerHello key_share")
		}
		f.key = key
	}
	return f
}

func isX25519PublicKey(u []byte) bool {
	if len(u) != x25519KeySize {
		return false
	}
	bigEndian := bytes.Clone(u)
	slices.Reverse(bigEndian)
	x := new(big.Int).SetBytes(bigEndian)
	p := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	if x.Sign() == 0 || x.Cmp(p) >= 0 {
		return false
	}
	x2 := new(big.Int).Mul(x, x)
	rhs := new(big.Int).Mul(x2, x)
	rhs.Add(rhs, new(big.Int).Mul(big.NewInt(486662), x2))
	rhs.Add(rhs, x)
	rhs.Mod(rhs, p)
	return big.Jacobi(rhs, p) == 1
}

func mlkemRejectionKey(dk *mlkem.DecapsulationKey768, ciphertext []byte) []byte {
	z := dk.Bytes()[32:]
	return sha3.SumSHAKE256(append(bytes.Clone(z), ciphertext...), mlkem.SharedKeySize)
}

func TestTheFakeTLSFlightAnswersAPostQuantumOfferWithARealEncapsulation(t *testing.T) {
	sec := newFakeSecret(t)
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		t.Fatalf("ML-KEM key: %v", err)
	}
	sid := randomBytes(32)
	hello := signClientHello(sec, buildTelegramHello(sid, []testKeyShare{
		{group: 0x4a4a, data: []byte{0}},
		{group: groupX25519MLKEM768, data: append(dk.EncapsulationKey().Bytes(), clientX25519Share(t)...)},
		{group: groupX25519, data: clientX25519Share(t)},
	}, nil))

	res := runFakeTLSHandshake(t, []*Secret{sec}, hello)
	checkTelegramFlight(t, sec, hello, res.flight)
	f := parseServerHelloFields(t, res.flight)
	if !bytes.Equal(f.sessionID, sid) {
		t.Fatalf("session id % x, want the client's % x", f.sessionID, sid)
	}
	if f.group != groupX25519MLKEM768 || len(f.key) != mlkem.CiphertextSize768+x25519KeySize {
		t.Fatalf("key_share group 0x%04x with %d bytes, want 0x11ec with %d", f.group, len(f.key), mlkem.CiphertextSize768+x25519KeySize)
	}
	ciphertext := f.key[:mlkem.CiphertextSize768]
	shared, err := dk.Decapsulate(ciphertext)
	if err != nil {
		t.Fatalf("Decapsulate: %v", err)
	}
	if bytes.Equal(shared, mlkemRejectionKey(dk, ciphertext)) {
		t.Fatalf("the ML-KEM ciphertext is not an encapsulation to the client's key")
	}
	junk := randomBytes(mlkem.CiphertextSize768)
	if rejected, _ := dk.Decapsulate(junk); !bytes.Equal(rejected, mlkemRejectionKey(dk, junk)) {
		t.Fatalf("random bytes did not hit ML-KEM's implicit rejection, so the check above proves nothing")
	}
	if !isX25519PublicKey(f.key[mlkem.CiphertextSize768:]) {
		t.Fatalf("the X25519 half of the hybrid share is not an X25519 public key")
	}
	if !slices.Equal(f.extensions, []uint16{extKeyShare, 0x002b}) || f.cipher != 0x1301 {
		t.Fatalf("extensions %04x and cipher 0x%04x, want [0033 002b] and 0x1301", f.extensions, f.cipher)
	}
}

func TestTheFakeTLSFlightAnswersX25519WithoutAUsablePostQuantumShare(t *testing.T) {
	sec := newFakeSecret(t)
	invalidKey := bytes.Repeat([]byte{0xff}, mlkem.EncapsulationKeySize768)
	cases := []struct {
		name  string
		hello func(sid []byte) []byte
		sid   []byte
	}{
		{"x25519 only", func(sid []byte) []byte {
			return buildTelegramHello(sid, []testKeyShare{{group: groupX25519, data: clientX25519Share(t)}}, nil)
		}, randomBytes(32)},
		{"post-quantum share one byte short", func(sid []byte) []byte {
			return buildTelegramHello(sid, []testKeyShare{
				{group: groupX25519MLKEM768, data: randomBytes(mlkem.EncapsulationKeySize768 + x25519KeySize - 1)},
				{group: groupX25519, data: clientX25519Share(t)},
			}, nil)
		}, randomBytes(32)},
		{"post-quantum key out of range", func(sid []byte) []byte {
			return buildTelegramHello(sid, []testKeyShare{
				{group: groupX25519MLKEM768, data: append(bytes.Clone(invalidKey), clientX25519Share(t)...)},
				{group: groupX25519, data: clientX25519Share(t)},
			}, nil)
		}, randomBytes(32)},
		{"no key_share", func(sid []byte) []byte {
			return buildTelegramHello(sid, nil, nil)
		}, randomBytes(32)},
		{"truncated key_share", func(sid []byte) []byte {
			return buildTelegramHello(sid, nil, []byte{0x00, 0x33, 0xff, 0xff, 0x00})
		}, randomBytes(32)},
		{"empty session id", func(sid []byte) []byte {
			return buildTelegramHello(sid, []testKeyShare{{group: groupX25519, data: clientX25519Share(t)}}, nil)
		}, []byte{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hello := signClientHello(sec, tc.hello(tc.sid))
			res := runFakeTLSHandshake(t, []*Secret{sec}, hello)
			checkTelegramFlight(t, sec, hello, res.flight)
			f := parseServerHelloFields(t, res.flight)
			if !bytes.Equal(f.sessionID, tc.sid) {
				t.Fatalf("session id % x, want the client's % x", f.sessionID, tc.sid)
			}
			if f.group != groupX25519 || !isX25519PublicKey(f.key) {
				t.Fatalf("key_share group 0x%04x with %d bytes, want an X25519 public key", f.group, len(f.key))
			}
		})
	}

	t.Run("minimal hello without extensions", func(t *testing.T) {
		hello := makeValidClientHello(sec)
		res := runFakeTLSHandshake(t, []*Secret{sec}, hello)
		checkTelegramFlight(t, sec, hello, res.flight)
		f := parseServerHelloFields(t, res.flight)
		if len(f.sessionID) != 0 || f.group != groupX25519 || !isX25519PublicKey(f.key) {
			t.Fatalf("session id % x, group 0x%04x: want an empty echo and an X25519 public key", f.sessionID, f.group)
		}
	})
}

func TestTheFakeTLSKeySharesAreFreshRealKeys(t *testing.T) {
	onCurve := 0
	for range 64 {
		u := randomBytes(x25519KeySize)
		u[31] &= 0x7f
		if isX25519PublicKey(u) {
			onCurve++
		}
	}
	if onCurve == 64 {
		t.Fatalf("64 random values all passed the curve check, so it tells nothing apart")
	}

	dk, err := mlkem.GenerateKey768()
	if err != nil {
		t.Fatalf("ML-KEM key: %v", err)
	}
	offers := map[string]helloOffer{
		"x25519":       {},
		"post-quantum": {mlkemKey: dk.EncapsulationKey().Bytes()},
	}
	for name, offer := range offers {
		seen := map[string]bool{}
		for range 64 {
			_, share, err := serverKeyShare(offer)
			if err != nil {
				t.Fatalf("%s: serverKeyShare: %v", name, err)
			}
			public := share[len(share)-x25519KeySize:]
			if !isX25519PublicKey(public) {
				t.Fatalf("%s: % x is not an X25519 public key", name, public)
			}
			if seen[string(public)] {
				t.Fatalf("%s: an X25519 key was sent twice", name)
			}
			seen[string(public)] = true
		}
	}
}

func TestTheServerHelloWaitsLikeARealServer(t *testing.T) {
	sec := newFakeSecret(t)
	hello := makeValidClientHello(sec)
	res := runFakeTLSHandshake(t, []*Secret{sec}, hello)
	if res.elapsed < serverHelloDelay[0] {
		t.Fatalf("the flight came %v after the ClientHello, want at least %v", res.elapsed, serverHelloDelay[0])
	}

	seen := map[time.Duration]bool{}
	for range 200 {
		d := helloDelay()
		if d < serverHelloDelay[0] || d > serverHelloDelay[1] {
			t.Fatalf("delay %v outside %v..%v", d, serverHelloDelay[0], serverHelloDelay[1])
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Fatalf("200 delays were all %v, want them spread over the range", helloDelay())
	}
}
