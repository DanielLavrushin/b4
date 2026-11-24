package hubwire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func resolverSet(doh string) config.SetConfig {
	set := redirectingSet()
	set.DNS.DoHURL = doh
	set.DNS.Strict = true
	set.DNS.Pins = map[string][]string{"youtube.com": {"93.184.216.34"}}
	return set
}

func strippedReason(r *Report, path string) string {
	for _, s := range r.Stripped {
		if s.Path == path {
			return s.Reason
		}
	}
	return ""
}

func warned(warnings []Warning, code string) (Warning, bool) {
	for _, w := range warnings {
		if w.Code == code {
			return w, true
		}
	}
	return Warning{}, false
}

func resolverJSON(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var personalResolvers = []string{
	"https://dns.nextdns.io/canaryid",
	"https://dns.adguard-dns.com/dns-query/canaryid",
	"https://dns.nextdns.io/dns-query?token=canaryquery",
	"https://dns.nextdns.io/dns-query?",
	"https://dns.nextdns.io/dns-query#canaryfragment",
	"https://canaryuser:canarypass@dns.nextdns.io/dns-query",
	"https://canaryuser@dns.nextdns.io/",
	"https://canaryuser:123456789/dns-query",
	"https://canaryuser:canary^pass@dns.nextdns.io/dns-query",
	"https://unknown.example/canaryid",
}

func TestAPersonalResolverIsLeftOutOfASharedSet(t *testing.T) {
	for _, doh := range personalResolvers {
		set := resolverSet(doh)
		env, report, err := Build(&set, BuildOptions{})
		if err != nil {
			t.Fatalf("%s: %v", doh, err)
		}
		raw := resolverJSON(t, env)
		if strings.Contains(raw, "canary") {
			t.Errorf("%s: the shared set holds a private part: %s", doh, raw)
		}
		if _, found := lookupPath(env.Set, "dns.doh_url"); found {
			t.Errorf("%s: the shared set still names a resolver: %s", doh, raw)
		}
		if got := strippedReason(report, "dns.doh_url"); got != ReasonPersonalResolver {
			t.Errorf("%s: the report must say why the resolver is missing, got %q in %+v", doh, got, report)
		}
		if _, found := warned(report.Warnings, "doh_unknown"); found {
			t.Errorf("%s: a resolver that is left out needs no warning about its host: %+v", doh, report.Warnings)
		}
		if pins, _ := lookupPath(env.Set, "dns.pins"); pins == nil {
			t.Errorf("%s: the rest of the DNS settings must stay: %s", doh, raw)
		}

		imp, err := Open(env, OpenOptions{})
		if err != nil {
			t.Fatalf("%s: %v", doh, err)
		}
		if imp.Fingerprint != env.Fingerprint {
			t.Errorf("%s: the importer sees another strategy than was shared", doh)
		}
		for _, code := range []string{ReasonPersonalResolver, "fingerprint_mismatch"} {
			if _, found := warned(imp.Warnings, code); found {
				t.Errorf("%s: nothing is dropped on import any more: %+v", doh, imp.Warnings)
			}
		}
		live, err := LiveFingerprint(&set, nil)
		if err != nil || live != env.Fingerprint {
			t.Errorf("%s: the shared set reads as edited on its own router: %q against %q, %v", doh, live, env.Fingerprint, err)
		}
	}
}

func TestAPublicResolverIsShared(t *testing.T) {
	for doh, known := range map[string]bool{
		"https://dns.google/dns-query":         true,
		"https://dns.google/dns-query/":        true,
		"https://cloudflare-dns.com":           true,
		"https://1.1.1.1/":                     true,
		"https://dns.google:8443/dns-query":    true,
		"https://dns.alidns.com:443/dns-query": true,
		"https://unknown.example/dns-query":    false,
		"https://id.unknown.example/":          false,
	} {
		set := resolverSet(doh)
		projection, report, err := Scrub(&set)
		if err != nil {
			t.Fatalf("%s: %v", doh, err)
		}
		if got, _ := lookupPath(projection, "dns.doh_url"); got != doh {
			t.Errorf("%s: the shared set names %v", doh, got)
		}
		if got := strippedReason(report, "dns.doh_url"); got != "" {
			t.Errorf("%s: reported as left out (%q)", doh, got)
		}
		if _, found := warned(report.Warnings, "doh_unknown"); found == known {
			t.Errorf("%s: doh_unknown warned %v, want %v", doh, found, !known)
		}
	}
}

func TestAPersonalResolverOfARedirectThatIsOffIsNotReported(t *testing.T) {
	set := resolverSet("https://dns.nextdns.io/canaryid")
	set.DNS.Enabled = false
	projection, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	if raw := resolverJSON(t, projection); strings.Contains(raw, "canary") {
		t.Errorf("the shared set holds a private part: %s", raw)
	}
	if got := strippedReason(report, "dns.doh_url"); got != "" {
		t.Errorf("a redirect that is off is not shared at all, reported %q", got)
	}
}

func TestAnOlderSharedSetLosesAPersonalResolverOnImport(t *testing.T) {
	for _, doh := range personalResolvers[:5] {
		set := map[string]interface{}{
			"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}},
			"dns": map[string]interface{}{
				"enabled": true,
				"doh_url": doh,
				"pins":    map[string]interface{}{"example.com": []interface{}{"93.184.216.34"}},
			},
		}
		env := &Envelope{Format: Format, Title: "older", Set: set, Fingerprint: Fingerprint(set)}
		imp, err := Open(env, OpenOptions{})
		if err != nil {
			t.Fatalf("%s: %v", doh, err)
		}
		if imp.Set.DNS.DoHURL != "" || !imp.Set.DNS.Enabled || len(imp.Set.DNS.Pins) != 1 {
			t.Errorf("%s: only the resolver goes, got %+v", doh, imp.Set.DNS)
		}
		host, _ := DoHHost(doh)
		warning, found := warned(imp.Warnings, ReasonPersonalResolver)
		if !found || warning.Params["path"] != "dns.doh_url" || warning.Params["host"] != host || host == "" {
			t.Errorf("%s: the importer must learn which resolver went: %+v", doh, imp.Warnings)
		}
		if _, mismatch := warned(imp.Warnings, "fingerprint_mismatch"); mismatch {
			t.Errorf("%s: nobody edited the set: %+v", doh, imp.Warnings)
		}
		if raw := resolverJSON(t, imp); strings.Contains(raw, "canary") {
			t.Errorf("%s: the imported set holds a private part: %s", doh, raw)
		}
	}
}
