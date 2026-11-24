package dns

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/utils"
	"golang.org/x/sys/unix"
)

const canaryResolverPath = "/canarypath/canaryid?token=canaryquery"

func refusedAddr(t *testing.T) string {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	bound, err := unix.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("127.0.0.1:%d", bound.(*unix.SockaddrInet4).Port)
}

func echoDoHServer(t *testing.T, format string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if r, err := http.ReadRequest(bufio.NewReader(conn)); err == nil {
					fmt.Fprintf(conn, format, r.RequestURI)
				}
			}()
		}
	}()
	return "http://" + ln.Addr().String() + canaryResolverPath
}

func canaryDoHServer(t *testing.T, answer http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(answer)
	t.Cleanup(srv.Close)
	return srv.URL + canaryResolverPath
}

func rcodeAnswer(rcode byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		if len(query) < 12 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		binary.BigEndian.PutUint16(query[2:4], 0x8180|uint16(rcode))
		w.Header().Set("Content-Type", DoHContentType)
		_, _ = w.Write(query)
	}
}

func refusedError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

func TestAFailedLookupNamesTheResolverWithoutItsPath(t *testing.T) {
	closed := refusedAddr(t)
	for _, tc := range []struct {
		name  string
		url   string
		names string
		check func(error) bool
	}{
		{"refused connection", "https://" + closed + canaryResolverPath, closed, refusedError},
		{"user name in the URL", "https://canaryuser:canarypass@" + closed + canaryResolverPath, closed, refusedError},
		{"URL that does not parse", "https://canaryuser:canary^pass@dns.example" + canaryResolverPath, "https://", nil},
		{"escape that does not parse", "https://dns.example/canary%zz", "https://", nil},
		{"forgotten host", "https://canaryuser:canarypass/dns-query", "https://", nil},
		{"forgotten host with a number for a password", "https://canaryuser:123456789/dns-query", "https://", nil},
		{"status code that repeats the request", echoDoHServer(t, "HTTP/1.1 %s\r\n\r\n"), utils.ErrURLReply.Error(), nil},
		{"header that repeats the request", echoDoHServer(t, "HTTP/1.1 200 OK\r\n%s\r\n\r\n"), utils.ErrURLReply.Error(), nil},
		{"redirect to a dead host", canaryDoHServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "http://"+closed+canaryResolverPath)
			w.WriteHeader(http.StatusTemporaryRedirect)
		}), "redirected to " + closed, refusedError},
		{"redirect that does not parse", canaryDoHServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "http://[canaryhost/canarypath")
			w.WriteHeader(http.StatusTemporaryRedirect)
		}), "redirect", nil},
		{"redirect loop", canaryDoHServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", r.URL.String())
			w.WriteHeader(http.StatusTemporaryRedirect)
		}), "redirects", nil},
		{"HTTP error", canaryDoHServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}), "403", nil},
		{"no answer in time", canaryDoHServer(t, func(_ http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			<-r.Context().Done()
		}), "127.0.0.1", func(err error) bool {
			var timeout interface{ Timeout() bool }
			return errors.Is(err, context.DeadlineExceeded) && errors.As(err, &timeout) && timeout.Timeout()
		}},
		{"name without an address", canaryDoHServer(t, rcodeAnswer(0)), "127.0.0.1", func(err error) bool {
			return errors.Is(err, ErrNoAddress) && !errors.Is(err, ErrTransientAnswer)
		}},
		{"name that does not exist", canaryDoHServer(t, rcodeAnswer(3)), "127.0.0.1", func(err error) bool {
			return errors.Is(err, ErrNoAddress)
		}},
		{"SERVFAIL", canaryDoHServer(t, rcodeAnswer(2)), "127.0.0.1", func(err error) bool {
			return errors.Is(err, ErrTransientAnswer)
		}},
		{"answer to another query", canaryDoHServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", DoHContentType)
			_, _ = w.Write([]byte{1, 2, 3})
		}), "127.0.0.1", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LookupIPs(context.Background(), Server{Source: tc.url, DoHURL: tc.url, Timeout: 300 * time.Millisecond}, "www.example.com", true, false)
			if err == nil {
				t.Fatal("the lookup must fail")
			}
			if strings.Contains(err.Error(), "canary") || strings.Contains(err.Error(), "123456789") {
				t.Errorf("the error holds a private part of the resolver URL: %v", err)
			}
			if strings.Contains(err.Error(), "redirected to") && !strings.HasPrefix(tc.name, "redirect") {
				t.Errorf("the error names a redirect that never was: %v", err)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("the error must still say what failed (%q): %v", tc.names, err)
			}
			if tc.check != nil && !tc.check(err) {
				t.Errorf("callers no longer recognise the failure: %v", err)
			}
		})
	}
}

func TestTheResolverLabelIsItsHostOnly(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://dns.example/canarypath", "dns.example"},
		{"https://dns.example:8443/canarypath?id=canaryquery#canaryfragment", "dns.example:8443"},
		{"https://canaryuser:canarypass@dns.example/canarypath", "dns.example"},
		{"https://canaryuser:canary/pass@dns.example/", "[redacted]"},
		{"https://dns.example/canary%zz", "[redacted]"},
		{"https://canaryuser:123456789/dns-query", "[redacted]"},
		{"192.0.2.1", "192.0.2.1"},
		{"2001:db8::1", "2001:db8::1"},
		{"", ""},
	} {
		if got := SourceLabel(tc.in); got != tc.want {
			t.Errorf("SourceLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := (Server{DoHURL: tc.in}).String(); tc.in != "" && strings.Contains(got, "canary") {
			t.Errorf("the server %q prints as %q", tc.in, got)
		}
	}
}
