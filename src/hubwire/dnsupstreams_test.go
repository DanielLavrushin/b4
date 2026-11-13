package hubwire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func redirectingSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Name = "YouTube"
	set.Targets.SNIDomains = []string{"youtube.com"}
	set.DNS.Enabled = true
	set.DNS.DoHURL = "https://dns.google/dns-query"
	return set
}

func TestTheResolverListIsNeverShared(t *testing.T) {
	plain := redirectingSet()
	base, _, err := Scrub(&plain)
	if err != nil {
		t.Fatal(err)
	}

	listed := redirectingSet()
	listed.DNS.Upstreams = []string{"192.168.1.53", "https://abc123.dns.nextdns.io/dns-query"}
	projection, report, err := Scrub(&listed)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := lookupPath(projection, "dns.upstreams"); found {
		t.Error("the projection must not carry dns.upstreams")
	}
	raw, _ := json.Marshal(projection)
	for _, private := range []string{"upstreams", "192.168.1.53", "abc123"} {
		if strings.Contains(string(raw), private) {
			t.Errorf("the projection carries %q: %s", private, raw)
		}
	}
	reason := ""
	for _, s := range report.Stripped {
		if s.Path == "dns.upstreams" {
			reason = s.Reason
		}
	}
	if reason != "private" {
		t.Errorf("dns.upstreams must be reported as stripped for being private, got %q in %+v", reason, report.Stripped)
	}
	if Fingerprint(projection) != Fingerprint(base) {
		t.Error("a resolver list changed the strategy fingerprint")
	}
	withoutList, errPlain := LiveFingerprint(&plain, nil)
	withList, errListed := LiveFingerprint(&listed, nil)
	if errPlain != nil || errListed != nil || withoutList != withList {
		t.Errorf("live fingerprints differ: %q %v against %q %v", withoutList, errPlain, withList, errListed)
	}
}

func TestTheResolverListIsDroppedOnImport(t *testing.T) {
	env := &Envelope{
		Format: Format,
		Title:  "crafted",
		Set: map[string]interface{}{
			"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}},
			"dns": map[string]interface{}{
				"enabled":   true,
				"doh_url":   "https://dns.google/dns-query",
				"upstreams": []interface{}{"203.0.113.9"},
			},
		},
	}
	imp, err := Open(env, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Set.DNS.Upstreams) != 0 {
		t.Errorf("an imported set must not carry a resolver list, got %q", imp.Set.DNS.Upstreams)
	}
	if imp.Set.DNS.DoHURL != "https://dns.google/dns-query" {
		t.Errorf("the known DoH URL must still cross, got %q", imp.Set.DNS.DoHURL)
	}
	for _, w := range imp.Warnings {
		if w.Code == "unknown_fields" {
			t.Errorf("dns.upstreams is a known private field, not an unknown one: %+v", w)
		}
	}
}
