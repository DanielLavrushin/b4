package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const procRouteSample = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
br0	0032A8C0	00000000	0001	0	0	0	00FFFFFF	0	0	0
ppp0	00000000	0100400A	0003	0	0	10	00000000	0	0	0
b4tun0	00000000	00000000	0001	0	0	0	00000000	0	0	0
eth0	00000000	0101A8C0	0003	0	0	100	00000000	0	0	0
`

const procIPv6RouteSample = `00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200       lo
00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000002 00000000 00000003     ppp0
20010db8000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000001 00000000 00000001      br0
`

const procIfInet6Sample = `20010db8000000000000000000000005 03 40 00 01     ppp0
20010db8000000000000000000000006 03 40 00 80     ppp0
fe800000000000000000000000000001 03 40 20 80     ppp0
`

func stubHostFiles(t *testing.T, files map[string]string) {
	t.Helper()
	prev := readHostFile
	t.Cleanup(func() { readHostFile = prev })
	readHostFile = func(path string) ([]byte, error) {
		if s, ok := files[path]; ok {
			return []byte(s), nil
		}
		return nil, os.ErrNotExist
	}
}

func TestDefaultRouteParsersPickTheLowestMetricAndSkipTheTunDevice(t *testing.T) {
	if got := parseDefaultRouteV4(procRouteSample, ""); got != "b4tun0" {
		t.Fatalf("lowest metric is b4tun0 when nothing is skipped, got %q", got)
	}
	if got := parseDefaultRouteV4(procRouteSample, "b4tun0"); got != "ppp0" {
		t.Fatalf("with b4's TUN device skipped the uplink is ppp0, got %q", got)
	}
	if got := parseDefaultRouteV6(procIPv6RouteSample, ""); got != "ppp0" {
		t.Fatalf("the lo unreachable default must not count as a route, got %q", got)
	}
}

func TestParseIfInet6ReadsTheAddressFlags(t *testing.T) {
	flags := parseIfInet6(procIfInet6Sample)
	if f := flags[netip.MustParseAddr("2001:db8::5")]; f != ifaFlagTemporary {
		t.Fatalf("2001:db8::5 is a temporary address, flags=%#x", f)
	}
	if f := flags[netip.MustParseAddr("2001:db8::6")]; f != 0x80 {
		t.Fatalf("2001:db8::6 is permanent, flags=%#x", f)
	}
}

func TestAddressScopeNamesTheNATKinds(t *testing.T) {
	cases := map[string]string{
		"100.72.13.5":  "cgnat",
		"192.168.1.1":  "private",
		"10.0.0.1":     "private",
		"8.8.8.8":      "public",
		"2a00:1450::1": "public",
		"fd00::1":      "ula",
		"169.254.1.1":  "other",
	}
	for ip, want := range cases {
		if got := addressScope(netip.MustParseAddr(ip)); got != want {
			t.Errorf("%s: got %q, want %q", ip, got, want)
		}
	}
}

func TestCollectHostAddressesFindsWANAndLAN(t *testing.T) {
	stubHostFiles(t, map[string]string{
		procNetRouteFile:     procRouteSample,
		procNetIPv6RouteFile: procIPv6RouteSample,
		procNetIfInet6File:   procIfInet6Sample,
	})
	ifaces := []hostIface{
		{name: "lo", up: true, loopback: true, addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{name: "br0", up: true, addrs: []netip.Addr{netip.MustParseAddr("192.168.50.1"), netip.MustParseAddr("fd00::1")}},
		{name: "ppp0", up: true, addrs: []netip.Addr{
			netip.MustParseAddr("100.72.13.5"),
			netip.MustParseAddr("fe80::1"),
			netip.MustParseAddr("2001:db8::5"),
			netip.MustParseAddr("2001:db8::6"),
		}},
		{name: "docker0", up: true, addrs: []netip.Addr{netip.MustParseAddr("172.17.0.1")}},
		{name: "b4tun0", up: true, addrs: []netip.Addr{netip.MustParseAddr("10.99.0.1")}},
	}

	got := collectHostAddresses(ifaces, "b4tun0", "")
	if got.WANv4 == nil || *got.WANv4 != (HostAddress{Iface: "ppp0", IP: "100.72.13.5", Scope: "cgnat"}) {
		t.Fatalf("wan_v4: %+v", got.WANv4)
	}
	if got.WANv6 == nil || got.WANv6.IP != "2001:db8::6" {
		t.Fatalf("wan_v6 must skip the temporary privacy address that changes daily: %+v", got.WANv6)
	}
	if want := []HostAddress{{Iface: "br0", IP: "192.168.50.1", Scope: "private"}}; !reflect.DeepEqual(got.LAN, want) {
		t.Fatalf("lan must list the LAN bridge only, not docker or b4's TUN device: %+v", got.LAN)
	}

	overridden := collectHostAddresses(ifaces, "b4tun0", "br0")
	if overridden.WANv4 == nil || overridden.WANv4.Iface != "br0" {
		t.Fatalf("while TUN runs, its resolved uplink wins over the main table: %+v", overridden.WANv4)
	}
}

func TestHostAddressesEndpointProbesThePublicAddressOnlyWhenAsked(t *testing.T) {
	stubHostFiles(t, map[string]string{})
	prevIfaces, prevLookup := hostInterfaces, publicIPv4Lookup
	t.Cleanup(func() {
		hostInterfaces, publicIPv4Lookup = prevIfaces, prevLookup
		publicIPv4Cached, publicIPv4CachedAt = "", time.Time{}
	})
	hostInterfaces = func() []hostIface { return nil }
	publicIPv4Cached, publicIPv4CachedAt = "", time.Time{}
	calls := 0
	publicIPv4Lookup = func(context.Context, uint) (string, error) {
		calls++
		return "203.0.113.7", nil
	}

	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	api := &API{cfgPtr: testCfgPtr(&cfg)}

	get := func(url string) HostAddressesResponse {
		rec := httptest.NewRecorder()
		api.handleHostAddresses(rec, httptest.NewRequest(http.MethodGet, url, nil))
		var out HostAddressesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v (%s)", url, err, rec.Body.String())
		}
		return out
	}

	if out := get("/api/system/addresses"); out.PublicV4 != "" || calls != 0 || !out.Success || out.LAN == nil {
		t.Fatalf("without ?public=1 no third party may be contacted: %+v calls=%d", out, calls)
	}
	if out := get("/api/system/addresses?public=1"); out.PublicV4 != "203.0.113.7" {
		t.Fatalf("public probe: %+v", out)
	}
	get("/api/system/addresses?public=1")
	if calls != 1 {
		t.Fatalf("the public address is cached, looked up %d times", calls)
	}

	publicIPv4Cached = ""
	publicIPv4Lookup = func(context.Context, uint) (string, error) {
		return "", errors.New("none of the IP lookup services answered")
	}
	if out := get("/api/system/addresses?public=1"); out.PublicError == "" || out.PublicV4 != "" {
		t.Fatalf("a failed probe must say so: %+v", out)
	}
}
