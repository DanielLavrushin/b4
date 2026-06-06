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
	id := nextConnID()
	tag := tg(id)
	log.Tracef("%s bridge accept %s -> %s:%d", tag, client.RemoteAddr(), origIP, origPort)
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	init := make([]byte, obfuscatedFrameLen)
	head, herr := io.ReadFull(client, init[:4])
	if herr != nil {
		_ = client.SetReadDeadline(time.Time{})
		if head == 0 {
			log.Tracef("%s bridge empty conn from %s -> drop", tag, origIP)
			return true, nil
		}
		log.Debugf("%s bridge short head (%d B) from %s:%d -> fail open", tag, head, origIP, origPort)
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init[:head]...)}
	}
	if reservedFirst4(init[:4]) {
		_ = client.SetReadDeadline(time.Time{})
		log.Debugf("%s bridge non-obfuscated transport (% x) from %s:%d -> fail open", tag, init[:4], origIP, origPort)
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init[:4]...)}
	}
	n, rerr := io.ReadFull(client, init[4:])
	_ = client.SetReadDeadline(time.Time{})
	if rerr != nil {
		log.Debugf("%s bridge short handshake (%d/%d B) from %s:%d -> fail open", tag, 4+n, obfuscatedFrameLen, origIP, origPort)
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init[:4+n]...)}
	}

	res, derr := decodeObfuscatedDirect(init, client)
	if derr != nil {
		log.Debugf("%s bridge obfuscated decode failed from %s:%d: %v -> fail open", tag, origIP, origPort, derr)
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init...)}
	}
	log.Tracef("%s bridge handshake ok from %s:%d: proto=0x%08x handshake-dc=%d", tag, origIP, origPort, res.ProtoTag, res.DC)

	var dc int
	var dcSrc string
	if mapped, ok := dcForIP(origIP); ok {
		dc, dcSrc = mapped, "ip"
	} else if validTransparentDC(res.DC) {
		dc, dcSrc = res.DC, "handshake"
	} else if mapped, ok := dcForIPRange(origIP); ok {
		dc, dcSrc = mapped, "ip-range"
	} else {
		log.Debugf("%s bridge unresolved DC for %s:%d (handshake dc=%d proto=0x%08x) -> fail open", tag, origIP, origPort, res.DC, res.ProtoTag)
		return false, &prefixConn{Conn: client, prefix: append([]byte(nil), init...)}
	}
	if rng, ok := dcForIPRange(origIP); ok && validTransparentDC(res.DC) && rng != res.DC {
		log.Debugf("%s bridge DC ambiguity for %s: ip-range=DC%d handshake=DC%d -> using DC%d (src=%s)", tag, origIP, rng, res.DC, dc, dcSrc)
	}

	cfg := b.cfg.Load()
	mtCfg := cfg.System.MTProto
	mtCfg.UpstreamMode = "auto"
	mtCfg.DCRelay = ""
	mtCfg.BridgeSkipNativeEdge = true

	dcConn, transport, err := DialObfuscatedDCWithPool(&mtCfg, cfg.Queue, dc, res.ProtoTag, nil, id)
	if err != nil {
		if shouldLogDialError(dc) {
			log.Errorf("%s bridge dial DC %d failed: %v", tag, dc, err)
		} else {
			log.Debugf("%s bridge dial DC %d failed (suppressed): %v", tag, dc, err)
		}
		return true, nil
	}
	defer dcConn.Close()

	label := fmt.Sprintf("%s %s<->DC%d(transparent)", tag, client.RemoteAddr(), dc)
	log.Infof("%s bridge relay %s:%d -> DC%d via %s [dc-from=%s]", tag, origIP, origPort, dc, transport, dcSrc)

	var splitter *msgSplitter
	if _, isWS := dcConn.Conn.(*wsConn); isWS {
		splitter = newMsgSplitter(res.ProtoTag)
	}
	relayConns(res.Conn, dcConn, splitter, label, &b.bufPool)
	return true, nil
}

func (b *TransparentBridge) FailOpenViaWorker(client net.Conn, origIP net.IP, origPort int) bool {
	cfg := b.cfg.Load()
	mt := cfg.System.MTProto
	domains := workerDomains(&mt)
	if len(domains) == 0 {
		return false
	}
	id := nextConnID()
	tag := tg(id)
	dst := origIP.String()
	dc := 0
	if m, ok := dcForIP(origIP); ok {
		dc = m
	} else if m, ok := dcForIPRange(origIP); ok {
		dc = m
	}
	for _, wd := range domains {
		path := fmt.Sprintf("/apiws?dst=%s&dc=%d&port=%d", dst, dc, origPort)
		wc, derr := dialWS(wd, wd, path, wsDialTimeout, cfg.Queue.Mark)
		if derr != nil {
			log.Debugf("%s failopen worker dial %s for %s:%d failed: %v", tag, wd, dst, origPort, derr)
			continue
		}
		log.Infof("%s failopen relay %s:%d via wsworker://%s", tag, dst, origPort, wd)
		label := fmt.Sprintf("%s %s<->%s:%d(failopen)", tag, client.RemoteAddr(), dst, origPort)
		relayConns(client, wc, nil, label, &b.bufPool)
		return true
	}
	return false
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
