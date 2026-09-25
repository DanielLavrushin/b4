package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type ClientConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Timeout  time.Duration
	// BypassMark, when non-zero, is set as SO_MARK on the outbound socket so
	// the dial doesn't get caught by b4's own routing rules (e.g. the OUTPUT
	// mark added for proxy-mode local-origin redirect).
	BypassMark uint32
}

func ApplyBypassMark(d *net.Dialer, mark uint32) {
	if mark == 0 {
		return
	}
	d.Control = func(network, address string, c syscall.RawConn) error {
		var sockErr error
		err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
		})
		if err != nil {
			return err
		}
		return sockErr
	}
}

type ConnectRejectedError struct {
	Code byte
}

func (e *ConnectRejectedError) Error() string {
	return fmt.Sprintf("upstream connect rejected: code=%d", e.Code)
}

func IsConnectRejected(err error) bool {
	var rejected *ConnectRejectedError
	return errors.As(err, &rejected)
}

func DialUpstream(ctx context.Context, cfg ClientConfig, targetHost string, targetPort int) (net.Conn, error) {
	if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid upstream config")
	}
	if targetPort < 1 || targetPort > 65535 {
		return nil, fmt.Errorf("invalid target port")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = dialTimeout
	}

	d := net.Dialer{Timeout: timeout}
	ApplyBypassMark(&d, cfg.BypassMark)
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial upstream: %w", err)
	}

	deadline := time.Now().Add(timeout)
	_ = conn.SetDeadline(deadline)

	if err := clientGreet(conn, cfg.Username, cfg.Password); err != nil {
		conn.Close()
		return nil, err
	}
	if err := clientConnect(conn, targetHost, targetPort); err != nil {
		conn.Close()
		return nil, err
	}

	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func clientGreet(conn net.Conn, user, pass string) error {
	useAuth := user != "" || pass != ""

	var greet []byte
	if useAuth {
		greet = []byte{socks5Version, 2, authNone, authUserPass}
	} else {
		greet = []byte{socks5Version, 1, authNone}
	}
	if _, err := conn.Write(greet); err != nil {
		return fmt.Errorf("greet write: %w", err)
	}

	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("greet read: %w", err)
	}
	if resp[0] != socks5Version {
		return fmt.Errorf("upstream bad version: %d", resp[0])
	}
	switch resp[1] {
	case authNone:
		return nil
	case authUserPass:
		if !useAuth {
			return fmt.Errorf("upstream requires auth but none configured")
		}
		return clientUserPass(conn, user, pass)
	case authNoAccept:
		return fmt.Errorf("upstream rejected all auth methods")
	default:
		return fmt.Errorf("upstream selected unsupported auth: %d", resp[1])
	}
}

func clientUserPass(conn net.Conn, user, pass string) error {
	if len(user) > 255 || len(pass) > 255 {
		return fmt.Errorf("user/pass too long")
	}
	buf := make([]byte, 0, 3+len(user)+len(pass))
	buf = append(buf, authSubVersion, byte(len(user)))
	buf = append(buf, user...)
	buf = append(buf, byte(len(pass)))
	buf = append(buf, pass...)
	if _, err := conn.Write(buf); err != nil {
		return fmt.Errorf("auth write: %w", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("auth read: %w", err)
	}
	if resp[0] != authSubVersion || resp[1] != 0 {
		return fmt.Errorf("upstream auth failed")
	}
	return nil
}

func clientConnect(conn net.Conn, targetHost string, targetPort int) error {
	req := []byte{socks5Version, cmdConnect, 0x00}

	if ip := net.ParseIP(targetHost); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, atypIPv4)
			req = append(req, v4...)
		} else {
			req = append(req, atypIPv6)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(targetHost) > 255 {
			return fmt.Errorf("target host too long")
		}
		req = append(req, atypDomain, byte(len(targetHost)))
		req = append(req, targetHost...)
	}

	var portBuf [2]byte
	binary.BigEndian.PutUint16(portBuf[:], uint16(targetPort))
	req = append(req, portBuf[:]...)

	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("connect write: %w", err)
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("connect reply head: %w", err)
	}
	if head[0] != socks5Version {
		return fmt.Errorf("upstream bad version in reply: %d", head[0])
	}
	if head[1] != repSuccess {
		return &ConnectRejectedError{Code: head[1]}
	}

	var skip int
	switch head[3] {
	case atypIPv4:
		skip = 4
	case atypIPv6:
		skip = 16
	case atypDomain:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return fmt.Errorf("connect reply addr len: %w", err)
		}
		skip = int(l[0])
	default:
		return fmt.Errorf("upstream bad atyp in reply: %d", head[3])
	}
	if _, err := io.ReadFull(conn, make([]byte, skip+2)); err != nil {
		return fmt.Errorf("connect reply addr/port: %w", err)
	}
	return nil
}
