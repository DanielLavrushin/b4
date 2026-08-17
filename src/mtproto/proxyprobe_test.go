package mtproto

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestProxyProbe is a manual diagnostic, not part of the normal suite. It plays
// a Telegram client against a running b4 MTProto proxy - fake-TLS handshake,
// obfuscated2 frame, one req_pq - and reports how long the answer took. That
// gap is the whole bug this work is about: the client is committed once the
// local handshakes are answered, and Telegram calls the proxy misconfigured if
// nothing comes back before it gives up.
//
//	B4_PROXY_PROBE=host:port B4_PROXY_SECRET=<hex> B4_PROXY_DC=203 \
//	  go test ./mtproto/ -run TestProxyProbe -v
func TestProxyProbe(t *testing.T) {
	addr := os.Getenv("B4_PROXY_PROBE")
	secretHex := os.Getenv("B4_PROXY_SECRET")
	if addr == "" || secretHex == "" {
		t.Skip("set B4_PROXY_PROBE=host:port and B4_PROXY_SECRET=<hex> to run")
	}
	dc := 203
	if s := os.Getenv("B4_PROXY_DC"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("B4_PROXY_DC: %v", err)
		}
		dc = v
	}
	rounds := 5
	if s := os.Getenv("B4_PROXY_ROUNDS"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			rounds = v
		}
	}

	sec, err := ParseSecret(secretHex)
	if err != nil {
		t.Fatalf("ParseSecret: %v", err)
	}

	var slowest time.Duration
	failed := 0
	for i := 0; i < rounds; i++ {
		took, err := probeProxyOnce(addr, sec, dc)
		if err != nil {
			failed++
			t.Errorf("round %d: DC %d answered nothing: %v (after %v)", i+1, dc, err, took)
			continue
		}
		if took > slowest {
			slowest = took
		}
		t.Logf("round %d: DC %d answered in %v", i+1, dc, took)
	}
	t.Logf("slowest first answer: %v over %d round(s), %d failed", slowest, rounds, failed)
}

// probeProxyOnce opens one session and returns how long the data center took to
// answer, measured from the moment the client connects - which is what the
// client itself is measuring when it decides a proxy is broken.
func probeProxyOnce(addr string, sec *Secret, dc int) (time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return time.Since(start), err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))

	if _, err := conn.Write(makeValidClientHello(sec)); err != nil {
		return time.Since(start), fmt.Errorf("client hello: %w", err)
	}

	frame, enc, dec, err := clientObfuscatedFrame(sec, dc, connectionTagAbridged)
	if err != nil {
		return time.Since(start), err
	}
	if err := writeTLSRecord(conn, frame); err != nil {
		return time.Since(start), fmt.Errorf("obfuscated frame: %w", err)
	}

	req := reqPQFrame()
	enc.XORKeyStream(req, req)
	if err := writeTLSRecord(conn, req); err != nil {
		return time.Since(start), fmt.Errorf("req_pq: %w", err)
	}

	// The fake-TLS server hello ends in one application-data record of noise, and
	// its bytes are not part of the relayed stream - decrypting them would burn
	// the keystream and turn everything after it into garbage.
	if _, err := readTLSAppData(conn); err != nil {
		return time.Since(start), fmt.Errorf("server hello noise: %w", err)
	}

	payload, err := readTLSAppData(conn)
	if err != nil {
		return time.Since(start), err
	}
	dec.XORKeyStream(payload, payload)
	if err := checkResPQ(payload); err != nil {
		return time.Since(start), err
	}
	return time.Since(start), nil
}

// checkResPQ makes the probe prove it reached Telegram rather than anything b4
// produced on its own: only the data center can answer req_pq.
func checkResPQ(payload []byte) error {
	if len(payload) < 1 {
		return fmt.Errorf("empty answer")
	}
	n := int(payload[0]&0x7f) * 4
	msg := payload[1:]
	if payload[0] == 0x7f {
		if len(payload) < 4 {
			return fmt.Errorf("short extended length header")
		}
		n = (int(payload[1]) | int(payload[2])<<8 | int(payload[3])<<16) * 4
		msg = payload[4:]
	}
	if len(msg) < n || n < 24 {
		return fmt.Errorf("truncated packet: header says %d, have %d", n, len(msg))
	}
	if id := binary.LittleEndian.Uint64(msg[0:8]); id != 0 {
		return fmt.Errorf("auth_key_id %#x, want 0 for an unencrypted answer", id)
	}
	if ctor := binary.LittleEndian.Uint32(msg[20:24]); ctor != 0x05162463 {
		return fmt.Errorf("constructor %#08x, want res_pq 0x05162463", ctor)
	}
	return nil
}

// clientObfuscatedFrame is completeObfuscation's client-side mirror: the same
// frame, but keyed through the proxy secret rather than raw, which is what a
// client speaking to a secret-protected proxy sends.
func clientObfuscatedFrame(sec *Secret, dc int, protoTag uint32) (wire []byte, enc, dec cipher.Stream, err error) {
	frame := generateFrame(dc, protoTag)

	encIV := make([]byte, 16)
	copy(encIV, frame[40:56])
	enc, err = newAESCTR(deriveKey(frame[8:40], sec.Key[:]), encIV)
	if err != nil {
		return nil, nil, nil, err
	}

	reversed := make([]byte, 48)
	for i := 0; i < 48; i++ {
		reversed[i] = frame[55-i]
	}
	decIV := make([]byte, 16)
	copy(decIV, reversed[32:48])
	dec, err = newAESCTR(deriveKey(reversed[:32], sec.Key[:]), decIV)
	if err != nil {
		return nil, nil, nil, err
	}

	wire = make([]byte, obfuscatedFrameLen)
	copy(wire, frame)
	enc.XORKeyStream(wire, wire)
	copy(wire[0:56], frame[0:56])
	return wire, enc, dec, nil
}

func reqPQFrame() []byte {
	body := make([]byte, 20)
	binary.LittleEndian.PutUint32(body[0:4], 0x60469778)
	_, _ = rand.Read(body[4:20])

	now := time.Now()
	frac := (uint64(now.Nanosecond()) << 32) / uint64(time.Second/time.Nanosecond)
	msg := make([]byte, 0, 40)
	msg = append(msg, make([]byte, 8)...)
	msgID := make([]byte, 8)
	binary.LittleEndian.PutUint64(msgID, (uint64(now.Unix())<<32)|(frac&0xfffffffc))
	msg = append(msg, msgID...)
	l := make([]byte, 4)
	binary.LittleEndian.PutUint32(l, uint32(len(body)))
	msg = append(msg, l...)
	msg = append(msg, body...)

	out := make([]byte, 0, 1+len(msg))
	out = append(out, byte(len(msg)/4))
	return append(out, msg...)
}

func writeTLSRecord(w io.Writer, payload []byte) error {
	rec := make([]byte, 5+len(payload))
	rec[0] = tlsRecordAppData
	rec[1] = 0x03
	rec[2] = 0x03
	binary.BigEndian.PutUint16(rec[3:5], uint16(len(payload)))
	copy(rec[5:], payload)
	_, err := w.Write(rec)
	return err
}

// readTLSAppData returns the first application-data record, stepping over the
// server's canned handshake and change-cipher records the way a fake-TLS client
// does.
func readTLSAppData(r io.Reader) ([]byte, error) {
	for {
		hdr := make([]byte, 5)
		if _, err := io.ReadFull(r, hdr); err != nil {
			return nil, err
		}
		n := int(binary.BigEndian.Uint16(hdr[3:5]))
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		if hdr[0] == tlsRecordAppData {
			return buf, nil
		}
	}
}
