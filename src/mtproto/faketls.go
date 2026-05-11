package mtproto

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	tlsRecordHandshake    = 0x16
	tlsRecordChangeCipher = 0x14
	tlsRecordAppData      = 0x17
	handshakeClientHello  = 0x01
	handshakeServerHello  = 0x02
	maxTLSRecordPayload  = 16379
	timestampTolerance   = 120
	secondDuration       = time.Second
)

type FakeTLSConn struct {
	net.Conn
	readBuf []byte
}

func (c *FakeTLSConn) Read(p []byte) (int, error) {
	if len(c.readBuf) > 0 {
		n := copy(p, c.readBuf)
		c.readBuf = c.readBuf[n:]
		return n, nil
	}

	for {
		hdr := make([]byte, 5)
		if _, err := io.ReadFull(c.Conn, hdr); err != nil {
			return 0, err
		}

		payloadLen := int(binary.BigEndian.Uint16(hdr[3:5]))
		if payloadLen > maxTLSRecordPayload+256 {
			return 0, fmt.Errorf("TLS record too large: %d", payloadLen)
		}

		if hdr[0] == tlsRecordChangeCipher {
			discard := make([]byte, payloadLen)
			if _, err := io.ReadFull(c.Conn, discard); err != nil {
				return 0, err
			}
			continue
		}

		if hdr[0] != tlsRecordAppData {
			return 0, fmt.Errorf("unexpected TLS record type 0x%02x", hdr[0])
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(c.Conn, payload); err != nil {
			return 0, err
		}

		n := copy(p, payload)
		if n < len(payload) {
			c.readBuf = payload[n:]
		}
		return n, nil
	}
}

func (c *FakeTLSConn) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		chunk := len(p)
		if chunk > maxTLSRecordPayload {
			chunk = maxTLSRecordPayload
		}
		rec := make([]byte, 5+chunk)
		rec[0] = tlsRecordAppData
		rec[1] = 0x03
		rec[2] = 0x03
		binary.BigEndian.PutUint16(rec[3:5], uint16(chunk))
		copy(rec[5:], p[:chunk])

		if _, err := c.Conn.Write(rec); err != nil {
			return total, err
		}
		total += chunk
		p = p[chunk:]
	}
	return total, nil
}

// FakeTLSVerifyError signals that a TLS-shaped ClientHello arrived but
// failed verification. Initial holds the bytes already read so the caller
// can forward them to a masking domain (anti-prober disguise).
type FakeTLSVerifyError struct {
	Err     error
	Initial []byte
}

func (e *FakeTLSVerifyError) Error() string { return e.Err.Error() }
func (e *FakeTLSVerifyError) Unwrap() error { return e.Err }

func AcceptFakeTLS(conn net.Conn, secret *Secret) (*FakeTLSConn, error) {
	recHdr := make([]byte, 5)
	if _, err := io.ReadFull(conn, recHdr); err != nil {
		return nil, fmt.Errorf("read TLS record header: %w", err)
	}
	if recHdr[0] != tlsRecordHandshake {
		return nil, fmt.Errorf("not a TLS handshake: type 0x%02x", recHdr[0])
	}

	recLen := int(binary.BigEndian.Uint16(recHdr[3:5]))
	if recLen < 39 || recLen > 16384 {
		return nil, &FakeTLSVerifyError{Err: fmt.Errorf("invalid ClientHello length: %d", recLen), Initial: recHdr}
	}

	body := make([]byte, recLen)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("read ClientHello body: %w", err)
	}

	clientHello := append(recHdr, body...)

	if body[0] != handshakeClientHello {
		return nil, &FakeTLSVerifyError{Err: fmt.Errorf("not a ClientHello: type 0x%02x", body[0]), Initial: clientHello}
	}

	if len(body) < 38 {
		return nil, &FakeTLSVerifyError{Err: fmt.Errorf("ClientHello too short"), Initial: clientHello}
	}
	clientRandom := make([]byte, 32)
	copy(clientRandom, body[6:38])

	zeroed := make([]byte, len(clientHello))
	copy(zeroed, clientHello)
	for i := 0; i < 32; i++ {
		zeroed[11+i] = 0
	}

	mac := hmac.New(sha256.New, secret.Key[:])
	mac.Write(zeroed)
	expected := mac.Sum(nil)

	for i := 0; i < 28; i++ {
		if clientRandom[i] != expected[i] {
			return nil, &FakeTLSVerifyError{Err: fmt.Errorf("HMAC verification failed at byte %d", i), Initial: clientHello}
		}
	}

	tsBytes := make([]byte, 4)
	for i := 0; i < 4; i++ {
		tsBytes[i] = clientRandom[28+i] ^ expected[28+i]
	}
	ts := binary.LittleEndian.Uint32(tsBytes)
	now := uint32(time.Now().Unix())
	diff := int64(now) - int64(ts)
	if diff < 0 {
		diff = -diff
	}
	if diff > timestampTolerance {
		return nil, &FakeTLSVerifyError{Err: fmt.Errorf("timestamp out of range: diff=%ds", diff), Initial: clientHello}
	}

	sessionID := extractSessionID(body)

	serverHello := buildServerHello(secret, clientRandom, sessionID)
	if _, err := conn.Write(serverHello); err != nil {
		return nil, fmt.Errorf("write ServerHello: %w", err)
	}

	return &FakeTLSConn{Conn: conn}, nil
}

func proxyToMaskingDomain(client net.Conn, initial []byte, host string, mark uint) {
	if host == "" {
		return
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if mark > 0 {
		dialer.Control = func(network, address string, c syscall.RawConn) error {
			var sErr error
			if err := c.Control(func(fd uintptr) {
				sErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_MARK, int(mark))
			}); err != nil {
				return err
			}
			return sErr
		}
	}
	upstream, err := dialer.Dial("tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return
	}
	defer upstream.Close()

	if len(initial) > 0 {
		if _, err := upstream.Write(initial); err != nil {
			return
		}
	}

	_ = client.SetDeadline(time.Time{})

	done := make(chan struct{}, 2)
	go func() { io.Copy(upstream, client); done <- struct{}{} }()
	go func() { io.Copy(client, upstream); done <- struct{}{} }()
	<-done
	upstream.Close()
	client.Close()
	<-done
}

func extractSessionID(helloBody []byte) []byte {
	if len(helloBody) < 39 {
		return nil
	}
	sessLen := int(helloBody[38])
	if sessLen == 0 || len(helloBody) < 39+sessLen {
		return nil
	}
	sid := make([]byte, sessLen)
	copy(sid, helloBody[39:39+sessLen])
	return sid
}

func buildServerHello(secret *Secret, clientRandom, sessionID []byte) []byte {
	if sessionID == nil {
		sessionID = make([]byte, 32)
		rand.Read(sessionID)
	}

	var shBody bytes.Buffer
	shBody.Write([]byte{0x03, 0x03})
	shBody.Write(make([]byte, 32))
	shBody.WriteByte(byte(len(sessionID)))
	shBody.Write(sessionID)
	shBody.Write([]byte{0x13, 0x01})
	shBody.WriteByte(0x00)

	x25519Key := make([]byte, 32)
	rand.Read(x25519Key)

	var extensions bytes.Buffer
	extensions.Write([]byte{0x00, 0x2b, 0x00, 0x02, 0x03, 0x04})
	keyShareData := make([]byte, 0, 36)
	keyShareData = append(keyShareData, 0x00, 0x1d, 0x00, 0x20)
	keyShareData = append(keyShareData, x25519Key...)
	extensions.Write([]byte{0x00, 0x33})
	extLen := len(keyShareData)
	extensions.Write([]byte{byte(extLen >> 8), byte(extLen)})
	extensions.Write(keyShareData)

	extBytes := extensions.Bytes()
	shBody.Write([]byte{byte(len(extBytes) >> 8), byte(len(extBytes))})
	shBody.Write(extBytes)

	shBytes := shBody.Bytes()
	hsLen := len(shBytes)
	var shRecord bytes.Buffer
	shRecord.Write([]byte{tlsRecordHandshake, 0x03, 0x03})
	recLen := 4 + hsLen
	shRecord.Write([]byte{byte(recLen >> 8), byte(recLen)})
	shRecord.Write([]byte{handshakeServerHello, byte(hsLen >> 16), byte(hsLen >> 8), byte(hsLen)})
	shRecord.Write(shBytes)

	changeCipher := []byte{tlsRecordChangeCipher, 0x03, 0x03, 0x00, 0x01, 0x01}

	var nb [2]byte
	rand.Read(nb[:])
	noiseLen := 1900 + int(binary.BigEndian.Uint16(nb[:]))%201
	noise := make([]byte, noiseLen)
	rand.Read(noise)
	var noiseRecord bytes.Buffer
	noiseRecord.Write([]byte{tlsRecordAppData, 0x03, 0x03})
	noiseRecord.Write([]byte{byte(noiseLen >> 8), byte(noiseLen)})
	noiseRecord.Write(noise)

	var full bytes.Buffer
	full.Write(shRecord.Bytes())
	full.Write(changeCipher)
	full.Write(noiseRecord.Bytes())

	fullBytes := full.Bytes()

	randomOffset := findServerRandomOffset(fullBytes)
	if randomOffset < 0 {
		return fullBytes
	}

	for i := 0; i < 32; i++ {
		fullBytes[randomOffset+i] = 0
	}

	mac := hmac.New(sha256.New, secret.Key[:])
	mac.Write(clientRandom)
	mac.Write(fullBytes)
	serverRandom := mac.Sum(nil)

	copy(fullBytes[randomOffset:randomOffset+32], serverRandom)

	return fullBytes
}

func findServerRandomOffset(data []byte) int {
	if len(data) < 11+32 {
		return -1
	}
	return 11
}

