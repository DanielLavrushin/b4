package tproxy

import (
	"context"
	"net"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func socketMark(t *testing.T, uc *net.UDPConn) int {
	t.Helper()
	raw, err := uc.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var got int
	var gerr error
	if err := raw.Control(func(fd uintptr) {
		got, gerr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK)
	}); err != nil {
		t.Fatal(err)
	}
	if gerr != nil {
		t.Fatal(gerr)
	}
	return got
}

func TestReplySocketCarriesTheSelfDialMark(t *testing.T) {
	dst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	uc, err := openReplySocket(context.Background(), dst, false, 0x40000)
	if err != nil {
		t.Skipf("a transparent reply socket needs CAP_NET_ADMIN: %v", err)
	}
	defer uc.Close()
	if got := socketMark(t, uc); got != 0x40000 {
		if os.Geteuid() != 0 {
			t.Skipf("SO_MARK needs CAP_NET_ADMIN, got mark 0x%x", got)
		}
		t.Errorf("reply socket mark = 0x%x, want 0x40000 so the routing chains return b4's own replies", got)
	}

	plain, err := openReplySocket(context.Background(), dst, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if got := socketMark(t, plain); got != 0 {
		t.Errorf("a zero mark must leave the socket unmarked, got 0x%x", got)
	}
}
