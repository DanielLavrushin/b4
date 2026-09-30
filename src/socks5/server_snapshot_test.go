package socks5

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestAConnectionAcceptedWithCredentialsKeepsNeedingThem(t *testing.T) {
	s, addr := startTestServer(t, config.Socks5Config{Username: "u", Password: "p"})

	early, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = early.Close() })
	deadline := time.Now().Add(2 * time.Second)
	for s.activeConns.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the server never accepted the connection")
		}
		time.Sleep(5 * time.Millisecond)
	}

	relaxed := *s.getCfg()
	relaxed.System.Socks5.Username = ""
	relaxed.System.Socks5.Password = ""
	s.UpdateConfig(&relaxed)

	if err := early.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	reply := make([]byte, 2)
	if _, err := early.Write([]byte{socks5Version, 1, authNone}); err == nil {
		if _, err := io.ReadFull(early, reply); err == nil && reply[1] != authNoAccept {
			t.Fatalf("a connection accepted while a password was required must be dropped or refused, not served without one, got % x", reply)
		}
	}

	_, reply, err = greet(t, addr, authNone)
	if err != nil || reply[1] != authNone {
		t.Fatalf("a connection opened after the change follows the new settings, got % x (%v)", reply, err)
	}
}
