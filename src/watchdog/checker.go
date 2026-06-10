package watchdog

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
	"golang.org/x/sys/unix"
)

func checkDomain(input string, mark uint, timeout time.Duration) CheckResult {
	checkURL := input
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		checkURL = "https://" + input + "/"
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	dialer := &net.Dialer{
		Timeout:   timeout / 2,
		KeepAlive: timeout,
		Control: func(_, _ string, c syscall.RawConn) error {
			var ctrlErr error
			if err := c.Control(func(fd uintptr) {
				ctrlErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
			}); err != nil {
				return err
			}
			return ctrlErr
		},
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
		DialContext:           dialer.DialContext,
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 {
				if netprobe.IsBlockPageRedirect(req.URL.String()) {
					return fmt.Errorf("ISP block page (redirect to %s)", req.URL.String())
				}
			}
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", checkURL, nil)
	if err != nil {
		return CheckResult{Error: err.Error()}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		_, detail := netprobe.ClassifyTLSError(err)
		return CheckResult{Error: detail}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 451 {
		return CheckResult{Error: "ISP block page (HTTP 451)"}
	}

	buf := make([]byte, 16*1024)
	headBuf := make([]byte, 0, 4*1024)
	var bytesRead int64
	maxRead := int64(16 * 1024)

	for bytesRead < maxRead {
		select {
		case <-ctx.Done():
			goto evaluate
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			bytesRead += int64(n)
			if len(headBuf) < 4*1024 {
				headBuf = append(headBuf, buf[:n]...)
				if len(headBuf) > 4*1024 {
					headBuf = headBuf[:4*1024]
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return CheckResult{Error: fmt.Sprintf("read error after %d bytes: %v", bytesRead, readErr)}
		}
	}

evaluate:
	duration := time.Since(start)
	speed := float64(0)
	if duration.Seconds() > 0 {
		speed = float64(bytesRead) / duration.Seconds()
	}

	if blockErr := netprobe.DetectBlockPageBody(headBuf); blockErr != "" {
		return CheckResult{Error: blockErr}
	}

	if bytesRead < 1024 {
		return CheckResult{Error: fmt.Sprintf("insufficient data: %d bytes", bytesRead)}
	}

	return CheckResult{
		OK:        true,
		Speed:     speed,
		BytesRead: bytesRead,
	}
}

func checkAllConcurrently(domains []string, mark uint, timeout time.Duration) map[string]CheckResult {
	results := make(map[string]CheckResult, len(domains))
	type result struct {
		domain string
		check  CheckResult
	}
	ch := make(chan result, len(domains))

	for _, d := range domains {
		go func(domain string) {
			r := checkDomain(domain, mark, timeout)
			ch <- result{domain: domain, check: r}
		}(d)
	}

	for range domains {
		r := <-ch
		results[r.domain] = r.check
	}

	log.Tracef("[WATCHDOG] checked %d domains concurrently", len(domains))
	return results
}
