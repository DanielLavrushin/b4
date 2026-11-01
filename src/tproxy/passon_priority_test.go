package tproxy

import (
	"bytes"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func TestTheSetThatListsTheAddressGoesFirstWithIPPriority(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	byAddress := proxySetFor("by-address", nil, []string{"127.0.0.1/32"})
	byAddress.Targets.IPPriority = true
	byName := proxySetFor("by-name", []string{"api.telegram.org"}, nil)
	upAddress := startMockSocks(t, len(hello), okReply)
	upName := startMockSocks(t, 0, okReply)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{byAddress, byName},
		peerListener(t, upAddress.port(), byAddress), peerListener(t, upName.port(), byName))

	proxyThrough(t, bridge, hello)
	if r := upAddress.next(t); r.atyp != 1 || r.host != "127.0.0.1" || !bytes.Equal(r.payload, hello) {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the address with the hello intact", r.atyp, r.host)
	}
	noSocksRequest(t, upName, 200*time.Millisecond)
}

func TestASetWithIPPriorityThatCannotTakeTheConnectionStillKeepsItFromTheNameSet(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	dpiOnly := proxySetFor("dpi-only", nil, []string{"127.0.0.1/32"})
	dpiOnly.Routing.Enabled = false
	dpiOnly.Targets.IPPriority = true
	byName := proxySetFor("by-name", []string{"api.telegram.org"}, nil)
	upName := startMockSocks(t, 0, okReply)
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{dpiOnly, byName}, peerListener(t, upName.port(), byName))

	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, upName, 200*time.Millisecond)
}

func TestASetAboveTheSetWithIPPriorityStillTakesTheNameItLists(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	byName := proxySetFor("by-name", []string{"api.telegram.org"}, nil)
	byAddress := proxySetFor("by-address", nil, []string{"127.0.0.1/32"})
	byAddress.Targets.IPPriority = true
	upName := startMockSocks(t, len(hello), okReply)
	upAddress := startMockSocks(t, 0, okReply)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{byName, byAddress},
		peerListener(t, upName.port(), byName), peerListener(t, upAddress.port(), byAddress))

	proxyThrough(t, bridge, hello)
	if r := upName.next(t); r.host != "api.telegram.org" {
		t.Fatalf("CONNECT asked for %q, want the name of the set listed first", r.host)
	}
	noSocksRequest(t, upAddress, 200*time.Millisecond)
}

func TestASetWithATLSVersionFilterNeverTakesTheConnectionFromTheNameSet(t *testing.T) {
	for _, tc := range []struct {
		filter  string
		version uint16
	}{{"1.2", tls.VersionTLS12}, {"1.3", tls.VersionTLS13}} {
		t.Run("TLS "+tc.filter, func(t *testing.T) {
			hello := realClientHello(t, "api.telegram.org", tc.version)
			if _, version := sniffedHost(hello); version != tc.version {
				t.Fatalf("the ClientHello offers version %#x, want %#x", version, tc.version)
			}
			byAddress := proxySetFor("by-address", nil, []string{"127.0.0.1/32"})
			byAddress.Targets.IPPriority = true
			byAddress.Targets.TLSVersion = tc.filter
			byName := proxySetFor("by-name", []string{"api.telegram.org"}, nil)
			upName := startMockSocks(t, len(hello), okReply)
			upAddress := startMockSocks(t, 0, okReply)
			bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{byAddress, byName},
				peerListener(t, upAddress.port(), byAddress), peerListener(t, upName.port(), byName))

			proxyThrough(t, bridge, hello)
			if r := upName.next(t); r.host != "api.telegram.org" {
				t.Fatalf("CONNECT asked for %q, want the name of the set that lists it", r.host)
			}
			noSocksRequest(t, upAddress, 200*time.Millisecond)
		})
	}
}

func TestPassOnSetPutsTheAddressFirstOnlyForAViableSetWithIPPriorityRankedAboveTheNameSet(t *testing.T) {
	const phone = "aa:bb:cc:dd:ee:01"
	everything := func(*config.SetConfig) bool { return true }
	cases := []struct {
		name       string
		setup      func(addr, byName *config.SetConfig) []*config.SetConfig
		host       string
		dst        string
		dport      uint16
		mac        string
		applies    sni.SetFilter
		want       string
		wantByName bool
	}{
		{name: "the switch is off", want: "name", wantByName: true, setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.Targets.IPPriority = false
			return []*config.SetConfig{addr, byName}
		}},
		{name: "the address set sits above the name set", want: "address"},
		{name: "the name set sits above the address set", want: "name", wantByName: true, setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			return []*config.SetConfig{byName, addr}
		}},
		{name: "no set lists the name", host: "web.telegram.org", want: "address"},
		{name: "the address set's port filter leaves the port out", dport: 8443, want: "name", wantByName: true, setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.TCPPortRanges = []config.PortRange{{Min: 443, Max: 443}}
			return []*config.SetConfig{addr, byName}
		}},
		{name: "the address set's port filter takes the port", want: "address", setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.TCPPortRanges = []config.PortRange{{Min: 443, Max: 443}}
			return []*config.SetConfig{addr, byName}
		}},
		{name: "a set with a TLS version filter never goes first: 1.2", want: "name", wantByName: true, setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.Targets.TLSVersion = "1.2"
			return []*config.SetConfig{addr, byName}
		}},
		{name: "a set with a TLS version filter never goes first: 1.3", want: "name", wantByName: true, setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.Targets.TLSVersion = "1.3"
			return []*config.SetConfig{addr, byName}
		}},
		{name: "the address set's IP version filter leaves IPv4 out", want: "name", wantByName: true, setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.Targets.IPVersion = "6"
			return []*config.SetConfig{addr, byName}
		}},
		{name: "an IPv6-only address set goes first for an IPv6 destination", dst: "2001:db8::1", want: "address", setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.Targets.IpsToMatch = []string{"2001:db8::1/128"}
			addr.Targets.IPVersion = "6"
			return []*config.SetConfig{addr, byName}
		}},
		{name: "a catch-all range never goes first", want: "name", wantByName: true, setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.Targets.IpsToMatch = []string{"0.0.0.0/0"}
			return []*config.SetConfig{addr, byName}
		}},
		{name: "an address set that does not apply to the client", want: "name", wantByName: true, applies: func(set *config.SetConfig) bool { return set.Id != "address" }},
		{name: "the address step passes over a set that does not apply to the client", host: "web.telegram.org", applies: func(set *config.SetConfig) bool { return set.Id != "address" }, setup: func(addr, _ *config.SetConfig) []*config.SetConfig {
			return []*config.SetConfig{addr}
		}},
		{name: "an address set that applies but cannot take the connection", setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.Routing.Enabled = false
			return []*config.SetConfig{addr, byName}
		}},
		{name: "a name set limited to the device below the address set", mac: phone, want: "name", wantByName: true, setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			byName.Targets.SourceDevices = []string{phone}
			return []*config.SetConfig{addr, byName}
		}},
		{name: "an address set limited to the device below a general name set", mac: phone, want: "address", setup: func(addr, byName *config.SetConfig) []*config.SetConfig {
			addr.Targets.SourceDevices = []string{phone}
			return []*config.SetConfig{byName, addr}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr := proxySetFor("address", nil, []string{"127.0.0.1/32"})
			addr.Targets.IPPriority = true
			byName := proxySetFor("name", []string{"api.telegram.org"}, nil)
			sets := []*config.SetConfig{addr, byName}
			if tc.setup != nil {
				sets = tc.setup(addr, byName)
			}
			host, dst, dport, applies := tc.host, tc.dst, tc.dport, tc.applies
			if host == "" {
				host = "api.telegram.org"
			}
			if dst == "" {
				dst = "127.0.0.1"
			}
			if dport == 0 {
				dport = 443
			}
			if applies == nil {
				applies = everything
			}
			r := NewResolver(nil)
			r.Set(sni.NewSuffixSet(sets))
			set, byNameStep := r.PassOnSet(host, net.ParseIP(dst), dport, tc.mac, applies, func(*config.SetConfig, bool) bool { return true })
			got := ""
			if set != nil {
				got = set.Id
			}
			if got != tc.want || byNameStep != tc.wantByName {
				t.Fatalf("PassOnSet = %q (by name %v), want %q (by name %v)", got, byNameStep, tc.want, tc.wantByName)
			}
		})
	}
}

func TestPassOnSetWithoutAMatcher(t *testing.T) {
	takes := func(*config.SetConfig, bool) bool { return true }
	if set, byName := NewResolver(nil).PassOnSet("api.telegram.org", net.ParseIP("127.0.0.1"), 443, "", func(*config.SetConfig) bool { return true }, takes); set != nil || byName {
		t.Fatalf("a resolver with no matcher yet chose %v (by name %v)", set, byName)
	}
}

func TestASaveDuringAPassOnDecisionDoesNotMixTheTwoConfigs(t *testing.T) {
	holder := proxySetFor("holder", nil, []string{"127.0.0.1/32"})
	holder.Targets.IPPriority = true
	byName := proxySetFor("by-name", []string{"api.telegram.org"}, nil)
	wider := proxySetFor("wider", nil, []string{"127.0.0.0/8"})
	r := NewResolver(nil)
	r.Set(sni.NewSuffixSet([]*config.SetConfig{holder, byName, wider}))
	saved := sni.NewSuffixSet([]*config.SetConfig{byName, wider})
	applies := func(*config.SetConfig) bool {
		r.Set(saved)
		return true
	}
	takes := func(*config.SetConfig, bool) bool { return true }
	decide := func() (string, bool) {
		set, byNameStep := r.PassOnSet("api.telegram.org", net.ParseIP("127.0.0.1"), 443, "", applies, takes)
		if set == nil {
			return "", byNameStep
		}
		return set.Id, byNameStep
	}

	if id, byNameStep := decide(); id != "holder" || byNameStep {
		t.Fatalf("a decision that began before the save chose %q (by name %v), want the holder its own sets give", id, byNameStep)
	}
	if id, byNameStep := decide(); id != "by-name" || !byNameStep {
		t.Fatalf("a decision after the save chose %q (by name %v), want the set that lists the name", id, byNameStep)
	}
}
