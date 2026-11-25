package config

import (
	"strings"
	"testing"
)

func TestOnlyThePublishedPathOfAPublicResolverIsNotPersonal(t *testing.T) {
	for raw, personal := range map[string]bool{
		"https://freedns.controld.com/p0":                            false,
		"https://freedns.controld.com/p0/":                           false,
		"https://FreeDNS.ControlD.com/p0":                            false,
		"https://freedns.controld.com:443/p0":                        false,
		"https://doh.cleanbrowsing.org/doh/security-filter":          false,
		"https://doh.cleanbrowsing.org/doh/security-filter/":         false,
		"https://doh.applied-privacy.net/query":                      false,
		"https://doh.applied-privacy.net/query/":                     false,
		"https://dns.google/dns-query":                               false,
		"https://freedns.controld.com/P0":                            true,
		"https://freedns.controld.com/%70%30":                        true,
		"https://freedns.controld.com//p0":                           true,
		"https://freedns.controld.com/p0/canaryid":                   true,
		"https://freedns.controld.com/canaryid":                      true,
		"https://freedns.controld.com/p0?token=canary":               true,
		"https://freedns.controld.com/p0?":                           true,
		"https://freedns.controld.com/p0#canary":                     true,
		"https://canaryuser:canarypass@freedns.controld.com/p0":      true,
		"https://freedns.controld.com:123456789/p0":                  true,
		"https://freedns.controld.com.example/p0":                    true,
		"https://dns.controld.com/p0":                                true,
		"https://dns.controld.com/canaryid":                          true,
		"https://doh.cleanbrowsing.org/doh/canaryid":                 true,
		"https://doh.cleanbrowsing.org/doh/security-filter/canaryid": true,
		"https://doh.applied-privacy.net/query/canaryid":             true,
		"https://doh.applied-privacy.net/query?token=canary":         true,
		"https://doh.applied-privacy.net/canaryid":                   true,
		"https://dns.google/query":                                   true,
		"https://dns.nextdns.io/canaryid":                            true,
		"https://dns.adguard-dns.com/dns-query/canaryid":             true,
	} {
		if got := PersonalResolverURL(raw); got != personal {
			t.Errorf("PersonalResolverURL(%q) = %v, want %v", raw, got, personal)
		}
	}
}

func TestAPublishedPathBelongsToAKnownResolver(t *testing.T) {
	for entry := range publicDoHPaths {
		host, path, found := strings.Cut(entry, "/")
		if !found || path == "" || !KnownDoHHosts[host] {
			t.Errorf("%s: whoever imports a set drops the redirect of a host that is not known", entry)
		}
	}
}
