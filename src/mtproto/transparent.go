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
	dc, ok := dcForIP(origIP)
	if !ok {
		return false, client
	}

	_ = client.SetReadDeadline(time.Now().Add(15 * time.Second))
	first := make([]byte, 1)
	if _, err := io.ReadFull(client, first); err != nil {
		return true, nil
	}
	if !looksObfuscated2(first[0]) {
		log.Debugf("MTProto transparent: non-obfuscated transport (0x%02x) for DC%d from %s -> fail open", first[0], dc, origIP)
		return false, &prefixConn{Conn: client, prefix: first}
	}

	res, err := AcceptObfuscatedDirect(&prefixConn{Conn: client, prefix: first})
	if err != nil {
		log.Debugf("MTProto transparent: obfuscated accept failed for DC%d from %s: %v", dc, origIP, err)
		return true, nil
	}
	_ = client.SetReadDeadline(time.Time{})

	cfg := b.cfg.Load()
	mtCfg := cfg.System.MTProto
	if mtCfg.UpstreamMode != "ws" && mtCfg.UpstreamMode != "auto" {
		mtCfg.UpstreamMode = "auto"
	}

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
	log.Infof("MTProto transparent relay: %s -> DC%d (%s)", origIP, dc, transport)

	var splitter *msgSplitter
	if _, isWS := dcConn.Conn.(*wsConn); isWS {
		splitter = newMsgSplitter(res.ProtoTag)
	}
	relayConns(res.Conn, dcConn, splitter, label, &b.bufPool)
	return true, nil
}

func looksObfuscated2(b byte) bool {
	switch b {
	case 0xef, 0xee, 0xdd, 0x16, 0x44, 0x47, 0x48, 0x4f, 0x50, 0x43, 0x54:
		return false
	}
	return true
}
