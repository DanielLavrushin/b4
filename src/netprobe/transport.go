package netprobe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func MarkControl(mark int) func(string, string, syscall.RawConn) error {
	if mark == 0 {
		return nil
	}
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		if cerr := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
		}); cerr != nil {
			return cerr
		}
		if serr != nil {
			return fmt.Errorf("failed to set SO_MARK=%d: %w", mark, serr)
		}
		return nil
	}
}

func Dialer(mark int, timeout, keepAlive time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout:   timeout,
		KeepAlive: keepAlive,
		Control:   MarkControl(mark),
	}
}

func HTTPClient(mark int, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		DialContext:           Dialer(mark, timeout, timeout).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          100,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

func MarkedResolver(mark int, timeout time.Duration, server string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			target := addr
			if server != "" {
				target = net.JoinHostPort(server, "53")
			}
			return Dialer(mark, timeout, timeout).DialContext(ctx, network, target)
		},
	}
}
