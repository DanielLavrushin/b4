package mtproto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

const (
	obfuscatedFrameLen    = 64
	connectionTagAbridged = 0xefefefef
	connectionTagInter    = 0xeeeeeeee
	connectionTagPadded   = 0xdddddddd

	telegramWSEdgeIP = "149.154.167.220"
	wsDialTimeout    = 8 * time.Second
	tcpDialTimeout   = 8 * time.Second
)

type ObfuscatedConn struct {
	net.Conn
	reader cipher.Stream
	writer cipher.Stream
}

func (c *ObfuscatedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.reader.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (c *ObfuscatedConn) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	c.writer.XORKeyStream(buf, p)
	return c.Conn.Write(buf)
}

type ClientHandshakeResult struct {
	DC       int
	ProtoTag uint32
	Conn     *ObfuscatedConn
}

func AcceptObfuscated(conn net.Conn, secret *Secret) (*ClientHandshakeResult, error) {
	frame := make([]byte, obfuscatedFrameLen)
	if _, err := io.ReadFull(conn, frame); err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}

	decKey := deriveKey(frame[8:40], secret.Key[:])
	decIV := make([]byte, 16)
	copy(decIV, frame[40:56])
	decStream, err := newAESCTR(decKey, decIV)
	if err != nil {
		return nil, fmt.Errorf("init decrypt: %w", err)
	}

	reversed := make([]byte, 48)
	for i := 0; i < 48; i++ {
		reversed[i] = frame[55-i]
	}
	encKey := deriveKey(reversed[:32], secret.Key[:])
	encIV := make([]byte, 16)
	copy(encIV, reversed[32:48])
	encStream, err := newAESCTR(encKey, encIV)
	if err != nil {
		return nil, fmt.Errorf("init encrypt: %w", err)
	}

	decrypted := make([]byte, obfuscatedFrameLen)
	copy(decrypted, frame)
	decStream.XORKeyStream(decrypted, decrypted)

	tag := binary.LittleEndian.Uint32(decrypted[56:60])
	switch tag {
	case connectionTagAbridged, connectionTagInter, connectionTagPadded:
	default:
		return nil, fmt.Errorf("invalid connection tag: 0x%08x", tag)
	}

	dc := int(int16(binary.LittleEndian.Uint16(decrypted[60:62])))

	return &ClientHandshakeResult{
		DC:       dc,
		ProtoTag: tag,
		Conn: &ObfuscatedConn{
			Conn:   conn,
			reader: decStream,
			writer: encStream,
		},
	}, nil
}

type transportKind int

const (
	transportTCP transportKind = iota
	transportWS
)

type transportPlan struct {
	kind     transportKind
	addr     string
	sni      string
	dialHost string
}

func (p transportPlan) describe() string {
	switch p.kind {
	case transportWS:
		if p.dialHost != "" && p.dialHost != p.sni {
			return fmt.Sprintf("ws://%s@%s", p.sni, p.dialHost)
		}
		return "ws://" + p.sni
	default:
		return "tcp://" + p.addr
	}
}

func planTransports(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int) ([]transportPlan, error) {
	absDC := dc
	if absDC < 0 {
		absDC = -absDC
	}

	mode := cfg.UpstreamMode
	if mode == "" {
		mode = "tcp"
	}

	var plans []transportPlan
	if mode == "ws" || mode == "auto" {
		edgeIP := strings.TrimSpace(cfg.WSEndpointHost)
		if edgeIP == "" {
			edgeIP = telegramWSEdgeIP
		}
		wsDC := absDC
		if absDC == 203 {
			wsDC = 2
		}
		if wsDC >= 1 && wsDC <= 5 {
			primary := transportPlan{kind: transportWS, sni: fmt.Sprintf("kws%d.web.telegram.org", wsDC), dialHost: edgeIP}
			media := transportPlan{kind: transportWS, sni: fmt.Sprintf("kws%d-1.web.telegram.org", wsDC), dialHost: edgeIP}
			if dc < 0 {
				plans = append(plans, media, primary)
			} else {
				plans = append(plans, primary, media)
			}
		}
		if d := strings.TrimSpace(cfg.WSCustomDomain); d != "" {
			plans = append(plans, transportPlan{
				kind: transportWS,
				sni:  fmt.Sprintf("kws%d.%s", absDC, d),
			})
		}
	}

	allowTCP := mode == "tcp" || mode == "auto" || len(plans) == 0
	if allowTCP {
		addr, err := ResolveDC(dc, queueCfg.IPv6Enabled, cfg.DCRelay)
		if err != nil {
			if len(plans) == 0 {
				return nil, err
			}
		} else {
			plans = append(plans, transportPlan{kind: transportTCP, addr: addr})
		}
	}

	if len(plans) == 0 {
		return nil, fmt.Errorf("no transports available for DC %d (mode=%s)", absDC, mode)
	}
	return plans, nil
}

func DialObfuscatedDC(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int, protoTag uint32) (*ObfuscatedConn, string, error) {
	plans, err := planTransports(cfg, queueCfg, dc)
	if err != nil {
		return nil, "", err
	}

	var lastErr error
	for _, p := range plans {
		log.Debugf("MTProto DC %d dialing %s", dc, p.describe())
		start := time.Now()
		conn, err := dialOne(p, queueCfg.Mark)
		if err != nil {
			lastErr = err
			log.Debugf("MTProto DC %d %s failed after %dms: %v", dc, p.describe(), time.Since(start).Milliseconds(), err)
			continue
		}
		obfConn, err := completeObfuscation(conn, dc, protoTag)
		if err != nil {
			lastErr = err
			conn.Close()
			log.Debugf("MTProto DC %d obf init failed on %s: %v", dc, p.describe(), err)
			continue
		}
		log.Infof("MTProto DC %d connected via %s in %dms", dc, p.describe(), time.Since(start).Milliseconds())
		return obfConn, p.describe(), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no transport succeeded")
	}
	return nil, "", lastErr
}

type TransportProbeResult struct {
	Transport string `json:"transport"`
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

func ProbeTransports(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int) ([]TransportProbeResult, error) {
	plans, err := planTransports(cfg, queueCfg, dc)
	if err != nil {
		return nil, err
	}
	out := make([]TransportProbeResult, 0, len(plans))
	for _, p := range plans {
		start := time.Now()
		conn, err := dialOne(p, queueCfg.Mark)
		ms := time.Since(start).Milliseconds()
		res := TransportProbeResult{Transport: p.describe()}
		if err != nil {
			res.OK = false
			res.Error = err.Error()
		} else {
			res.OK = true
			res.LatencyMs = ms
			_ = conn.Close()
		}
		out = append(out, res)
	}
	return out, nil
}

func dialOne(p transportPlan, mark uint) (net.Conn, error) {
	switch p.kind {
	case transportWS:
		host := p.dialHost
		if host == "" {
			host = p.sni
		}
		return dialWS(host, p.sni, wsDialTimeout, mark)
	default:
		dialer := net.Dialer{Timeout: tcpDialTimeout}
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
		conn, err := dialer.Dial("tcp", p.addr)
		if err != nil {
			return nil, err
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
		}
		return conn, nil
	}
}

func completeObfuscation(conn net.Conn, dc int, protoTag uint32) (*ObfuscatedConn, error) {
	frame := generateFrame(dc, protoTag)

	encKey := frame[8:40]
	encIV := make([]byte, 16)
	copy(encIV, frame[40:56])
	encStream, err := newAESCTR(encKey, encIV)
	if err != nil {
		return nil, fmt.Errorf("init encrypt: %w", err)
	}

	reversed := make([]byte, 48)
	for i := 0; i < 48; i++ {
		reversed[i] = frame[55-i]
	}
	decKey := reversed[:32]
	decIV := make([]byte, 16)
	copy(decIV, reversed[32:48])
	decStream, err := newAESCTR(decKey, decIV)
	if err != nil {
		return nil, fmt.Errorf("init decrypt: %w", err)
	}

	encrypted := make([]byte, obfuscatedFrameLen)
	copy(encrypted, frame)
	encStream.XORKeyStream(encrypted, encrypted)
	copy(encrypted[8:56], frame[8:56])

	if _, err := conn.Write(encrypted); err != nil {
		return nil, fmt.Errorf("send handshake: %w", err)
	}

	return &ObfuscatedConn{
		Conn:   conn,
		reader: decStream,
		writer: encStream,
	}, nil
}

func generateFrame(dc int, protoTag uint32) []byte {
	frame := make([]byte, obfuscatedFrameLen)
	for {
		if _, err := rand.Read(frame); err != nil {
			continue
		}

		if frame[0] == 0xef {
			continue
		}
		first4 := binary.LittleEndian.Uint32(frame[0:4])
		if first4 == 0x44414548 || first4 == 0x54534f50 ||
			first4 == 0x20544547 || first4 == 0x4954504f ||
			first4 == 0x02010316 || first4 == 0xdddddddd ||
			first4 == 0xeeeeeeee {
			continue
		}
		if binary.LittleEndian.Uint32(frame[4:8]) == 0 {
			continue
		}
		break
	}

	binary.LittleEndian.PutUint32(frame[56:60], protoTag)
	binary.LittleEndian.PutUint16(frame[60:62], uint16(int16(dc)))
	return frame
}

func deriveKey(rawKey []byte, secret []byte) []byte {
	h := sha256.New()
	h.Write(rawKey)
	h.Write(secret)
	return h.Sum(nil)
}

func newAESCTR(key, iv []byte) (cipher.Stream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, iv), nil
}
