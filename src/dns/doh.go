package dns

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const DoHContentType = "application/dns-message"

var ErrDoHMethodNotAllowed = errors.New("doh method not allowed")

var (
	dohPingIdle    = 5 * time.Second
	dohPingTimeout = 5 * time.Second
)

func MarkedDoHClient(mark int, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          100,
		IdleConnTimeout:       30 * time.Second,
		HTTP2:                 &http.HTTP2Config{SendPingTimeout: dohPingIdle, PingTimeout: min(timeout, dohPingTimeout)},
	}
	var control func(string, string, syscall.RawConn) error
	if mark != 0 {
		control = func(_, _ string, c syscall.RawConn) error {
			var serr error
			if cerr := c.Control(func(fd uintptr) {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
			}); cerr != nil {
				return cerr
			}
			return serr
		}
	}
	d := &net.Dialer{Timeout: timeout, Control: control}
	d.Resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			bd := &net.Dialer{Timeout: timeout, Control: control}
			return bd.DialContext(ctx, network, address)
		},
	}
	tr.DialContext = d.DialContext
	return &http.Client{Transport: tr, Timeout: timeout}
}

type dohSend func(*http.Request) (*http.Response, error)

func ResolveDoH(ctx context.Context, client *http.Client, serverURL string, query []byte) ([]byte, error) {
	return resolveDoH(ctx, client.Do, serverURL, query)
}

func ResolveSetDoH(ctx context.Context, client *http.Client, serverURL string, query []byte) ([]byte, error) {
	return resolveDoH(ctx, func(req *http.Request) (*http.Response, error) {
		return sendAgainIfLost(client, req)
	}, serverURL, query)
}

func resolveDoH(ctx context.Context, send dohSend, serverURL string, query []byte) ([]byte, error) {
	body, err := dohPOST(ctx, send, serverURL, query)
	if errors.Is(err, ErrDoHMethodNotAllowed) {
		body, err = dohGET(ctx, send, serverURL, query)
	}
	return body, err
}

func dohPOST(ctx context.Context, send dohSend, serverURL string, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", DoHContentType)
	req.Header.Set("Content-Type", DoHContentType)
	return dohDo(send, req)
}

func dohGET(ctx context.Context, send dohSend, serverURL string, query []byte) ([]byte, error) {
	enc := base64.RawURLEncoding.EncodeToString(query)
	sep := "?"
	if strings.ContainsRune(serverURL, '?') {
		sep = "&"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+sep+"dns="+enc, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", DoHContentType)
	return dohDo(send, req)
}

func sendAgainIfLost(client *http.Client, req *http.Request) (*http.Response, error) {
	var reused atomic.Bool
	trace := &httptrace.ClientTrace{GotConn: func(got httptrace.GotConnInfo) { reused.Store(got.Reused) }}
	resp, err := client.Do(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
	if err == nil || !reused.Load() || timedOut(err) || req.Context().Err() != nil {
		return resp, err
	}
	again := req.Clone(req.Context())
	if req.GetBody != nil {
		if again.Body, err = req.GetBody(); err != nil {
			return nil, err
		}
	}
	return client.Do(again)
}

func timedOut(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

func dohDo(send dohSend, req *http.Request) ([]byte, error) {
	resp, err := send(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		io.Copy(io.Discard, resp.Body)
		return nil, ErrDoHMethodNotAllowed
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("doh status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 65536))
}
