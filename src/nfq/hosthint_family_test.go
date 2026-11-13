package nfq

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

const (
	familyDevice   = "10.0.0.1"
	familyResolver = "8.8.8.8"
	familyName     = "i.ytimg.com"
	familyV4Addr   = "1.2.3.4"
	familyV6Addr   = "2001:db8::2"
)

func familySet(id, version string) *config.SetConfig {
	set := newHintSet()
	set.Id, set.Name = id, "YT "+id
	set.Targets.IPVersion = version
	return &set
}

func familyWorker(t *testing.T, sets ...*config.SetConfig) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = true
	cfg.Sets = sets
	return newTestWorker(t, &cfg)
}

func familyAnswer(w *Worker, qtype uint16, addrs ...string) {
	answer := buildTestMixedResponse(familyName, qtype, addrs...)
	w.ProcessPacket(makeV4UDPPacket(answer, net.ParseIP(familyResolver), net.ParseIP(familyDevice), 53, 5353))
}

func familyHint(w *Worker, addr string) string {
	id, _, _ := w.hostHints.Lookup(familyDevice, addr)
	return id
}

func TestADNSAnswerHintsTheSetThatTakesTheAddressFamily(t *testing.T) {
	for _, c := range []struct {
		name          string
		first, second string
		qtype         uint16
		addr, want    string
	}{
		{"an A answer, the IPv6 set listed first", "6", "4", dnsTypeA, familyV4Addr, "v4"},
		{"an A answer, the IPv4 set listed first", "4", "6", dnsTypeA, familyV4Addr, "v4"},
		{"an AAAA answer, the IPv4 set listed first", "4", "6", dnsTypeAAAA, familyV6Addr, "v6"},
		{"an AAAA answer, the IPv6 set listed first", "6", "4", dnsTypeAAAA, familyV6Addr, "v6"},
		{"an A answer, a set for any family listed second", "6", "", dnsTypeA, familyV4Addr, "v"},
		{"an AAAA answer, a set for any family listed first", "", "6", dnsTypeAAAA, familyV6Addr, "v"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := familyWorker(t, familySet("v"+c.first, c.first), familySet("v"+c.second, c.second))
			familyAnswer(w, c.qtype, c.addr)
			if got := familyHint(w, c.addr); got != c.want {
				t.Fatalf("the hint for %s names set %q, want %q", c.addr, got, c.want)
			}
		})
	}
}

func TestAnAnswerWithBothFamiliesHintsEachAddressItsOwnSet(t *testing.T) {
	w := familyWorker(t, familySet("v6", "6"), familySet("v4", "4"))
	familyAnswer(w, dnsTypeA, familyV4Addr, familyV6Addr)
	if got := familyHint(w, familyV4Addr); got != "v4" {
		t.Errorf("the hint for %s names set %q, want v4", familyV4Addr, got)
	}
	if got := familyHint(w, familyV6Addr); got != "v6" {
		t.Errorf("the hint for %s names set %q, want v6", familyV6Addr, got)
	}
}

func TestASetOfTheOtherFamilyStillHintsWhenNoSetTakesTheAddress(t *testing.T) {
	w := familyWorker(t, familySet("v6", "6"))
	familyAnswer(w, dnsTypeA, familyV4Addr)
	if got := familyHint(w, familyV4Addr); got != "v6" {
		t.Fatalf("the hint for %s names set %q, want v6: the IP version filter is a preference, and the ClientHello falls back to this set too", familyV4Addr, got)
	}
}

func TestTheFirstSynToAnAnsweredAddressLeavesWithTheWindowOfTheSetForItsFamily(t *testing.T) {
	v4 := familySet("v4", "4")
	v4.TCP.RSTProtection.Enabled = false
	v4.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1, Cutoff: wsFirstData}
	v6 := familySet("v6", "6")
	v6.TCP.RSTProtection.Enabled = false
	w := familyWorker(t, v6, v4)
	familyAnswer(w, dnsTypeA, familyV4Addr)

	syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
	got := wsDispatch(t, w, &wsQueue{}, 1, syn)
	if got.packet == nil {
		t.Fatalf("the SYN to %s left unchanged, want the window of set v4: the hint named set %q", familyV4Addr, familyHint(w, familyV4Addr))
	}
	if win := wsWindow(got.packet); win != 1 {
		t.Fatalf("the SYN left with window %d, want 1", win)
	}
}

func TestAHintGoesToTheSetOfTheFamilyThatTakesTheDevice(t *testing.T) {
	const phone, laptop = "aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"
	for _, c := range []struct{ name, mac, want string }{
		{"the device both limited sets take", phone, "phone4"},
		{"another device", laptop, "any"},
	} {
		t.Run(c.name, func(t *testing.T) {
			phone6, phone4 := familySet("phone6", "6"), familySet("phone4", "4")
			phone6.Targets.SourceDevices = []string{phone}
			phone4.Targets.SourceDevices = []string{phone}
			w := familyWorker(t, phone6, familySet("any", ""), phone4)
			w.ipToMac.Store(map[string]string{familyDevice: c.mac})
			familyAnswer(w, dnsTypeA, familyV4Addr)
			if got := familyHint(w, familyV4Addr); got != c.want {
				t.Fatalf("the hint for %s names set %q, want %q", familyV4Addr, got, c.want)
			}
		})
	}
}

func TestStoredHintsFollowTheAddressFamilyWhateverSetTheAnswerPathNamed(t *testing.T) {
	v6, v4 := familySet("v6", "6"), familySet("v4", "4")
	w := familyWorker(t, v6, v4)
	w.storeHostHints(net.ParseIP(familyDevice), v6, familyName, []net.IP{net.ParseIP(familyV4Addr), net.ParseIP(familyV6Addr)})
	if got := familyHint(w, familyV4Addr); got != "v4" {
		t.Errorf("the hint for %s names set %q, want v4", familyV4Addr, got)
	}
	if got := familyHint(w, familyV6Addr); got != "v6" {
		t.Errorf("the hint for %s names set %q, want v6", familyV6Addr, got)
	}
}

func TestAPinnedAnswerHintsTheSetThatTakesTheAddressFamily(t *testing.T) {
	v6, v4 := familySet("v6", "6"), familySet("v4", "4")
	v6.DNS.Pins = map[string][]string{familyName: {familyV4Addr}}
	w := familyWorker(t, v6, v4)
	query := dns.BuildQuery(familyName, 7, dnsTypeA)
	pinned := w.pinnedAnswer(v6, query, familyName)
	if pinned == nil {
		t.Fatal("the pin did not answer the A query")
	}
	w.applyPinnedAnswer(w.getConfig(), v6, net.ParseIP(familyDevice), familyName, pinned)
	if got := familyHint(w, familyV4Addr); got != "v4" {
		t.Fatalf("the hint for the pinned %s names set %q, want v4", familyV4Addr, got)
	}
}

func TestAHintKeepsTheSetOfTheAnswerWhenNoOtherSetListsTheName(t *testing.T) {
	backup := familySet("backup", "6")
	backup.Targets.DomainsToMatch = []string{"unrelated.example"}
	w := familyWorker(t, familySet("v4", "4"), backup)
	w.storeHostHints(net.ParseIP(familyDevice), backup, "www.elsewhere.example", []net.IP{net.ParseIP(familyV4Addr)})
	if got := familyHint(w, familyV4Addr); got != "backup" {
		t.Fatalf("the hint names set %q, want backup: no set lists the name, so the set that answered keeps it", got)
	}
}

func TestADNSHintStillRecordsNoVersions(t *testing.T) {
	w := familyWorker(t, familySet("v6", "6"), familySet("v4", "4"))
	familyAnswer(w, dnsTypeA, familyV4Addr)
	hint, ok := w.hostHints.LookupTLS(familyDevice, familyV4Addr)
	if !ok || hint.tlsVersion != 0 || hint.ipVersion != 0 {
		t.Fatalf("hint %+v, found %v: a DNS hint records no versions, so what the connection writes into kernel sets stays guarded like its answer", hint, ok)
	}
}

func TestADNSAnswerAgreesWithTheHintOfAQUICInitial(t *testing.T) {
	v6, v4 := familySet("v6", "6"), familySet("v4", "4")
	w := familyWorker(t, v6, v4)
	familyAnswer(w, dnsTypeA, familyV4Addr)
	w.storeHostHint(familyDevice, familyV4Addr, v4, familyName, "quic", 0x0304, IPv4)
	if got := familyHint(w, familyV4Addr); got != "v4" {
		t.Fatalf("the hint for %s names set %q, want v4: two hints that name different sets cancel each other", familyV4Addr, got)
	}
}

func TestAHintGoesToAScopedBlockSetOnlyFromItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	for _, c := range []struct {
		client learnClient
		want   string
	}{
		{learnClient{name: "a client on br0", ip: scopeLAN, in: idx["br0"]}, "proxy"},
		{learnClient{name: "a client on br-guest", ip: scopeGuest, in: idx["br-guest"]}, "block"},
	} {
		t.Run(c.client.name, func(t *testing.T) {
			thisHost(t, scopeRouter)
			routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
			r := newLearnRig(t, c.client)
			earlier := dscpHookSet("earlier", "example.com", false, false)
			earlier.Targets.IPVersion = "6"
			r.set("block").Targets.IPVersion = "4"
			cfg := r.w.getConfig()
			cfg.Sets = []*config.SetConfig{earlier, r.set("block"), r.set("proxy"), r.set("spare")}
			r.w.matcher.Store(buildMatcher(cfg))
			r.resolvesTo("www.example.com", 0x9d1, 40850)
			if got, _, _ := r.w.hostHints.Lookup(c.client.ip, quicServer.String()); got != c.want {
				t.Fatalf("the hint for %s names set %q, want %q", quicServer, got, c.want)
			}
		})
	}
}

func TestTheBanRetestHintsTheSetOfTheAddressFamily(t *testing.T) {
	w := familyWorker(t, familySet("v6", "6"), familySet("v4", "4"))
	prev := routeSource
	routeSource = func(net.IP, int) net.IP { return net.ParseIP(familyDevice) }
	t.Cleanup(func() { routeSource = prev })

	w.hintBanRetest(net.ParseIP(familyV4Addr), 443, familyName)
	if got := familyHint(w, familyV4Addr); got != "v4" {
		t.Errorf("the re-test of %s is hinted set %q, want v4, the set a device gets for that address", familyV4Addr, got)
	}
	w.hintBanRetest(net.ParseIP(familyV6Addr), 443, familyName)
	if got := familyHint(w, familyV6Addr); got != "v6" {
		t.Errorf("the re-test of %s is hinted set %q, want v6, the set a device gets for that address", familyV6Addr, got)
	}
}
