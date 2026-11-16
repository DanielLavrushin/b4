package mtproto

import (
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func probeForwarder(t *testing.T, upstream string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.DialTimeout("tcp", upstream, 5*time.Second)
			if err != nil {
				_ = c.Close()
				continue
			}
			wg.Add(2)
			go pipe(up, c)
			go pipe(c, up)
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listener addr: %v", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("listener port: %v", err)
	}
	return n
}

func TestRelayProbe(t *testing.T) {
	spec := os.Getenv("B4_RELAY_PROBE")
	if spec == "" {
		t.Skip("set B4_RELAY_PROBE=host:baseport of a DC Relay, or B4_RELAY_PROBE=local for a forwarder on this machine")
	}
	dc := 2
	if s := os.Getenv("B4_RELAY_PROBE_DC"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("B4_RELAY_PROBE_DC: %v", err)
		}
		dc = v
	}
	rounds := 3
	if s := os.Getenv("B4_RELAY_PROBE_ROUNDS"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			rounds = v
		}
	}

	wsResetState()
	t.Cleanup(wsResetState)
	withCleanRelayState(t)
	withWSDialPort(t, blackholeListener(t))

	dcAddr, err := ResolveDC(dc, false, "")
	if err != nil {
		t.Fatalf("DC %d: %v", dc, err)
	}
	dcHost, _, err := net.SplitHostPort(dcAddr)
	if err != nil {
		t.Fatalf("DC %d address %q: %v", dc, dcAddr, err)
	}

	relay := spec
	if spec == "local" {
		port := probeForwarder(t, dcAddr)
		relay = net.JoinHostPort("127.0.0.1", strconv.Itoa(port-dcAbs(dc)+1))
	}
	cfg := &config.MTProtoConfig{UpstreamMode: "auto", WSEndpointHost: "127.0.0.1", DCRelay: relay}
	target := dialTarget{ip: dcHost, port: dcDefaultPort}

	for i := 1; i <= rounds; i++ {
		start := time.Now()
		conn, info, err := dialObfuscatedDC(cfg, config.QueueConfig{IPv4Enabled: true}, dc, connectionTagAbridged, nil, "", target)
		dialled := time.Since(start)
		if err != nil {
			t.Fatalf("round %d: dial failed after %v: %v", i, dialled, err)
		}
		if !info.plan.relay {
			_ = conn.Close()
			t.Fatalf("round %d: connected via %s in %v, not the relay", i, info.transport, dialled)
		}
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := conn.Write(reqPQFrame()); err != nil {
			_ = conn.Close()
			t.Fatalf("round %d: req_pq: %v", i, err)
		}
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		answered := time.Since(start)
		_ = conn.Close()
		if err != nil {
			t.Fatalf("round %d: DC %d answered nothing through %s: %v", i, dc, info.transport, err)
		}
		if err := checkResPQ(buf[:n]); err != nil {
			t.Fatalf("round %d: DC %d through %s: %v", i, dc, info.transport, err)
		}
		if earn := relayAnswered(info); earn != nil {
			earn()
		}
		t.Logf("round %d: DC %d via %s, dialled in %v, res_pq at %v", i, dc, info.transport, dialled.Round(time.Millisecond), answered.Round(time.Millisecond))
	}

	settled := time.Now().Add(wsDialTimeout + 2*time.Second)
	for !wsCooldownActive(dc) && time.Now().Before(settled) {
		time.Sleep(50 * time.Millisecond)
	}
}
