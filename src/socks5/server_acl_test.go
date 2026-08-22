package socks5

import (
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func startTestServer(t *testing.T, s5 config.Socks5Config) (*Server, string) {
	t.Helper()
	s5.Enabled = true
	s5.BindAddress = "127.0.0.1"
	if s5.Port == 0 {
		s5.Port = freePort(t)
	}
	cfg := &config.Config{}
	cfg.System.Socks5 = s5

	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s, net.JoinHostPort(s5.BindAddress, strconv.Itoa(s5.Port))
}

func greet(t *testing.T, addr string, methods ...byte) (net.Conn, []byte, error) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return c, nil, err
	}
	msg := append([]byte{socks5Version, byte(len(methods))}, methods...)
	if _, err := c.Write(msg); err != nil {
		return c, nil, err
	}
	reply := make([]byte, 2)
	n, err := io.ReadFull(c, reply)
	return c, reply[:n], err
}

func TestAcceptAllowsListedSource(t *testing.T) {
	_, addr := startTestServer(t, config.Socks5Config{AllowedSources: []string{"127.0.0.0/8"}})

	_, reply, err := greet(t, addr, authNone)
	if err != nil {
		t.Fatalf("greeting from a listed source failed: %v", err)
	}
	if reply[0] != socks5Version || reply[1] != authNone {
		t.Fatalf("expected method 0x00, got % x", reply)
	}
}

func TestAcceptRejectsUnlistedSource(t *testing.T) {
	s, addr := startTestServer(t, config.Socks5Config{AllowedSources: []string{"192.168.77.0/24"}})

	_, reply, err := greet(t, addr, authNone)
	if err == nil {
		t.Fatalf("an unlisted source must not complete the greeting, got % x", reply)
	}
	if len(reply) != 0 {
		t.Fatalf("an unlisted source must receive no protocol bytes, got % x", reply)
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		var ne net.Error
		if !errors.As(err, &ne) {
			t.Logf("closed with %v", err)
		}
	}
	if n := s.activeConns.Load(); n != 0 {
		t.Errorf("a refused source must not occupy a connection slot, activeConns=%d", n)
	}
}

func TestAcceptWithoutAllowlistIsUnrestricted(t *testing.T) {
	_, addr := startTestServer(t, config.Socks5Config{})

	_, reply, err := greet(t, addr, authNone)
	if err != nil {
		t.Fatalf("an unrestricted server must accept any source: %v", err)
	}
	if reply[1] != authNone {
		t.Fatalf("expected method 0x00, got % x", reply)
	}
}

func TestAllowlistDoesNotChangeAuthMethod(t *testing.T) {
	_, addr := startTestServer(t, config.Socks5Config{
		Username:       "u",
		Password:       "p",
		AllowedSources: []string{"127.0.0.1"},
	})

	_, reply, err := greet(t, addr, authNone, authUserPass)
	if err != nil {
		t.Fatalf("greeting failed: %v", err)
	}
	if reply[1] != authUserPass {
		t.Fatalf("a listed source must still be asked for the password, got % x", reply)
	}
}

func TestIncompleteCredentialsRefuseEveryClient(t *testing.T) {
	_, addr := startTestServer(t, config.Socks5Config{Username: "u"})

	_, reply, err := greet(t, addr, authNone, authUserPass)
	if err != nil {
		t.Fatalf("expected a method-selection reply, got %v", err)
	}
	if reply[1] != authNoAccept {
		t.Fatalf("a half-filled credential pair must refuse every method, got % x", reply)
	}
}

func TestAuthMethodIsNotDowngradableWhenCredentialsAreSet(t *testing.T) {
	_, addr := startTestServer(t, config.Socks5Config{Username: "u", Password: "p"})

	_, reply, err := greet(t, addr, authNone)
	if err != nil {
		t.Fatalf("expected a method-selection reply, got %v", err)
	}
	if reply[1] != authNoAccept {
		t.Fatalf("a client offering only 0x00 must be refused, got % x", reply)
	}
}

func TestBlankCredentialsRefuseUserPassOnlyClient(t *testing.T) {
	_, addr := startTestServer(t, config.Socks5Config{})

	_, reply, err := greet(t, addr, authUserPass)
	if err != nil {
		t.Fatalf("expected a method-selection reply, got %v", err)
	}
	if reply[1] != authNoAccept {
		t.Fatalf("blank credentials must never satisfy a username/password client, got % x", reply)
	}
}

func TestAllowlistChangeClosesLiveSession(t *testing.T) {
	s, addr := startTestServer(t, config.Socks5Config{AllowedSources: []string{"127.0.0.0/8"}})

	c, reply, err := greet(t, addr, authNone)
	if err != nil {
		t.Fatalf("greeting failed: %v", err)
	}
	if reply[1] != authNone {
		t.Fatalf("expected method 0x00, got % x", reply)
	}

	next := &config.Config{}
	next.System.Socks5 = s.getCfg().System.Socks5
	next.System.Socks5.AllowedSources = []string{"192.168.77.0/24"}
	s.UpdateConfig(next)

	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("revoking a source must close its live session")
	}
}

func TestAllowlistChangeKeepsStillListedSession(t *testing.T) {
	s, addr := startTestServer(t, config.Socks5Config{AllowedSources: []string{"127.0.0.0/8"}})

	c, _, err := greet(t, addr, authNone)
	if err != nil {
		t.Fatalf("greeting failed: %v", err)
	}

	next := &config.Config{}
	next.System.Socks5 = s.getCfg().System.Socks5
	next.System.Socks5.AllowedSources = []string{"127.0.0.1/32", "10.0.0.0/8"}
	s.UpdateConfig(next)

	if err := c.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	_, err = c.Read(buf)
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("a session that still matches must stay open, got %v", err)
	}
}

func TestAllowlistAppliesAfterRestartingListener(t *testing.T) {
	s, addr := startTestServer(t, config.Socks5Config{})

	if _, _, err := greet(t, addr, authNone); err != nil {
		t.Fatalf("greeting failed: %v", err)
	}

	next := &config.Config{}
	next.System.Socks5 = s.getCfg().System.Socks5
	next.System.Socks5.Port = freePort(t)
	next.System.Socks5.AllowedSources = []string{"192.168.77.0/24"}
	s.UpdateConfig(next)

	newAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(next.System.Socks5.Port))
	_, reply, err := greet(t, newAddr, authNone)
	if err == nil {
		t.Fatalf("a rebound listener must apply the allowlist, got % x", reply)
	}
	if len(reply) != 0 {
		t.Fatalf("expected no protocol bytes, got % x", reply)
	}
}

func TestStopClosesLiveSessions(t *testing.T) {
	s, addr := startTestServer(t, config.Socks5Config{})

	c, _, err := greet(t, addr, authNone)
	if err != nil {
		t.Fatalf("greeting failed: %v", err)
	}

	if err := s.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("stopping the server must close its live sessions")
	}
}

func TestDisablingProxyClosesLiveSessions(t *testing.T) {
	s, addr := startTestServer(t, config.Socks5Config{})

	c, _, err := greet(t, addr, authNone)
	if err != nil {
		t.Fatalf("greeting failed: %v", err)
	}

	next := &config.Config{}
	next.System.Socks5 = s.getCfg().System.Socks5
	next.System.Socks5.Enabled = false
	s.UpdateConfig(next)

	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("disabling the proxy must close its live sessions")
	}
}
