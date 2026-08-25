package watchdog

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestIsReservedAddrCoversTheWholeLocalSpace(t *testing.T) {
	reserved := []string{
		"127.0.0.1", "::1",
		"10.1.2.3", "192.168.1.1", "172.16.0.1",
		"169.254.1.1", "fe80::1",
		"100.64.0.1", "100.127.255.255",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"240.0.0.1",
		"fd00::1",
		"::ffff:192.168.1.1",
	}
	for _, s := range reserved {
		if !isReservedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s must be refused: probing it reaches the network b4 runs on", s)
		}
	}

	public := []string{"1.1.1.1", "8.8.8.8", "104.22.45.1", "2606:4700::1111", "100.63.255.255", "100.128.0.1"}
	for _, s := range public {
		if isReservedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s is a public address and must not be refused", s)
		}
	}
}

func TestProbeHostRefusesLiteralPrivateAddress(t *testing.T) {
	var priv *ErrPrivateDestination
	_, err := ProbeHost(context.Background(), "192.168.1.1", ProbeOptions{Timeout: time.Second})
	if !errors.As(err, &priv) {
		t.Fatalf("a private literal must be refused before any connection, got %v", err)
	}
}

func TestProbeHostRejectsNonHostnames(t *testing.T) {
	for _, in := range []string{
		"http://10.0.0.1/cgi-bin/factory_reset",
		"https://127.0.0.1:8080/admin",
	} {
		var priv *ErrPrivateDestination
		_, err := ProbeHost(context.Background(), in, ProbeOptions{Timeout: time.Second})
		if !errors.As(err, &priv) {
			t.Errorf("%q reduces to a private host and must be refused, got %v", in, err)
		}
	}

	if _, err := ProbeHost(context.Background(), "   ", ProbeOptions{}); err == nil {
		t.Error("an empty host must be refused")
	}
}

func TestIsReservedHostOnlyJudgesLiterals(t *testing.T) {
	for _, in := range []string{"192.168.1.1", "127.0.0.1", "::1", "0.0.0.0", "https://10.0.0.1/admin"} {
		if !IsReservedHost(in) {
			t.Errorf("%q resolves to a reserved literal and must be refused", in)
		}
	}
	for _, in := range []string{"rutracker.org", "1.1.1.1", "example.com", ""} {
		if IsReservedHost(in) {
			t.Errorf("%q is not a reserved literal and must be allowed", in)
		}
	}
}

func fakeResolver(t *testing.T, answers map[uint16][][]byte) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			q := append([]byte(nil), buf[:n]...)
			if n < 12 {
				continue
			}
			i := 12
			for i < n && q[i] != 0 {
				i += int(q[i]) + 1
			}
			if i >= n || i+5 > n {
				continue
			}
			qtype := uint16(q[i+1])<<8 | uint16(q[i+2])
			resp := append([]byte(nil), q[:i+5]...)
			resp[2], resp[3] = 0x81, 0x80
			rrs := answers[qtype]
			resp[6], resp[7] = 0, byte(len(rrs))
			resp[8], resp[9], resp[10], resp[11] = 0, 0, 0, 0
			for _, rdata := range rrs {
				rr := []byte{0xC0, 0x0C, byte(qtype >> 8), byte(qtype), 0, 1, 0, 0, 0, 30, 0, byte(len(rdata))}
				resp = append(append(resp, rr...), rdata...)
			}
			pc.WriteTo(resp, from)
		}
	}()

	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", pc.LocalAddr().String())
		},
	}
	t.Cleanup(func() { net.DefaultResolver = previous })
}

func TestProbeHostRefusesWhenEveryAnswerIsReserved(t *testing.T) {
	fakeResolver(t, map[uint16][][]byte{
		1:  {{10, 0, 0, 7}, {192, 168, 3, 4}},
		28: {net.ParseIP("fd00::1").To16()},
	})

	var priv *ErrPrivateDestination
	_, err := ProbeHost(context.Background(), "sinkholed.invalid", ProbeOptions{Timeout: 3 * time.Second})
	if !errors.As(err, &priv) {
		t.Fatalf("a name whose every answer is reserved must be refused, got %v", err)
	}
}

func TestProbeHostDoesNotLatchARefusalWhenAPublicAnswerExists(t *testing.T) {
	fakeResolver(t, map[uint16][][]byte{
		1:  {{127, 0, 0, 1}, {203, 0, 113, 9}},
		28: {net.ParseIP("fd00::1").To16()},
	})

	var priv *ErrPrivateDestination
	res, err := ProbeHost(context.Background(), "mixed.invalid", ProbeOptions{Timeout: 3 * time.Second})
	if errors.As(err, &priv) {
		t.Fatalf("one reserved answer must not decide the verdict when a public address was also offered: %v", err)
	}
	if err != nil {
		t.Fatalf("the probe should report a verdict, not a caller error: %v", err)
	}
	if res.OK {
		t.Error("203.0.113.9 is unroutable here, so the probe cannot have succeeded")
	}
	if res.Verdict == "" {
		t.Error("a failed fetch must carry the verdict the tool exists to report")
	}
}

func TestChecksGoThroughTheEngine(t *testing.T) {
	cfg := config.NewConfig()

	if markThroughEngine == cfg.MainInjectedMark() {
		t.Fatalf("a check carrying the injected mark 0x%x is accepted before b4's queue, so it would report the unbypassed path",
			cfg.MainInjectedMark())
	}
	if markThroughEngine == cfg.DiscoveryFlowMark() {
		t.Fatalf("the discovery flow mark 0x%x only reaches a queue while a discovery run has installed its steering rules", cfg.DiscoveryFlowMark())
	}
	if markThroughEngine == uint(config.SelfDialMark) {
		t.Fatalf("the self-dial mark 0x%x is b4 exempting its own traffic, which is what a check must not do", config.SelfDialMark)
	}
}
