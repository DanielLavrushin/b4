package mtproto

import (
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const transparentBufSize = 65536

type prefixConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

func (c *prefixConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

type TransparentBridge struct {
	cfg     atomic.Pointer[config.Config]
	bufPool sync.Pool

	mu       sync.Mutex
	pool     *wsPool
	poolInit bool
}

func NewTransparentBridge(cfg *config.Config) *TransparentBridge {
	b := &TransparentBridge{
		bufPool: sync.Pool{New: func() interface{} {
			buf := make([]byte, transparentBufSize)
			return &buf
		}},
	}
	b.cfg.Store(cfg)
	return b
}

func (b *TransparentBridge) UpdateConfig(newCfg *config.Config) {
	old := b.cfg.Swap(newCfg)
	if old != nil &&
		old.System.MTProto.WSEndpointHost == newCfg.System.MTProto.WSEndpointHost &&
		old.System.MTProto.WSCustomDomain == newCfg.System.MTProto.WSCustomDomain &&
		old.Queue.Mark == newCfg.Queue.Mark {
		return
	}
	b.mu.Lock()
	oldPool := b.pool
	b.pool = nil
	b.poolInit = false
	b.mu.Unlock()
	if oldPool != nil {
		oldPool.close()
	}
}

func (b *TransparentBridge) getPool() *wsPool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.poolInit {
		cfg := b.cfg.Load()
		mt := cfg.System.MTProto
		p := newWSPool(MTProtoUpstream{
			WSEndpointHost: mt.WSEndpointHost,
			WSCustomDomain: mt.WSCustomDomain,
		}, cfg.Queue.Mark, wsPoolDefaultSize)
		p.warmup([]int{2, 4})
		b.pool = p
		b.poolInit = true
	}
	return b.pool
}

func (b *TransparentBridge) Handle(client net.Conn, origIP net.IP, origPort int) (bool, net.Conn) {
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	init := make([]byte, obfuscatedFrameLen)
	head, herr := io.ReadFull(client, init[:4])
	if herr != nil {
		_ = client.SetReadDeadline(time.Time{})
		if head == 0 {
			return true, nil
		}
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init[:head]...)}
	}
	if reservedFirst4(init[:4]) {
		_ = client.SetReadDeadline(time.Time{})
		log.Debugf("MTProto transparent: non-obfuscated transport (% x) from %s -> fail open", init[:4], origIP)
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init[:4]...)}
	}
	n, rerr := io.ReadFull(client, init[4:])
	_ = client.SetReadDeadline(time.Time{})
	if rerr != nil {
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init[:4+n]...)}
	}

	res, derr := decodeObfuscatedDirect(init, client)
	if derr != nil {
		log.Debugf("MTProto transparent: obfuscated decode failed from %s: %v -> fail open", origIP, derr)
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init...)}
	}

	var dc int
	var dcSrc string
	if mapped, ok := dcForIP(origIP); ok {
		dc, dcSrc = mapped, "ip"
	} else if mapped, ok := dcForIPRange(origIP); ok {
		dc, dcSrc = mapped, "ip-range"
	} else if validTransparentDC(res.DC) {
		dc, dcSrc = res.DC, "handshake"
	} else {
		log.Debugf("MTProto transparent: unresolved DC for %s (handshake dc=%d proto=0x%08x) -> fail open", origIP, res.DC, res.ProtoTag)
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init...)}
	}

	cfg := b.cfg.Load()
	mtCfg := cfg.System.MTProto
	mtCfg.UpstreamMode = "auto"
	mtCfg.DCRelay = ""

	dcConn, transport, err := DialObfuscatedDCWithPool(&mtCfg, cfg.Queue, dc, res.ProtoTag, b.getPool())
	if err != nil {
		if shouldLogDialError(dc) {
			log.Errorf("MTProto transparent dial DC %d: %v", dc, err)
		} else {
			log.Debugf("MTProto transparent dial DC %d (suppressed): %v", dc, err)
		}
		return true, nil
	}
	defer dcConn.Close()

	label := fmt.Sprintf("%s<->DC%d(transparent)", client.RemoteAddr(), dc)
	log.Infof("MTProto transparent relay: %s -> DC%d (%s) [dc-from=%s]", origIP, dc, transport, dcSrc)

	var splitter *msgSplitter
	if _, isWS := dcConn.Conn.(*wsConn); isWS {
		splitter = newMsgSplitter(res.ProtoTag)
	}
	relayConns(res.Conn, dcConn, splitter, label, &b.bufPool)
	return true, nil
}

func validTransparentDC(dc int) bool {
	a := dc
	if a < 0 {
		a = -a
	}
	return (a >= 1 && a <= 5) || a == 203
}

func reservedFirst4(b []byte) bool {
	return isReservedFirst4(b)
}
