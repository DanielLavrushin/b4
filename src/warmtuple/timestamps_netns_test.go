package warmtuple

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func requireWarmNetns(t *testing.T) {
	t.Helper()
	if os.Getenv("B4_NETNS_TEST") != "1" {
		t.Skip("set B4_NETNS_TEST=1 and run inside a network namespace (make test-netns)")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root inside the namespace")
	}
	self, err1 := os.Readlink("/proc/self/ns/net")
	init, err2 := os.Readlink("/proc/1/ns/net")
	if err1 == nil && err2 == nil && self == init {
		t.Fatal("refusing to run: this is the host network namespace, and the test writes net.ipv4.tcp_timestamps")
	}
	if out, err := exec.Command("ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		t.Skipf("cannot bring up lo: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

func drainingListener(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
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
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				_, _ = io.Copy(io.Discard, c)
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	return ln.Addr().(*net.TCPAddr).AddrPort()
}

func TestNetnsWarmNeedsTCPTimestamps(t *testing.T) {
	requireWarmNetns(t)
	original, err := os.ReadFile(tcpTimestampsSysctl)
	if err != nil {
		t.Skipf("this kernel has no %s in a network namespace: %v", tcpTimestampsSysctl, err)
	}
	if err := os.WriteFile(tcpTimestampsSysctl, original, 0o644); err != nil {
		t.Skipf("%s is not writable here: %v", tcpTimestampsSysctl, err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(tcpTimestampsSysctl, original, 0o644); err != nil {
			t.Errorf("restoring net.ipv4.tcp_timestamps to %s: %v", strings.TrimSpace(string(original)), err)
		}
	})
	target := drainingListener(t)
	for _, c := range []struct {
		value      string
		port       uint16
		timestamps bool
	}{{"1", 63000, true}, {"0", 63001, false}} {
		t.Run("tcp_timestamps="+c.value, func(t *testing.T) {
			if err := os.WriteFile(tcpTimestampsSysctl, []byte(c.value), 0o644); err != nil {
				t.Fatal(err)
			}
			m, err := New(Config{Host: "warm.example.org", Target: target, Source: netip.MustParseAddr("127.0.0.1"),
				FirstPort: c.port, LastPort: c.port, WarmTimeout: 5 * time.Second, ReuseTimeout: time.Second, RetryInterval: 20 * time.Millisecond, ReconnectDelay: 20 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			var warms atomic.Int32
			m.warm = func(context.Context, net.Conn) error { warms.Add(1); return nil }
			started := time.Now()
			conn, err := m.DialContext(context.Background(), net.JoinHostPort("warm.example.org", strconv.Itoa(int(target.Port()))))
			took := time.Since(started)
			if c.timestamps {
				if err != nil {
					t.Fatalf("with TCP timestamps the same-tuple reconnect failed after %s: %v", took, err)
				}
				defer conn.Close()
				if local := conn.LocalAddr().(*net.TCPAddr).AddrPort(); local.Port() != c.port || !conn.(*Conn).Warmed || warms.Load() != 1 {
					t.Fatalf("with TCP timestamps the pass came from %s with warmed=%t after %d warms, want port %d after one warm", local, conn.(*Conn).Warmed, warms.Load(), c.port)
				}
				return
			}
			if conn != nil {
				conn.Close()
			}
			if !errors.Is(err, ErrNoTimestamps) || !errors.Is(err, ErrWarm) || warms.Load() != 0 || took >= 500*time.Millisecond || m.Active() != 0 {
				t.Fatalf("without TCP timestamps the dial ended after %s with %v and %d warms, want ErrNoTimestamps before any warm in under 500ms", took, err, warms.Load())
			}
		})
	}
}
