package watchdog

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
	"golang.org/x/sys/unix"
)

type ErrPrivateDestination struct{ Addr string }

func (e *ErrPrivateDestination) Error() string {
	return fmt.Sprintf("%s is a private or local address; probing it would reach the network b4 runs on, not the internet", e.Addr)
}

type ProbeOptions struct {
	Mark    uint
	Timeout time.Duration
}

func isReservedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid(),
		addr.IsLoopback(),
		addr.IsUnspecified(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsPrivate():
		return true
	}
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
		if b[0] == 0 || b[0] >= 240 {
			return true
		}
	}
	return false
}

func IsReservedHost(host string) bool {
	host = strings.TrimSpace(host)
	for _, candidate := range []string{host, strings.Trim(host, "[]"), ExtractDomain(host)} {
		if addr, err := netip.ParseAddr(candidate); err == nil {
			return isReservedAddr(addr)
		}
	}
	return false
}

func ProbeHost(ctx context.Context, host string, opt ProbeOptions) (CheckResult, error) {
	host = ExtractDomain(host)
	if host == "" {
		return CheckResult{}, fmt.Errorf("no hostname to probe")
	}
	if strings.ContainsAny(host, "/?#@ \t") {
		return CheckResult{}, fmt.Errorf("%q is not a bare hostname", host)
	}
	if addr, err := netip.ParseAddr(host); err == nil && isReservedAddr(addr) {
		return CheckResult{}, &ErrPrivateDestination{Addr: host}
	}

	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var attempts struct {
		sync.Mutex
		refused *ErrPrivateDestination
		usable  bool
	}

	dialer := &net.Dialer{
		Timeout:   timeout / 2,
		KeepAlive: timeout,
		Control: func(_, address string, c syscall.RawConn) error {
			hostPart, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			addr, err := netip.ParseAddr(hostPart)
			if err != nil {
				return err
			}
			if isReservedAddr(addr) {
				refusal := &ErrPrivateDestination{Addr: addr.String()}
				attempts.Lock()
				if attempts.refused == nil {
					attempts.refused = refusal
				}
				attempts.Unlock()
				return refusal
			}
			attempts.Lock()
			attempts.usable = true
			attempts.Unlock()
			if opt.Mark == 0 {
				return nil
			}
			var ctrlErr error
			if err := c.Control(func(fd uintptr) {
				ctrlErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(opt.Mark))
			}); err != nil {
				return err
			}
			return ctrlErr
		},
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: false,
		},
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
		DialContext:           dialer.DialContext,
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if netprobe.IsBlockPageRedirect(req.URL.String()) {
				return fmt.Errorf("ISP block page (redirect to %s)", req.URL.String())
			}
			if !strings.EqualFold(req.URL.Hostname(), host) {
				return fmt.Errorf("redirect leaves %s for %s", host, req.URL.Hostname())
			}
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		return CheckResult{Error: err.Error(), Verdict: netprobe.DomainError}, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		attempts.Lock()
		refused, usable := attempts.refused, attempts.usable
		attempts.Unlock()
		if refused != nil && !usable {
			return CheckResult{}, refused
		}
		status, detail := netprobe.ClassifyTLSError(err)
		return CheckResult{Error: detail, Verdict: status}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 451 {
		return CheckResult{Error: "ISP block page (HTTP 451)", Verdict: netprobe.DomainISPPage}, nil
	}

	buf := make([]byte, 16*1024)
	head := make([]byte, 0, 4*1024)
	var read int64
	const maxRead = int64(16 * 1024)

	for read < maxRead && ctx.Err() == nil {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			read += int64(n)
			if len(head) < 4*1024 {
				head = append(head, buf[:n]...)
				if len(head) > 4*1024 {
					head = head[:4*1024]
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			status, _ := netprobe.ClassifyTLSErrorStaged(readErr, netprobe.StageRead, int(read))
			return CheckResult{Error: fmt.Sprintf("read error after %d bytes: %v", read, readErr), Verdict: status}, nil
		}
	}

	speed := float64(0)
	if d := time.Since(start).Seconds(); d > 0 {
		speed = float64(read) / d
	}
	if blockErr := netprobe.DetectBlockPageBody(head); blockErr != "" {
		return CheckResult{Error: blockErr, Verdict: netprobe.DomainISPPage}, nil
	}
	if read < 1024 {
		return CheckResult{Error: fmt.Sprintf("insufficient data: %d bytes", read), Verdict: netprobe.DomainError}, nil
	}
	return CheckResult{OK: true, Speed: speed, BytesRead: read}, nil
}
