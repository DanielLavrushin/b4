package mtproto

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

func TestSetTCPUserTimeoutAppliesToSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	setTCPUserTimeout(c, 45*time.Second)

	raw, err := c.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var got int
	var gerr error
	if cerr := raw.Control(func(fd uintptr) {
		got, gerr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT)
	}); cerr != nil {
		t.Fatal(cerr)
	}
	if gerr != nil {
		t.Fatal(gerr)
	}
	if got != 45000 {
		t.Fatalf("TCP_USER_TIMEOUT = %d ms, want 45000", got)
	}
}

func TestMTProtoTimeoutResolvers(t *testing.T) {
	cases := []struct {
		name    string
		userSet int
		userTO  time.Duration
		idleSet int
		idleTO  time.Duration
	}{
		{"omitted -> defaults", 0, defaultUserTimeout, 0, defaultIdleTimeout},
		{"disabled", -1, 0, -1, 0},
		{"explicit", 45, 45 * time.Second, 90, 90 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.System.MTProto.TCPUserTimeoutSec = tc.userSet
			cfg.System.MTProto.IdleTimeoutSec = tc.idleSet
			if got := mtprotoTCPUserTimeout(cfg); got != tc.userTO {
				t.Fatalf("user timeout(%d) = %v, want %v", tc.userSet, got, tc.userTO)
			}
			if got := mtprotoIdleTimeout(cfg); got != tc.idleTO {
				t.Fatalf("idle timeout(%d) = %v, want %v", tc.idleSet, got, tc.idleTO)
			}
		})
	}
}

func testRelayPool() *sync.Pool {
	return &sync.Pool{New: func() interface{} {
		b := make([]byte, relayBufSize)
		return &b
	}}
}

func TestRelayIdleReaperClosesSilentSession(t *testing.T) {
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer clientB.Close()
	defer dcB.Close()

	pool := testRelayPool()
	done := make(chan struct{})
	start := time.Now()
	go func() {
		relayConns(clientA, dcA, nil, "idle-test", pool, 300*time.Millisecond, nil)
		close(done)
	}()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
			t.Fatalf("relay returned after %v, too early to be the idle reaper", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("relay was not reaped on idle: relayConns still blocked")
	}
}

func TestRelayIdleReaperDisabled(t *testing.T) {
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer clientB.Close()
	defer dcB.Close()

	pool := testRelayPool()
	done := make(chan struct{})
	go func() {
		relayConns(clientA, dcA, nil, "no-idle-test", pool, 0, nil)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("relay returned though idle timeout disabled and no side closed")
	case <-time.After(500 * time.Millisecond):
	}

	_ = clientA.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not return after the client conn was closed")
	}
}

func TestWSTryWriteControlSkipsWhenWriterWedged(t *testing.T) {
	c := &wsConn{}
	c.wMu.Lock()

	done := make(chan struct{})
	go func() {
		c.tryWriteControl(wsOpcodePong, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tryWriteControl blocked while the write lock was held by a wedged writer")
	}
	c.wMu.Unlock()
}
