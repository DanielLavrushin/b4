package mtproto

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"os"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

// TestWorkerProbe is a manual diagnostic, not part of the normal suite. It dials
// a real Cloudflare Worker and measures the two things the pool work depends on:
// how long an idle Worker WebSocket survives, and what a warm conn saves over a
// fresh dial. Run with B4_WORKER_PROBE=<worker-domain>.
func TestWorkerProbe(t *testing.T) {
	domain := os.Getenv("B4_WORKER_PROBE")
	if domain == "" {
		t.Skip("set B4_WORKER_PROBE=<domain> to run")
	}
	pl := transportPlan{
		kind:     transportWS,
		dc:       2,
		sni:      domain,
		dialHost: domain,
		wsPath:   "/apiws?dst=149.154.167.51&dc=2",
		isWorker: true,
	}

	t.Run("dial-latency", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			start := time.Now()
			c, err := dialWS(pl.dialHost, pl.sni, pl.wsPath, wsDialTimeout, 0)
			if err != nil {
				t.Fatalf("fresh dial %d failed: %v", i, err)
			}
			t.Logf("fresh dial %d: %dms", i, time.Since(start).Milliseconds())
			_ = c.Close()
		}
	})

	t.Run("pooled-latency", func(t *testing.T) {
		p := newCFWorkerPool(0)
		defer p.close()

		c, err := dialWS(pl.dialHost, pl.sni, pl.wsPath, wsDialTimeout, 0)
		if err != nil {
			t.Fatalf("seed dial failed: %v", err)
		}
		_ = c.Close()

		p.warm(pl)
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if got := p.get(pl); got != nil {
				t.Logf("pool hit after warm")
				start := time.Now()
				if _, err := completeObfuscation(got, 2, connectionTagAbridged); err != nil {
					t.Fatalf("obf on pooled conn: %v", err)
				}
				t.Logf("pooled handshake write: %dms (vs a full fresh dial above)", time.Since(start).Milliseconds())
				_ = got.Close()
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("pool never warmed")
	})

	// reqPQ drives a real MTProto exchange over an already-obfuscated conn:
	// an unencrypted req_pq_multi in abridged framing, answered with resPQ.
	// Getting resPQ back proves the transport carries live Telegram traffic,
	// which a successful dial on its own does not.
	reqPQ := func(t *testing.T, c *ObfuscatedConn) {
		t.Helper()
		var payload []byte
		payload = binary.LittleEndian.AppendUint64(payload, 0)                             // auth_key_id = 0
		payload = binary.LittleEndian.AppendUint64(payload, uint64(time.Now().Unix())<<32) // message_id
		payload = binary.LittleEndian.AppendUint32(payload, 20)                            // message_data_length
		payload = binary.LittleEndian.AppendUint32(payload, 0xbe7e8ef1)                    // req_pq_multi
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		payload = append(payload, nonce...)

		frame := append([]byte{byte(len(payload) / 4)}, payload...) // abridged framing
		if _, err := c.Write(frame); err != nil {
			t.Fatalf("write req_pq_multi: %v", err)
		}

		_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
		buf := make([]byte, 4096)
		n, err := c.Read(buf)
		if err != nil {
			t.Fatalf("read resPQ: %v", err)
		}
		if n < 25 {
			t.Fatalf("resPQ too short: %d bytes", n)
		}
		// skip abridged length byte, auth_key_id, message_id, length
		ctor := binary.LittleEndian.Uint32(buf[21:25])
		if ctor != 0x05162463 {
			t.Fatalf("expected resPQ (0x05162463), got 0x%08x in % x", ctor, buf[:n])
		}
		t.Logf("resPQ received, %d bytes - transport carries live Telegram traffic", n)
	}

	t.Run("req-pq-fresh", func(t *testing.T) {
		c, err := dialWS(pl.dialHost, pl.sni, pl.wsPath, wsDialTimeout, 0)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer c.Close()
		obf, err := completeObfuscation(c, 2, connectionTagAbridged)
		if err != nil {
			t.Fatalf("obf: %v", err)
		}
		reqPQ(t, obf)
	})

	t.Run("req-pq-pooled", func(t *testing.T) {
		p := newCFWorkerPool(0)
		defer p.close()

		c, err := dialWS(pl.dialHost, pl.sni, pl.wsPath, wsDialTimeout, 0)
		if err != nil {
			t.Fatalf("seed dial: %v", err)
		}
		_ = c.Close()
		p.warm(pl)

		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			raw := p.get(pl)
			if raw == nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			defer raw.Close()
			obf, err := completeObfuscation(raw, 2, connectionTagAbridged)
			if err != nil {
				t.Fatalf("obf on pooled conn: %v", err)
			}
			if !raw.liveNow() {
				t.Fatal("pooled conn reported dead right after handshake")
			}
			reqPQ(t, obf)
			return
		}
		t.Fatal("pool never warmed")
	})

	t.Run("bridge-path", func(t *testing.T) {
		cfg := &config.MTProtoConfig{
			UpstreamMode:         "ws",
			CFWorkerDomain:       domain,
			BridgeSkipNativeEdge: true,
		}
		pools := &dialPools{worker: newCFWorkerPool(0)}
		defer pools.worker.close()

		for i := 0; i < 3; i++ {
			start := time.Now()
			conn, info, err := dialObfuscatedDC(cfg, config.QueueConfig{}, 2, connectionTagAbridged, pools, "probe")
			if err != nil {
				t.Fatalf("dial %d: %v", i, err)
			}
			t.Logf("dial %d: %dms via %s isWorker=%v splitter=%v",
				i, time.Since(start).Milliseconds(), info.transport, info.isWorker,
				newSplitterFor(conn, info, connectionTagAbridged) != nil)
			_ = conn.Close()
			time.Sleep(time.Second)
		}
	})

	// bridge-live speaks to a Telegram DC exactly as a Telegram client would.
	// When a running b4 has that address in an mtproto-ws routing set the
	// connection is diverted into the transparent bridge, so a resPQ coming back
	// proves the whole live path - divert, bridge handshake, Worker pool, relay.
	// Check the b4 log to confirm it was diverted rather than going out direct.
	t.Run("bridge-live", func(t *testing.T) {
		target := os.Getenv("B4_BRIDGE_LIVE")
		if target == "" {
			t.Skip("set B4_BRIDGE_LIVE=<dc-ip:443> to run against a live b4")
		}
		for i := 0; i < 3; i++ {
			start := time.Now()
			raw, err := net.DialTimeout("tcp", target, 10*time.Second)
			if err != nil {
				t.Fatalf("dial %d: %v", i, err)
			}
			obf, err := completeObfuscation(raw, 2, connectionTagAbridged)
			if err != nil {
				raw.Close()
				t.Fatalf("obf %d: %v", i, err)
			}
			reqPQ(t, obf)
			t.Logf("bridge-live %d: resPQ round trip in %dms", i, time.Since(start).Milliseconds())
			raw.Close()
			time.Sleep(time.Second)
		}
	})

	t.Run("idle-lifetime", func(t *testing.T) {
		if os.Getenv("B4_WORKER_PROBE_IDLE") == "" {
			t.Skip("set B4_WORKER_PROBE_IDLE=1 (takes minutes)")
		}
		type variant struct {
			name string
			ping bool
			// a pooled conn is dialled but left un-handshaked until a client
			// claims it, so its lifetime is the one that sets the pool max age
			handshake bool
		}
		for _, v := range []variant{
			{name: "handshaked-no-ping", handshake: true},
			{name: "handshaked-ping-30s", handshake: true, ping: true},
			{name: "pooled-no-handshake"},
		} {
			ping := v.ping
			t.Run(v.name, func(t *testing.T) {
				c, err := dialWS(pl.dialHost, pl.sni, pl.wsPath, wsDialTimeout, 0)
				if err != nil {
					t.Fatalf("dial: %v", err)
				}
				defer c.Close()
				wsc := c.(*wsConn)
				if v.handshake {
					if _, err := completeObfuscation(wsc, 2, connectionTagAbridged); err != nil {
						t.Fatalf("obf: %v", err)
					}
				}

				stop := make(chan struct{})
				defer close(stop)
				if ping {
					go func() {
						tk := time.NewTicker(30 * time.Second)
						defer tk.Stop()
						for {
							select {
							case <-stop:
								return
							case <-tk.C:
								wsc.tryWriteControl(wsOpcodePing, nil)
							}
						}
					}()
				}

				start := time.Now()
				_ = wsc.SetReadDeadline(time.Now().Add(6 * time.Minute))
				buf := make([]byte, 4096)
				n, rerr := wsc.Read(buf)
				t.Logf("%s: idle conn ended after %.1fs (n=%d err=%v)", v.name, time.Since(start).Seconds(), n, rerr)
			})
		}
	})
}
