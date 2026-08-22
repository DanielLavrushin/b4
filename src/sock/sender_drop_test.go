package sock

import (
	"net"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestIsSendBackpressure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"eagain", syscall.EAGAIN, true},
		{"ewouldblock", syscall.EWOULDBLOCK, true},
		{"enobufs", syscall.ENOBUFS, true},
		{"eperm", syscall.EPERM, false},
		{"enetunreach", syscall.ENETUNREACH, false},
		{"ebadf", syscall.EBADF, false},
		{"emsgsize", syscall.EMSGSIZE, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSendBackpressure(c.err); got != c.want {
				t.Fatalf("isSendBackpressure(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestNoteSendDropCounts(t *testing.T) {
	ResetSendDropped()
	t.Cleanup(ResetSendDropped)

	for i := 0; i < 5; i++ {
		noteSendDrop("IPv4", syscall.ENOBUFS)
	}
	if got := SendDropped(); got != 5 {
		t.Fatalf("SendDropped() = %d, want 5", got)
	}
}

func TestSendIPv4ReturnsNilOnBackpressure(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("raw socket requires root")
	}

	s, err := NewSenderWithMark(0)
	if err != nil {
		t.Skipf("raw socket unavailable: %v", err)
	}
	t.Cleanup(s.Close)

	if err := syscall.SetsockoptInt(s.fd4, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 2048); err != nil {
		t.Skipf("cannot shrink send buffer: %v", err)
	}

	ResetSendDropped()
	t.Cleanup(ResetSendDropped)

	packet := make([]byte, 1400)
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = syscall.IPPROTO_TCP
	copy(packet[12:16], net.IPv4(127, 0, 0, 1).To4())
	copy(packet[16:20], net.IPv4(127, 0, 0, 1).To4())

	for i := 0; i < 20000; i++ {
		if err := s.SendIPv4(packet, net.IPv4(127, 0, 0, 1)); err != nil {
			t.Fatalf("SendIPv4 returned an error instead of dropping: %v", err)
		}
		if SendDropped() > 0 {
			return
		}
	}
	t.Skip("the loopback send buffer never filled, backpressure path not exercised")
}

func TestSendTimeoutIsApplied(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("raw socket requires root")
	}

	s, err := NewSenderWithMark(0)
	if err != nil {
		t.Skipf("raw socket unavailable: %v", err)
	}
	t.Cleanup(s.Close)

	tv, err := unix.GetsockoptTimeval(s.fd4, unix.SOL_SOCKET, unix.SO_SNDTIMEO)
	if err != nil {
		t.Fatalf("SO_SNDTIMEO not readable: %v", err)
	}
	if tv.Sec != sendTimeoutSec || tv.Usec != 0 {
		t.Fatalf("SO_SNDTIMEO = %ds %dus, want %ds", tv.Sec, tv.Usec, sendTimeoutSec)
	}
}
