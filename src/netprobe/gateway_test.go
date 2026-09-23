package netprobe

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestPublicAddress(t *testing.T) {
	for _, ip := range []string{"8.8.8.8", "157.240.253.174", "2a03:2880:f10c:83:face:b00c:0:25de", "1.1.1.1"} {
		if !PublicAddress(net.ParseIP(ip)) {
			t.Errorf("%s must be public", ip)
		}
	}
	for _, ip := range []string{
		"10.0.0.1", "172.16.5.4", "192.168.1.1", "127.0.0.1", "169.254.1.1", "100.70.0.1",
		"0.0.0.0", "0.1.2.3", "192.0.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1",
		"224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::1", "::", "fe80::1", "fd00::1", "ff02::1", "2001:db8::1", "100::1",
	} {
		if PublicAddress(net.ParseIP(ip)) {
			t.Errorf("%s must not be public", ip)
		}
	}
	if PublicAddress(nil) {
		t.Error("nil is not an address")
	}
}

func stubGatewayDial(t *testing.T, fn func(network, addr string) (net.Conn, error)) *[]string {
	t.Helper()
	orig := gatewayDial
	var dialed []string
	gatewayDial = func(_ context.Context, network, addr string, _ int, _ time.Duration) (net.Conn, error) {
		dialed = append(dialed, network+" "+addr)
		return fn(network, addr)
	}
	t.Cleanup(func() { gatewayDial = orig })
	return &dialed
}

func TestGatewayTerminatesClassifiesTheDialOutcome(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"connect succeeds", nil, true},
		{"refused by the first hop", syscall.ECONNREFUSED, true},
		{"wrapped refusal", &net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}, true},
		{"reset by the first hop right after accepting", syscall.ECONNRESET, true},
		{"wrapped reset", &net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNRESET}}, true},
		{"host unreachable", syscall.EHOSTUNREACH, false},
		{"network unreachable", syscall.ENETUNREACH, false},
		{"timeout", context.DeadlineExceeded, false},
		{"other error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubGatewayDial(t, func(string, string) (net.Conn, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				a, b := net.Pipe()
				b.Close()
				return a, nil
			})
			if got := GatewayTerminates(context.Background(), "8.8.8.8", 443, 0, time.Second); got != tc.want {
				t.Fatalf("GatewayTerminates = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGatewayTerminatesPicksTheFamilyAndSkipsNonPublicTargets(t *testing.T) {
	dialed := stubGatewayDial(t, func(string, string) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	})

	if !GatewayTerminates(context.Background(), "2a03:2880:f10c:83:face:b00c:0:25de", 443, 7, time.Second) {
		t.Fatal("a refused v6 dial means the first hop terminates it")
	}
	if len(*dialed) != 1 || (*dialed)[0] != "tcp6 [2a03:2880:f10c:83:face:b00c:0:25de]:443" {
		t.Fatalf("v6 target must dial tcp6 on the given port, got %v", *dialed)
	}

	for _, ip := range []string{"192.168.1.1", "10.1.2.3", "127.0.0.1", "100.64.0.1", "fe80::1", "not-an-ip"} {
		if GatewayTerminates(context.Background(), ip, 443, 0, time.Second) {
			t.Errorf("%s must never be probed as gateway-terminated", ip)
		}
	}
	if len(*dialed) != 1 {
		t.Fatalf("non-public targets must not be dialed at all, got %v", *dialed)
	}
}
