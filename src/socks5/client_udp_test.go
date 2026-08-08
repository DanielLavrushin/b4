package socks5

import (
	"context"
	"io"
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

func startStubAssociate(t *testing.T, advertised net.IP) (ctrlPort int) {
	t.Helper()

	relay, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { relay.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := relay.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = relay.WriteToUDP(buf[:n], addr)
		}
	}()
	relayPort := relay.LocalAddr().(*net.UDPAddr).Port

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		greet := make([]byte, 3)
		if _, err := io.ReadFull(conn, greet); err != nil {
			return
		}
		if _, err := conn.Write([]byte{socks5Version, authNone}); err != nil {
			return
		}
		req := make([]byte, 10)
		if _, err := io.ReadFull(conn, req); err != nil {
			return
		}
		reply := []byte{socks5Version, repSuccess, 0x00, atypIPv4}
		reply = append(reply, advertised.To4()...)
		reply = append(reply, byte(relayPort>>8), byte(relayPort))
		if _, err := conn.Write(reply); err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, conn)
	}()

	return ln.Addr().(*net.TCPAddr).Port
}

func TestDialUpstreamUDPPinsRelayToUpstreamHost(t *testing.T) {
	port := startStubAssociate(t, net.IPv4(192, 0, 2, 1))

	ucfg := ClientConfig{Host: "127.0.0.1", Port: port, Timeout: 3 * time.Second}
	u, err := DialUpstreamUDP(context.Background(), ucfg, net.IPv4(127, 0, 0, 1), 9)
	if err != nil {
		t.Fatalf("dial upstream udp: %v", err)
	}
	defer u.Close()

	msg := []byte("pinned relay")
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
