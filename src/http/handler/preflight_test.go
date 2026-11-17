package handler

import (
	"net"
	"testing"
)

func heldLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func TestProbePortReportsATakenPortAndAcceptsOneItCanBind(t *testing.T) {
	taken := heldLoopbackPort(t)
	f := probePort("system.mtproto.port", "127.0.0.1", taken)
	if f == nil || f.Code != "port_in_use_system" || f.Path != "system.mtproto.port" {
		t.Fatalf("a port another listener holds must be reported: %+v", f)
	}
	if f := probePort("system.mtproto.port", "127.0.0.1", 0); f != nil {
		t.Fatalf("a port the probe can bind must pass: %+v", f)
	}
}
