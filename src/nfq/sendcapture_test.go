package nfq

import (
	"syscall"
	"testing"

	"github.com/daniellavrushin/b4/sock"
)

func captureInjected(t *testing.T, w *Worker) func() [][]byte {
	t.Helper()
	pair := func() [2]int {
		fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("socketpair: %v", err)
		}
		return [2]int{fds[0], fds[1]}
	}
	v4, v6 := pair(), pair()
	w.sock = sock.NewSenderWithFDs(v4[0], v6[0])
	t.Cleanup(func() {
		w.wg.Wait()
		for _, fd := range []int{v4[0], v4[1], v6[0], v6[1]} {
			_ = syscall.Close(fd)
		}
	})
	return func() [][]byte {
		w.wg.Wait()
		var sent [][]byte
		buf := make([]byte, 65536)
		for _, fd := range []int{v4[1], v6[1]} {
			for {
				n, _, err := syscall.Recvfrom(fd, buf, syscall.MSG_DONTWAIT)
				if err != nil {
					break
				}
				sent = append(sent, append([]byte(nil), buf[:n]...))
			}
		}
		return sent
	}
}
