package detector

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func markControl(mark uint) func(string, string, syscall.RawConn) error {
	if mark == 0 {
		return nil
	}
	return func(_, _ string, c syscall.RawConn) error {
		var ctrlErr error
		if err := c.Control(func(fd uintptr) {
			ctrlErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(uint32(mark)))
		}); err != nil {
			return err
		}
		if ctrlErr != nil {
			return fmt.Errorf("failed to set SO_MARK=%d: %w", mark, ctrlErr)
		}
		return nil
	}
}

func markedDialer(mark uint, timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout: timeout,
		Control: markControl(mark),
	}
}

func markedHTTPClient(mark uint, timeout time.Duration) *http.Client {
	d := markedDialer(mark, timeout)
	tr := &http.Transport{
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          100,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}
}
