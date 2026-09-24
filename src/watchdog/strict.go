package watchdog

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

const (
	strictMaxRead       = 100 * 1024
	strictMinBytes      = 1024
	strictMaxRedirects  = 3
	strictCompleteRatio = 0.9
	strictBlockPageScan = 4 * 1024
	strictTimeout       = 15 * time.Second
)

type URLCheck struct {
	Status     string
	StatusCode int
	BytesRead  int64
	Speed      float64
	Error      string
}

type strictOptions struct {
	IPv6       bool
	Timeout    time.Duration
	RootCAs    *x509.CertPool
	isReserved func(netip.Addr) bool
}

func checkURLStrict(ctx context.Context, rawURL string, ipv6Enabled bool, timeout time.Duration) URLCheck {
	return strictCheck(ctx, rawURL, strictOptions{IPv6: ipv6Enabled, Timeout: timeout})
}

func strictCheck(parent context.Context, rawURL string, opt strictOptions) URLCheck {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return URLCheck{Status: URLStatusFailed, Error: fmt.Sprintf("%q is not an http or https URL", rawURL)}
	}

	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = strictTimeout
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	guard := newDialGuard(opt.isReserved)
	if refusal := guard.reservedLiteral(target.Hostname()); refusal != nil {
		return URLCheck{Status: URLStatusUnusable, Error: refusal.Error()}
	}

	dialer := guard.dialer(markThroughEngine, timeout)
	dial := dialer.DialContext
	if !opt.IPv6 {
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			if network == "tcp" {
				network = "tcp4"
			}
			return dialer.DialContext(ctx, network, address)
		}
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    opt.RootCAs,
		},
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
		DialContext:           dial,
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && netprobe.IsBlockPageRedirectFrom(via[0].URL, req.URL) {
				return fmt.Errorf("ISP block page (redirect to %s)", req.URL.String())
			}
			if len(via) > strictMaxRedirects {
				return fmt.Errorf("more than %d redirects", strictMaxRedirects)
			}
			if refusal := guard.reservedLiteral(req.URL.Hostname()); refusal != nil {
				guard.refuse(refusal)
				return refusal
			}
			guard.nextHop()
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return URLCheck{Status: URLStatusFailed, Error: err.Error()}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return classifyStrictError(err, guard)
	}
	defer resp.Body.Close()

	res := URLCheck{StatusCode: resp.StatusCode}
	switch {
	case resp.StatusCode == http.StatusUnavailableForLegalReasons:
		res.Status, res.Error = URLStatusFailed, "ISP block page (HTTP 451)"
		return res
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode >= 500:
		res.Status, res.Error = URLStatusFailed, fmt.Sprintf("HTTP %d", resp.StatusCode)
		return res
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, strictMaxRead))
	res.BytesRead = int64(len(body))
	if d := time.Since(start).Seconds(); d > 0 {
		res.Speed = float64(len(body)) / d
	}
	return classifyStrictBody(res, body, resp.ContentLength, readErr)
}

func classifyStrictBody(res URLCheck, body []byte, contentLength int64, readErr error) URLCheck {
	head := body
	if len(head) > strictBlockPageScan {
		head = head[:strictBlockPageScan]
	}
	if blockErr := netprobe.DetectBlockPageBody(head); blockErr != "" {
		res.Status, res.Error = URLStatusFailed, blockErr
		return res
	}

	read := int64(len(body))
	complete := bodyClosesHTML(body)
	switch {
	case read < strictMinBytes:
		res.Status, res.Error = URLStatusFailed, fmt.Sprintf("insufficient data: %d bytes", read)
		if readErr != nil {
			res.Error += fmt.Sprintf(" (%v)", readErr)
		}
		return res
	case contentLength > 0:
		want := min(contentLength, int64(strictMaxRead))
		if float64(read) < strictCompleteRatio*float64(want) && !complete {
			res.Status, res.Error = URLStatusFailed, fmt.Sprintf("truncated: %d of %d bytes", read, want)
			if readErr != nil {
				status, _ := netprobe.ClassifyTLSErrorStaged(readErr, netprobe.StageRead, int(read))
				res.Error += fmt.Sprintf(" [%s]", status)
			}
			return res
		}
	case readErr != nil && !complete:
		status, detail := netprobe.ClassifyTLSErrorStaged(readErr, netprobe.StageRead, int(read))
		res.Status, res.Error = URLStatusFailed, fmt.Sprintf("read failed after %d bytes: %s [%s]", read, detail, status)
		return res
	}

	res.Status, res.Error = URLStatusOK, ""
	return res
}

func bodyClosesHTML(body []byte) bool {
	tail := bytes.TrimRight(body, " \t\r\n")
	if len(tail) > 32 {
		tail = tail[len(tail)-32:]
	}
	return bytes.HasSuffix(bytes.ToLower(tail), []byte("</html>"))
}

func classifyStrictError(err error, guard *dialGuard) URLCheck {
	if refused := guard.refusal(); refused != nil {
		return URLCheck{Status: URLStatusUnusable, Error: refused.Error()}
	}
	if isCertificateError(err) {
		return URLCheck{Status: URLStatusUnusable, Error: "certificate verification failed: " + err.Error()}
	}
	status, detail := netprobe.ClassifyTLSError(err)
	if status == netprobe.DomainMTLS {
		return URLCheck{Status: URLStatusUnusable, Error: detail}
	}
	return URLCheck{Status: URLStatusFailed, Error: fmt.Sprintf("%s [%s]", detail, status)}
}

func isCertificateError(err error) bool {
	var verifyErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verifyErr) ||
		errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostname) ||
		errors.As(err, &invalid)
}
