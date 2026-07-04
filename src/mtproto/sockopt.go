package mtproto

import (
	"net"
	"time"

	"golang.org/x/sys/unix"
)

const defaultUserTimeout = 120 * time.Second

func setTCPUserTimeout(c net.Conn, d time.Duration) {
	tc, ok := c.(*net.TCPConn)
	if !ok || d <= 0 {
		return
	}
	ms := int(d.Milliseconds())
	if ms <= 0 {
		return
	}
	raw, err := tc.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fd uintptr) {
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, ms)
	})
}
