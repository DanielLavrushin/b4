package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

const (
	hostReadCap    = 100 << 10
	hostSmallBytes = 32 << 10
	hostHeadBytes  = 4 << 10
)

type hostFailure uint8

const (
	hostFailNone hostFailure = iota
	hostFailBlocked
	hostFailUnreachable
	hostFailName
)

type hostProbe struct {
	ok       bool
	small    bool
	aborted  bool
	reserved bool
	failure  hostFailure
	status   int
	bytes    int64
	wire     int64
	err      string
}

func (ds *DiscoverySuite) hostTimeout() time.Duration {
	if ds.cfg == nil || ds.cfg.System.Checker.DiscoveryTimeoutSec <= 0 {
		return 5 * time.Second
	}
	return time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second
}

func (ds *DiscoverySuite) probeHost(ctx context.Context, rawURL, origin string, with bool) hostProbe {
	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" {
		return hostProbe{failure: hostFailUnreachable, err: "the sample URL cannot be parsed"}
	}
	timeout := ds.hostTimeout()
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	reqCtx, abort := context.WithCancel(probeCtx)
	defer abort()

	transport := &http.Transport{
		TLSClientConfig:       ds.tlsConfig(),
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
		ForceAttemptHTTP2:     true,
	}
	defer transport.CloseIdleConnections()

	var armed, stalled atomic.Bool
	var wire atomic.Int64
	stall := time.AfterFunc(probeStallTimeout, func() {
		stalled.Store(true)
		abort()
	})
	stall.Stop()
	defer func() {
		armed.Store(false)
		stall.Stop()
	}()
	dial := ds.dialContext(timeout, target.Hostname(), "")
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &progressConn{Conn: countedConn{Conn: conn, read: &wire}, onRead: func() {
			if armed.Load() {
				stall.Reset(probeStallTimeout)
			}
		}}, nil
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return hostProbe{failure: hostFailUnreachable, err: err.Error()}
	}
	req.Header.Set("User-Agent", netprobe.ProbeUserAgent)
	req.Header.Set("Accept", "*/*")
	if origin != "" {
		req.Header.Set("Referer", origin)
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return hostProbe{aborted: true}
		}
		failure, detail := classifyHostError(err)
		var reserved *netprobe.ReservedAddrError
		return hostProbe{failure: failure, reserved: errors.As(err, &reserved), err: detail}
	}
	defer resp.Body.Close()

	p := hostProbe{status: resp.StatusCode}
	if failure, detail := hostStatusFailure(resp, target, with); failure != hostFailNone {
		p.failure, p.err = failure, detail
		return p
	}

	html := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html")
	buf := make([]byte, 16<<10)
	var head []byte
	eof := false
read:
	for p.bytes < hostReadCap || wire.Load() < hostReadCap {
		size := int64(len(buf))
		if p.bytes < hostReadCap {
			size = min(size, hostReadCap-p.bytes)
		}
		n, err := resp.Body.Read(buf[:size])
		if n > 0 {
			if !armed.Swap(true) {
				stall.Reset(probeStallTimeout)
			}
			p.bytes += int64(n)
			if html && len(head) < hostHeadBytes {
				head = append(head, buf[:min(n, hostHeadBytes-len(head))]...)
			}
		}
		switch {
		case err == nil:
		case err == io.EOF:
			eof = true
			break read
		case ctx.Err() != nil:
			return hostProbe{aborted: true}
		case stalled.Load():
			p.failure, p.err = hostFailBlocked, fmt.Sprintf("stalled after %d bytes", p.bytes)
			return p
		case probeCtx.Err() != nil && wire.Load() >= hostSmallBytes:
			break read
		case probeCtx.Err() != nil:
			p.failure, p.err = hostFailBlocked, fmt.Sprintf("timed out after %d bytes", p.bytes)
			return p
		default:
			p.failure, p.err = hostFailBlocked, fmt.Sprintf("read error after %d bytes: %v", p.bytes, err)
			return p
		}
	}

	if html {
		if blocked := netprobe.DetectBlockPageBody(head); blocked != "" {
			p.failure, p.err = hostFailBlocked, blocked
			return p
		}
	}
	p.ok = true
	p.wire = wire.Load()
	p.small = (resp.StatusCode >= 300 && resp.StatusCode < 400) || (eof && p.wire < hostSmallBytes)
	return p
}

type countedConn struct {
	net.Conn
	read *atomic.Int64
}

func (c countedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.read.Add(int64(n))
	return n, err
}

func hostStatusFailure(resp *http.Response, target *url.URL, with bool) (hostFailure, string) {
	status := resp.StatusCode
	switch {
	case status == http.StatusUnavailableForLegalReasons:
		return hostFailBlocked, "ISP block page (HTTP 451)"
	case status >= 300 && status < 400:
		if loc, err := resp.Location(); err == nil && netprobe.IsBlockPageRedirectFrom(target, loc) {
			return hostFailBlocked, (&blockPageRedirect{target: loc.String()}).Error()
		}
	case with && status == http.StatusBadRequest:
		return hostFailBlocked, "server answered HTTP 400, the strategy corrupted the request"
	case with && status >= 500:
		return hostFailUnreachable, fmt.Sprintf("server answered HTTP %d", status)
	}
	return hostFailNone, ""
}

func classifyHostError(err error) (hostFailure, string) {
	var reserved *netprobe.ReservedAddrError
	if errors.As(err, &reserved) {
		return hostFailName, reserved.Error()
	}
	status, detail := netprobe.ClassifyTLSError(err)
	if lookupFailureOf(err) != lookupOK {
		return hostFailName, detail
	}
	switch status {
	case netprobe.DomainTimeout, netprobe.DomainTLSDrop, netprobe.DomainSYNDrop, netprobe.DomainTCP16, netprobe.DomainTLSReset, netprobe.DomainTLSSpoof:
		return hostFailBlocked, detail
	}
	if strings.Contains(strings.ToLower(err.Error()), "does not look like a tls handshake") {
		return hostFailBlocked, "Non-TLS response received (DPI replacement)"
	}
	return hostFailUnreachable, detail
}
