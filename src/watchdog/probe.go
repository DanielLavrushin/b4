package watchdog

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/utils"
)

const (
	VerdictRedirect   netprobe.DomainStatus = "REDIRECT"
	ProbeMaxRedirects                       = 5
)

type ErrPrivateDestination struct {
	Addr string
	Host string
}

func (e *ErrPrivateDestination) Error() string {
	return fmt.Sprintf("%s is a private or local address; probing it would reach the network b4 runs on, not the internet", e.Addr)
}

type ProbeOptions struct {
	Mark    uint
	Timeout time.Duration
}

type probeStop struct{ result CheckResult }

func (s *probeStop) Error() string { return s.result.Error }

func IsReservedHost(host string) bool {
	host = strings.TrimSpace(host)
	for _, candidate := range []string{host, strings.Trim(host, "[]"), ExtractDomain(host)} {
		if utils.IsReservedHost(candidate) {
			return true
		}
	}
	return false
}

func ValidateProbeHost(host string) error {
	switch {
	case host == "":
		return fmt.Errorf("no hostname to probe")
	case strings.ContainsAny(host, "/?#@ \t"):
		return fmt.Errorf("%q is not a bare hostname: pass a domain such as example.com", host)
	case strings.IndexFunc(host, func(r rune) bool { return r > unicode.MaxASCII }) >= 0:
		return fmt.Errorf("%q is not written in ASCII: pass its punycode form, the xn-- spelling that DNS uses", host)
	}
	return nil
}

func ProbeHost(ctx context.Context, host string, opt ProbeOptions) (CheckResult, error) {
	host = strings.TrimSuffix(ExtractDomain(host), ".")
	if err := ValidateProbeHost(host); err != nil {
		return CheckResult{}, err
	}
	if addr, err := netip.ParseAddr(host); err == nil && utils.IsReservedAddr(addr) {
		return CheckResult{}, &ErrPrivateDestination{Addr: host, Host: host}
	}
	return probe(ctx, host, "https://"+host+"/", opt, newDialGuard(nil), nil)
}

func probe(ctx context.Context, host, target string, opt ProbeOptions, guard *dialGuard, roots *x509.CertPool) (CheckResult, error) {
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := guard.dialer(opt.Mark, timeout)

	verify := true
	if roots == nil {
		roots, verify = netprobe.TLSRoots()
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			RootCAs:            roots,
			InsecureSkipVerify: !verify,
		},
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
		DialContext:           dialer.DialContext,
	}
	defer transport.CloseIdleConnections()

	hop := host
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if stop := probeRedirectStop(host, hop, req.URL, via); stop != nil {
				return stop
			}
			guard.nextHop()
			hop = strings.TrimSuffix(req.URL.Hostname(), ".")
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return CheckResult{Error: err.Error(), Verdict: netprobe.DomainError}, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		var stop *probeStop
		if errors.As(err, &stop) {
			return stop.result, nil
		}
		if refused := guard.refusal(); refused != nil {
			return CheckResult{}, &ErrPrivateDestination{Addr: refused.Addr, Host: hop}
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		status, detail := netprobe.ClassifyTLSError(err)
		return atHop(CheckResult{Error: detail, Verdict: status}, host, hop), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 451 {
		return atHop(CheckResult{Error: "ISP block page (HTTP 451)", Verdict: netprobe.DomainISPPage}, host, hop), nil
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
			return atHop(CheckResult{Error: fmt.Sprintf("read error after %d bytes: %v", read, readErr), Verdict: status}, host, hop), nil
		}
	}

	speed := float64(0)
	if d := time.Since(start).Seconds(); d > 0 {
		speed = float64(read) / d
	}
	res, err := classifyProbeBody(ctx.Err(), head, read, speed, resp.StatusCode)
	return atHop(res, host, hop), err
}

func probeRedirectStop(host, from string, next *url.URL, via []*http.Request) *probeStop {
	target := next.Hostname()
	var res CheckResult
	switch {
	case len(via) > 0 && netprobe.IsBlockPageRedirectFrom(via[len(via)-1].URL, next):
		res = CheckResult{Error: fmt.Sprintf("ISP block page (redirect to %s)", next), Verdict: netprobe.DomainISPPage}
	case target == "":
		res = CheckResult{Error: fmt.Sprintf("answered with a redirect that names no host (%s)", next), Verdict: VerdictRedirect, Answered: true}
	case IsReservedHost(target):
		res = CheckResult{
			Error:   fmt.Sprintf("redirect to %s, a private or local address: a public site does not send its visitors there, so this is treated as a block page", target),
			Verdict: netprobe.DomainISPPage,
		}
	case !netprobe.SameSite(host, target):
		msg := fmt.Sprintf("answered with a redirect to %s, a different site, which the probe does not follow", target)
		if _, err := netip.ParseAddr(target); err == nil {
			msg = fmt.Sprintf("answered with a redirect to the bare address %s, which the probe does not follow", target)
		}
		res = CheckResult{Error: msg, Verdict: VerdictRedirect, RedirectTo: target, Answered: true}
	case len(via) > ProbeMaxRedirects:
		return &probeStop{CheckResult{
			Error:      fmt.Sprintf("still redirecting within the site after %d redirects, last to %s; the probe sends no cookies, and some sites keep redirecting without them", ProbeMaxRedirects, next),
			Verdict:    VerdictRedirect,
			RedirectTo: target,
			Answered:   true,
		}}
	default:
		return nil
	}
	return &probeStop{atHop(res, host, from)}
}

func atHop(res CheckResult, host, hop string) CheckResult {
	if !res.OK && res.Error != "" && !strings.EqualFold(hop, host) {
		res.Error = fmt.Sprintf("%s, at %s after a redirect from %s", res.Error, hop, host)
	}
	return res
}

func classifyProbeBody(ctxErr error, head []byte, read int64, speed float64, status int) (CheckResult, error) {
	if blockErr := netprobe.DetectBlockPageBody(head); blockErr != "" {
		return CheckResult{Error: blockErr, Verdict: netprobe.DomainISPPage, StatusCode: status}, nil
	}
	if ctxErr != nil {
		if errors.Is(ctxErr, context.Canceled) {
			return CheckResult{}, fmt.Errorf("the probe was cut short after %d bytes: %w", read, ctxErr)
		}
		verdict, detail := netprobe.ClassifyTLSErrorStaged(ctxErr, netprobe.StageRead, int(read))
		return CheckResult{
			Error:      fmt.Sprintf("%s after %d bytes", detail, read),
			Verdict:    verdict,
			BytesRead:  read,
			Speed:      speed,
			StatusCode: status,
		}, nil
	}
	if read < 1024 {
		return CheckResult{
			Error:      fmt.Sprintf("insufficient data: %d bytes (HTTP %d)", read, status),
			Verdict:    netprobe.DomainError,
			BytesRead:  read,
			StatusCode: status,
			Answered:   true,
		}, nil
	}
	return CheckResult{OK: true, Speed: speed, BytesRead: read, StatusCode: status, Answered: true}, nil
}
