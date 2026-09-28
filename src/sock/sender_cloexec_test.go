package sock

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSenderSocketsCloseOnExec(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("raw socket requires root")
	}

	s, err := NewSenderWithMark(0)
	if err != nil {
		t.Skipf("raw socket unavailable: %v", err)
	}
	t.Cleanup(s.Close)

	for _, fd := range []int{s.fd4, s.fd6} {
		if fd < 0 {
			continue
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			t.Fatal(err)
		}
		if flags&unix.FD_CLOEXEC == 0 {
			t.Fatalf("raw socket %d stays open across exec, so an in-place restart and every child process inherit it", fd)
		}
	}
}
