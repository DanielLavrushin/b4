package config

var KnownDoHHosts = map[string]bool{
	"1.1.1.1": true, "8.8.8.8": true, "8.8.4.4": true, "9.9.9.9": true, "1.0.0.1": true,
	"cloudflare-dns.com": true, "mozilla.cloudflare-dns.com": true, "security.cloudflare-dns.com": true,
	"family.cloudflare-dns.com": true, "dns.google": true, "dns.quad9.net": true, "dns10.quad9.net": true,
	"dns11.quad9.net": true, "dns12.quad9.net": true, "dns.adguard.com": true, "dns.adguard-dns.com": true,
	"family.adguard-dns.com": true, "unfiltered.adguard-dns.com": true, "dns.opendns.com": true,
	"doh.opendns.com": true, "dns.nextdns.io": true, "dns.mullvad.net": true, "adblock.dns.mullvad.net": true,
	"dns.sb": true, "doh.dns.sb": true, "dns.alidns.com": true, "doh.pub": true, "dns.comss.one": true,
	"common.dot.dns.yandex.net": true, "dns.yandex.ru": true, "anycast.uncensoreddns.org": true,
	"unicast.uncensoreddns.org": true, "dns.digitale-gesellschaft.ch": true, "dnsforge.de": true,
	"doh.libredns.gr": true, "doh.dns4all.eu": true, "protective.joindns4.eu": true, "dnspub.restena.lu": true,
	"dns.aquilenet.fr": true, "dns.anon.no": true, "wikimedia-dns.org": true, "xbox-dns.ru": true,
	"v0dka.ru": true, "ibuki.cgnat.net": true, "doh.cleanbrowsing.org": true, "dns.controld.com": true,
	"freedns.controld.com": true, "doh.tiarap.org": true, "doh.tiar.app": true, "dns.switch.ch": true,
	"dns.brahma.world": true, "doh.applied-privacy.net": true, "basic.rethinkdns.com": true,
}
