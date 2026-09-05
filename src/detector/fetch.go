package detector

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

const (
	fetchConnectTimeout = 8 * time.Second
	fetchReadTimeout    = 12 * time.Second
	fetchMaxBody        = 100 * 1024
	fetchUserAgent      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)

var (
	caBundleOnce    sync.Once
	caBundleMissing bool
)

func noCABundle() bool {
	caBundleOnce.Do(func() {
		pool, err := x509.SystemCertPool()
		caBundleMissing = err != nil || pool == nil || pool.Equal(x509.NewCertPool())
	})
	return caBundleMissing
}

func (s *Suite) fetchSite(ctx context.Context, domain, rawURL, ip string, mark uint, maxTLS uint16) Fetch {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return Fetch{Status: netprobe.DomainError, Detail: "invalid URL"}
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	ctx, cancel := context.WithTimeout(ctx, fetchConnectTimeout+fetchReadTimeout)
	defer cancel()

	start := time.Now()
	conn, err := netprobe.Dialer(int(mark), fetchConnectTimeout, 0).DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
	if err != nil {
		st, detail := netprobe.ClassifyTLSErrorStaged(err, netprobe.StageConnect, 0)
		return Fetch{Status: st, Detail: detail, LatencyMs: ms(start)}
	}
	defer conn.Close()

	tlsConf := &tls.Config{
		ServerName:         domain,
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         maxTLS,
		InsecureSkipVerify: noCABundle(),
	}
	tlsConn := tls.Client(conn, tlsConf)
	hctx, hcancel := context.WithTimeout(ctx, fetchConnectTimeout)
	err = tlsConn.HandshakeContext(hctx)
	hcancel()
	latency := ms(start)
	if err != nil {
		st, detail := netprobe.ClassifyTLSErrorStaged(err, netprobe.StageHandshake, 0)
		return Fetch{Status: st, Detail: detail, LatencyMs: latency}
	}

	deadline := time.Now().Add(fetchReadTimeout)
	tlsConn.SetDeadline(deadline)
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nAccept: */*\r\nConnection: close\r\n\r\n", path, u.Host, fetchUserAgent)
	if _, err := tlsConn.Write([]byte(req)); err != nil {
		st, detail := netprobe.ClassifyTLSErrorStaged(err, netprobe.StageRead, 0)
		return Fetch{Status: st, Detail: detail, LatencyMs: latency}
	}

	reader := bufio.NewReader(tlsConn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		st, detail := netprobe.ClassifyTLSErrorStaged(err, netprobe.StageRead, reader.Buffered())
		return Fetch{Status: st, Detail: detail, LatencyMs: latency}
	}
	defer resp.Body.Close()

	out := Fetch{Status: FetchOk, LatencyMs: latency, StatusCode: resp.StatusCode}
	location := resp.Header.Get("Location")
	out.RedirectTo = location

	if resp.StatusCode == 451 {
		out.Status = netprobe.DomainISPPage
		out.Detail = "HTTP 451 Unavailable For Legal Reasons"
		return out
	}
	if location != "" && netprobe.IsBlockPageRedirect(location) {
		out.Status = netprobe.DomainISPPage
		out.Detail = "redirect to ISP block page " + location
		return out
	}

	head := make([]byte, 0, 4096)
	buf := make([]byte, 16*1024)
	var read int64
	for read < fetchMaxBody {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			read += int64(n)
			if len(head) < 4096 {
				head = append(head, buf[:n]...)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if strings.Contains(strings.ToLower(rerr.Error()), "unexpected eof") && read > 0 && resp.ContentLength < 0 {
				break
			}
			st, detail := netprobe.ClassifyTLSErrorStaged(rerr, netprobe.StageRead, int(read))
			out.Status = st
			out.Detail = fmt.Sprintf("%s after %d bytes", detail, read)
			out.Bytes = read
			return out
		}
	}
	out.Bytes = read

	if netprobe.DetectBlockPageBody(head) != "" {
		out.Status = netprobe.DomainISPPage
		out.Detail = "ISP block page in the response body"
		return out
	}
	if resp.StatusCode >= 500 {
		out.Status = FetchServer
		out.Detail = fmt.Sprintf("HTTP %d from the site itself", resp.StatusCode)
		return out
	}
	if location != "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if host := redirectHost(location); host != "" && !sameSite(domain, host) {
			out.Detail = "redirects to " + host
		}
	}
	if out.Detail == "" {
		out.Detail = fmt.Sprintf("HTTP %d, %s", resp.StatusCode, tlsVersionName(tlsConn.ConnectionState().Version))
	}
	return out
}

func (s *Suite) probePlainHTTP(ctx context.Context, domain, ip string, mark uint) (FetchStatus, string) {
	ctx, cancel := context.WithTimeout(ctx, fetchConnectTimeout)
	defer cancel()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return netprobe.Dialer(int(mark), fetchConnectTimeout, 0).DialContext(ctx, "tcp", net.JoinHostPort(ip, "80"))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   fetchConnectTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+domain+"/", nil)
	if err != nil {
		return netprobe.DomainError, err.Error()
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		st, detail := netprobe.ClassifyTLSErrorStaged(err, netprobe.StageConnect, 0)
		if st == netprobe.DomainTLSDrop {
			st = netprobe.DomainTimeout
		}
		return st, detail
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	body := make([]byte, 8192)
	n, _ := io.ReadFull(resp.Body, body)
	st, detail := netprobe.ClassifyHTTPResponse(resp.StatusCode, location, string(body[:n]))
	if st != FetchOk {
		return st, detail
	}
	if location != "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		host := redirectHost(location)
		if host != "" && !sameSite(domain, host) && !cdnRedirect(host) {
			return netprobe.DomainISPPage, "redirect off-site to " + host
		}
	}
	return FetchOk, "HTTP " + strconv.Itoa(resp.StatusCode)
}

func redirectHost(location string) string {
	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func sameSite(domain, host string) bool {
	d := strings.TrimPrefix(strings.ToLower(domain), "www.")
	h := strings.TrimPrefix(host, "www.")
	return d == h || strings.HasSuffix(h, "."+d) || strings.HasSuffix(d, "."+h)
}

func cdnRedirect(host string) bool {
	for _, p := range Lists().CDNRedirectPattern {
		if strings.Contains(host, p) {
			return true
		}
	}
	return false
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	default:
		return "TLS"
	}
}

func ms(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

func isBlockedStatus(st FetchStatus) bool {
	switch st {
	case netprobe.DomainTLSDPI, netprobe.DomainTLSMITM, netprobe.DomainTLSSpoof, netprobe.DomainTLSAlert,
		netprobe.DomainTLSReset, netprobe.DomainTLSDrop, netprobe.DomainSYNDrop, netprobe.DomainTCP16,
		netprobe.DomainISPPage, netprobe.DomainBlocked, netprobe.DomainDNSFake, netprobe.DomainTimeout:
		return true
	}
	return false
}

func isFakeRange(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range []string{"198.18.0.0/15", "0.0.0.0/8", "127.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "192.168.0.0/16", "172.16.0.0/12", "::1/128", "100::/64"} {
		_, n, _ := net.ParseCIDR(cidr)
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}
