package nfq

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

func testECHConfigList(publicName string) []byte {
	var c []byte
	c = append(c, 0x42)
	c = binary.BigEndian.AppendUint16(c, 0x0020)
	c = binary.BigEndian.AppendUint16(c, 32)
	c = append(c, bytes.Repeat([]byte{0xab}, 32)...)
	c = binary.BigEndian.AppendUint16(c, 4)
	c = binary.BigEndian.AppendUint16(c, 0x0001)
	c = binary.BigEndian.AppendUint16(c, 0x0001)
	c = append(c, 0, byte(len(publicName)))
	c = append(c, publicName...)
	c = binary.BigEndian.AppendUint16(c, 0)
	list := binary.BigEndian.AppendUint16(nil, 0xfe0d)
	list = binary.BigEndian.AppendUint16(list, uint16(len(c)))
	list = append(list, c...)
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(list))), list...)
}

func httpsAnswerWithECH(qname string, qtype uint16, covers ...string) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x4242)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], uint16(len(covers)))
	msg = append(msg, encodeTestName(qname)...)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, 1)

	for _, cover := range covers {
		rdata := []byte{0, 1, 0}
		rdata = append(rdata, 0, 1, 0, 3, 2, 'h', '2')
		list := testECHConfigList(cover)
		rdata = binary.BigEndian.AppendUint16(rdata, 5)
		rdata = binary.BigEndian.AppendUint16(rdata, uint16(len(list)))
		rdata = append(rdata, list...)

		msg = append(msg, 0xC0, 0x0C)
		msg = binary.BigEndian.AppendUint16(msg, qtype)
		msg = binary.BigEndian.AppendUint16(msg, 1)
		msg = binary.BigEndian.AppendUint32(msg, 300)
		msg = binary.BigEndian.AppendUint16(msg, uint16(len(rdata)))
		msg = append(msg, rdata...)
	}
	return msg
}

func echTestSet(id string, strip bool, domains ...string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.Enabled = true
	set.Targets.SNIDomains = domains
	set.Targets.DomainsToMatch = domains
	set.DNS.StripECH = strip
	return &set
}

func echTestWorker(t *testing.T, sets ...*config.SetConfig) (*Worker, *config.Config) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Sets = sets
	return newTestWorker(t, &cfg), &cfg
}

func TestFilterDNSAnswerStripsECHForTheQueriedSet(t *testing.T) {
	site := echTestSet("steamdb", true, "steamdb.info")
	w, cfg := echTestWorker(t, site)
	resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")

	out, action, actor := w.filterDNSAnswer(cfg, site, "steamdb.info", "", resp, false)
	if out == nil || action != dnsActionECHStripped || actor != site {
		t.Fatalf("action %q by %v, want %q by the site's set", action, actor, dnsActionECHStripped)
	}
	if dns.ParseResponseECH(out) != nil {
		t.Fatal("the answer still carries the ECH key")
	}
}

func TestFilterDNSAnswerStripsECHByTheKeyCoverName(t *testing.T) {
	cover := echTestSet("cloudflare-ech", true, "cloudflare-ech.com")
	other := echTestSet("other", false, "steamdb.info")
	w, cfg := echTestWorker(t, other, cover)

	for name, querySet := range map[string]*config.SetConfig{"no set": nil, "set without the switch": other} {
		resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")
		out, action, actor := w.filterDNSAnswer(cfg, querySet, "steamdb.info", "", resp, false)
		if out == nil || action != dnsActionECHStripped || actor != cover {
			t.Fatalf("%s: action %q by %v, want the cover set to strip it", name, action, actor)
		}
	}

	resp := httpsAnswerWithECH("example.org", dnsTypeHTTPS, "cover.example.net")
	if out, _, _ := w.filterDNSAnswer(cfg, nil, "example.org", "", resp, false); out != nil {
		t.Fatal("stripped a key whose cover name no set lists")
	}
}

func TestFilterDNSAnswerKeepsECHWhenNoSetAsks(t *testing.T) {
	plain := echTestSet("cloudflare-ech", false, "cloudflare-ech.com")
	off := echTestSet("off", true, "steamdb.info")
	off.Enabled = false
	w, cfg := echTestWorker(t, plain, off)

	resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")
	if out, _, _ := w.filterDNSAnswer(cfg, off, "steamdb.info", "", resp, false); out != nil {
		t.Fatal("a disabled set stripped the key")
	}
	if out, _, _ := w.filterDNSAnswer(cfg, plain, "cloudflare-ech.com", "", resp, false); out != nil {
		t.Fatal("stripped although no enabled set has the switch on")
	}
}

func TestFilterDNSAnswerECHStripHonoursSourceDevices(t *testing.T) {
	const phone = "aa:bb:cc:dd:ee:01"
	cover := echTestSet("cloudflare-ech", true, "cloudflare-ech.com")
	cover.Targets.SourceDevices = []string{phone}
	w, cfg := echTestWorker(t, cover)

	resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")
	if out, _, _ := w.filterDNSAnswer(cfg, nil, "steamdb.info", "aa:bb:cc:dd:ee:02", resp, false); out != nil {
		t.Fatal("stripped the answer for a device the set does not cover")
	}
	if out, _, actor := w.filterDNSAnswer(cfg, nil, "steamdb.info", phone, resp, false); out == nil || actor != cover {
		t.Fatal("kept the key for the device the set covers")
	}
}

func TestFilterDNSAnswerECHStripScope(t *testing.T) {
	site := echTestSet("steamdb", true, "steamdb.info")
	w, cfg := echTestWorker(t, site)

	svcb := httpsAnswerWithECH("steamdb.info", dnsTypeSVCB, "cloudflare-ech.com")
	if out, action, _ := w.filterDNSAnswer(cfg, site, "steamdb.info", "", svcb, true); out == nil || action != dnsActionECHStripped {
		t.Fatal("an SVCB answer kept its key")
	}

	discovery := *cfg
	discovery.Queue.IsDiscovery = true
	resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")
	if out, _, _ := w.filterDNSAnswer(&discovery, site, "steamdb.info", "", resp, false); out != nil {
		t.Fatal("the Discovery queue stripped a key Discovery itself asked for")
	}
}

func TestECHStripWanted(t *testing.T) {
	site := echTestSet("steamdb", true, "steamdb.info")
	plain := echTestSet("plain", false, "example.org")

	withStrip := config.NewConfig()
	withStrip.Sets = []*config.SetConfig{plain, site}
	without := config.NewConfig()
	without.Sets = []*config.SetConfig{plain}

	cases := []struct {
		name  string
		cfg   *config.Config
		set   *config.SetConfig
		qtype uint16
		want  bool
	}{
		{"https query, a set strips", &withStrip, nil, dnsTypeHTTPS, true},
		{"svcb query, its own set strips", &withStrip, site, dnsTypeSVCB, true},
		{"address query", &withStrip, site, dnsTypeA, false},
		{"no set strips", &without, plain, dnsTypeHTTPS, false},
		{"no config", nil, site, dnsTypeHTTPS, false},
	}
	for _, tc := range cases {
		if got := echStripWanted(tc.cfg, tc.set, tc.qtype); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFilterDNSAnswerStripsECHWhenAnyKeyNamesACover(t *testing.T) {
	cover := echTestSet("other-cdn", true, "ech.other-cdn.example")
	w, cfg := echTestWorker(t, cover)

	resp := httpsAnswerWithECH("multi.example", dnsTypeHTTPS, "cloudflare-ech.com", "ech.other-cdn.example")
	out, action, actor := w.filterDNSAnswer(cfg, nil, "multi.example", "", resp, false)
	if out == nil || action != dnsActionECHStripped || actor != cover {
		t.Fatalf("action %q by %v: a client may pick the second record, whose key names the set's cover", action, actor)
	}
	if names := dns.ResponseECHPublicNames(out); len(names) != 0 {
		t.Fatalf("keys left in the answer: %v", names)
	}
}
