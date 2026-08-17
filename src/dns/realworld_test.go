package dns

import (
	"encoding/binary"
	"strings"
	"testing"
)

var realWorldNames = []string{
	"0.pool.ntp.org",
	"1-2-3.test-host.example.org",
	"1.pool.ntp.org",
	"192.168.1.1",
	"UPPER.Example.COM",
	"_dmarc.example.com",
	"a.co",
	"a961.b.akamai.net",
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.ccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc.ddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.example.com",
	"ae.iads.unity3d.com",
	"afs.ampaeservices.com",
	"analytics-ios.rayjump.com",
	"api.miwifi.com",
	"api16-core-ycru.tiktokv.com",
	"assets.msn.com",
	"beacons2.gvt2.com",
	"cdn-v6.amp-endpoint3.com",
	"cdn.activision.com",
	"cdn.iads.unity3d.com",
	"clck.yandex.net",
	"d.applovin.com",
	"graph.whatsapp.com",
	"ipapi.co",
	"kws2-1.web.telegram.org",
	"kws2.web.telegram.org",
	"kws203.onedaychamp.co.uk",
	"localhost.localdomain",
	"max.ru",
	"mssdk-ru.tiktokv.com",
	"p1.trex.media",
	"pool.ntp.org",
	"prefetch.monetization-sdk.chartboost.com",
	"profile.gc.apple.com",
	"pubads.g.doubleclick.net",
	"quasar.yandex.net",
	"sdkeventfnt-eu.dsp-api.moloco.com",
	"ssl.gstatic.com",
	"stun.cloudflare.com",
	"sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.sub.example.com",
	"test-gateway.instagram.com",
	"unagi-na.amazon.com",
	"wps.apple.com",
	"www.baidu.com",
	"www.googleapis.com",
	"x.io",
	"xn--80ak6aa92e.com",
	"xp.apple.com",
}

func TestParseQueryDomainRealWorldNames(t *testing.T) {
	qtypes := []uint16{1, 28, 65, 5, 16, 12, 33, 257}

	for _, name := range realWorldNames {
		for _, qt := range qtypes {
			query := buildDNSQuery(0x4242, name, qt)
			if got, ok := ParseQueryDomain(query); !ok || got != name {
				t.Errorf("query %q qtype=%d: got (%q,%v), want (%q,true)", name, qt, got, ok, name)
			}

			block := BuildBlockResponse(query)
			if block == nil {
				t.Errorf("BuildBlockResponse(%q) = nil", name)
				continue
			}
			if got, ok := ParseQueryDomain(block); !ok || got != name {
				t.Errorf("nxdomain response for %q: got (%q,%v)", name, got, ok)
			}

			servfail := BuildServfailResponse(query)
			if servfail == nil {
				t.Errorf("BuildServfailResponse(%q) = nil", name)
				continue
			}
			if got, ok := ParseQueryDomain(servfail); !ok || got != name {
				t.Errorf("servfail response for %q: got (%q,%v)", name, got, ok)
			}
		}

		built := BuildQuery(name, 0x1234, 1)
		want := strings.TrimSuffix(strings.TrimSpace(name), ".")
		if got, ok := ParseQueryDomain(built); !ok || got != want {
			t.Errorf("BuildQuery round-trip for %q: got (%q,%v), want %q", name, got, ok, want)
		}
	}
}

func TestParseQueryDomainToleratesExtraSections(t *testing.T) {
	name := "cdn.activision.com"
	query := buildDNSQuery(0x9999, name, 1)

	withOPT := append([]byte(nil), query...)
	withOPT = append(withOPT, 0x00, 0x00, 0x29, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	binary.BigEndian.PutUint16(withOPT[10:12], 1)
	if got, ok := ParseQueryDomain(withOPT); !ok || got != name {
		t.Errorf("EDNS0 OPT query: got (%q,%v), want (%q,true)", got, ok, name)
	}

	twoQuestions := append([]byte(nil), query...)
	twoQuestions = append(twoQuestions, encodeDNSName("second.example.com")...)
	twoQuestions = append(twoQuestions, 0x00, 0x01, 0x00, 0x01)
	binary.BigEndian.PutUint16(twoQuestions[4:6], 2)
	if got, ok := ParseQueryDomain(twoQuestions); !ok || got != name {
		t.Errorf("QDCOUNT=2 query: got (%q,%v), want (%q,true)", got, ok, name)
	}
}
