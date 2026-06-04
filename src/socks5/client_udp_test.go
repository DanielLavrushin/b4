package socks5

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startUDPEcho(t *testing.T) int {
	t.Helper()
	echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echo.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := echo.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteToUDP(buf[:n], addr)
		}
	}()
	return echo.LocalAddr().(*net.UDPAddr).Port
}

func TestDialUpstreamUDPRoundTrip(t *testing.T) {
	echoPort := startUDPEcho(t)

	port := freePort(t)
	cfg := config.NewConfig()
	cfg.System.Socks5.Enabled = true
	cfg.System.Socks5.BindAddress = "127.0.0.1"
	cfg.System.Socks5.Port = port

	srv := NewServer(&cfg)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

	time.Sleep(50 * time.Millisecond)

	ucfg := ClientConfig{Host: "127.0.0.1", Port: port, Timeout: 3 * time.Second}
	u, err := DialUpstreamUDP(context.Background(), ucfg, net.IPv4(127, 0, 0, 1), echoPort)
	if err != nil {
		t.Fatalf("dial upstream udp: %v", err)
	}
	defer u.Close()

	msg := []byte("hello udp world")
	if _, err := u.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = u.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 2048)
	n, err := u.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("echo mismatch: got %q want %q", buf[:n], msg)
	}
}

func TestDialUpstreamUDPAuth(t *testing.T) {
	echoPort := startUDPEcho(t)

	port := freePort(t)
	cfg := config.NewConfig()
	cfg.System.Socks5.Enabled = true
	cfg.System.Socks5.BindAddress = "127.0.0.1"
	cfg.System.Socks5.Port = port
	cfg.System.Socks5.Username = "user"
	cfg.System.Socks5.Password = "pass"

	srv := NewServer(&cfg)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

	time.Sleep(50 * time.Millisecond)

	ucfg := ClientConfig{Host: "127.0.0.1", Port: port, Username: "user", Password: "pass", Timeout: 3 * time.Second}
	u, err := DialUpstreamUDP(context.Background(), ucfg, net.IPv4(127, 0, 0, 1), echoPort)
	if err != nil {
		t.Fatalf("dial upstream udp with auth: %v", err)
	}
	defer u.Close()

	msg := []byte("authed datagram")
	if _, err := u.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = u.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 2048)
	n, err := u.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("echo mismatch: got %q want %q", buf[:n], msg)
	}
}
