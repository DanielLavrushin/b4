package nfq

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
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

func withoutCanary(t *testing.T, logs *syncBuffer, wanted ...string) {
	t.Helper()
	got := logs.String()
	for _, want := range wanted {
		if !strings.Contains(got, want) {
			t.Errorf("the log misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "canary") {
		t.Errorf("the log holds a private part of the resolver URL:\n%s", got)
	}
}

func TestARedirectedAnswerDoesNotLogTheResolversPath(t *testing.T) {
	w, cfg, set := listWorker(t)
	good := dscpDoHServer(t, w, "203.0.113.51")
	set.DNS.DoHURL = strings.TrimSuffix(good, "/dns-query") + canaryResolverPath

	logs := captureLog(t)
	log.SetLevel(log.LevelDebug)
	replies := watchReplies(t)
	askOverUDP(w, cfg, set, 51)
	onlyAddress(t, oneReply(t, replies), "203.0.113.51")

	tcpDNSUpstream(t)
	resp := dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 52, dnsTypeA)), "the TCP answer never came")
	onlyAddress(t, resp, "203.0.113.51")
	withoutCanary(t, logs, "answered for 192.168.1.100 with 1 IPs", "127.0.0.1")
}

func TestAFailedRedirectDoesNotLogTheResolversPath(t *testing.T) {
	for _, tc := range []struct {
		name   string
		strict bool
		wanted string
	}{
		{"strict", true, "the client gets SERVFAIL"},
		{"with a fallback", false, "falling back to"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, cfg, set := listWorker(t)
			set.DNS.DoHURL = "http://" + refusedAddr(t) + canaryResolverPath
			set.DNS.Strict = tc.strict
			resetDNSAnswerCache()
			t.Cleanup(resetDNSAnswerCache)
			prev := askOriginalResolver
			t.Cleanup(func() { askOriginalResolver = prev })
			askOriginalResolver = func(_ context.Context, query []byte, _ net.IP, _ dns.ForwardOptions) ([]byte, error) {
				return dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP("203.0.113.52")}), nil
			}

			logs := captureLog(t)
			log.SetLevel(log.LevelDebug)
			watchReplies(t)
			askOverUDP(w, cfg, set, 53)
			withoutCanary(t, logs, "could not answer www.example.com", tc.wanted, "connect: connection refused")
		})
	}
}

func TestAFailedRedirectOverTCPDoesNotLogTheResolversPath(t *testing.T) {
	w, _, set := listWorker(t)
	set.DNS.DoHURL = "http://" + refusedAddr(t) + canaryResolverPath
	resetDNSAnswerCache()
	t.Cleanup(resetDNSAnswerCache)
	tcpDNSUpstream(t)

	logs := captureLog(t)
	log.SetLevel(log.LevelDebug)
	dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 54, dnsTypeA)), "the TCP answer never came")
	withoutCanary(t, logs, "DNS TCP: the redirect could not answer www.example.com", "connect: connection refused")
}
