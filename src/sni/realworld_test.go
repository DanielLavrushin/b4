package sni

import (
	"testing"
)

var realWorldSNIs = []string{
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

func tlsRecordWithSNI(host string) []byte {
	ch := qBuildClientHello([]byte(host))
	return append([]byte{0x16, 0x03, 0x01, byte(len(ch) >> 8), byte(len(ch))}, ch...)
}

func TestSNIExtractionRealWorldNames(t *testing.T) {
	for i, host := range realWorldSNIs {
		dcid := []byte{0xD0, byte(i >> 8), byte(i), 0x11, 0x22, 0x33, 0x44, 0x55}
		if got, ok := ParseQUICClientHelloSNI(qBuildInitial(t, dcid, qBuildClientHello([]byte(host)))); !ok || got != host {
			t.Errorf("QUIC SNI %q (%d bytes): got (%q,%v)", host, len(host), got, ok)
		}

		if got, _, ok := ParseTLSClientHelloSNI(tlsRecordWithSNI(host)); !ok || got != host {
			t.Errorf("TLS SNI %q (%d bytes): got (%q,%v)", host, len(host), got, ok)
		}

		ch := qBuildClientHello([]byte(host))
		if got, ok := ParseTLSClientHelloBodySNI(ch[4:]); !ok || got != host {
			t.Errorf("TLS body SNI %q: got (%q,%v)", host, got, ok)
		}

		if !IsValidSNI([]byte(host)) {
			t.Errorf("IsValidSNI rejected real hostname %q (%d bytes)", host, len(host))
		}
	}
}
