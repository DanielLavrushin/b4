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
		if got := countWarnings(imp.Warnings, "dns_no_resolver"); got != 1 {
			t.Errorf("%s: the importer must learn once that the redirect has no resolver: %+v", doh, imp.Warnings)
		}
		live, err := LiveFingerprint(&set, nil)
		if err != nil || live != env.Fingerprint {
			t.Errorf("%s: the shared set reads as edited on its own router: %q against %q, %v", doh, live, env.Fingerprint, err)
		}
	}
}

func countWarnings(warnings []Warning, code string) int {
	n := 0
	for _, w := range warnings {
		if w.Code == code {
			n++
		}
	}
	return n
}

func TestAnImportedRedirectWithoutAResolverSaysSo(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*config.DNSConfig)
		want   int
	}{
		"plain IP":               {func(d *config.DNSConfig) { d.DoHURL, d.TargetDNS = "", "9.9.9.9" }, 1},
		"backups only":           {func(d *config.DNSConfig) { d.DoHURL, d.Upstreams = "", []string{"https://dns.google/dns-query"} }, 1},
		"no resolver at all":     {func(d *config.DNSConfig) { d.DoHURL = "" }, 1},
		"pins only":              {func(d *config.DNSConfig) { d.DoHURL, d.Enabled = "", false }, 0},
		"personal":               {func(d *config.DNSConfig) { d.DoHURL = "https://dns.nextdns.io/canaryid" }, 1},
		"public":                 {func(d *config.DNSConfig) { d.DoHURL = "https://dns.google/dns-query" }, 0},
		"public with a backup":   {func(d *config.DNSConfig) { d.Upstreams = []string{"9.9.9.9"} }, 0},
		"redirect off":           {func(d *config.DNSConfig) { d.Enabled = false }, 0},
		"unknown, drops it all":  {func(d *config.DNSConfig) { d.DoHURL = "https://unknown.example/dns-query" }, 0},
		"unknown, personal path": {func(d *config.DNSConfig) { d.DoHURL = "https://unknown.example/canaryid" }, 1},
	} {
		set := resolverSet("https://dns.google/dns-query")
		tc.change(&set.DNS)
		env, _, err := Build(&set, BuildOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		imp, err := Open(env, OpenOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := countWarnings(imp.Warnings, "dns_no_resolver"); got != tc.want {
			t.Errorf("%s: warned %d times, want %d: %+v", name, got, tc.want, imp.Warnings)
		}
		if says := imp.Set.DNS.Enabled && !imp.Set.DNS.HasUpstream(); says != (tc.want == 1) {
			t.Errorf("%s: the imported set itself says otherwise: %+v", name, imp.Set.DNS)
		}
	}
}

func TestAResolverOnlyInForeignFieldsDoesNotCount(t *testing.T) {
	set := map[string]interface{}{
		"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}},
		"dns":     map[string]interface{}{"enabled": true, "target_dns": "9.9.9.9", "upstreams": []interface{}{"9.9.9.9"}},
	}
	imp, err := Open(&Envelope{Format: Format, Title: "hand made", Set: set}, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Set.DNS.HasUpstream() || countWarnings(imp.Warnings, "dns_no_resolver") != 1 {
		t.Errorf("the import drops these fields, so the redirect has no resolver: %+v %+v", imp.Set.DNS, imp.Warnings)
	}
}

func TestAPublicResolverIsShared(t *testing.T) {
	for doh, known := range map[string]bool{
		"https://dns.google/dns-query":                      true,
		"https://dns.google/dns-query/":                     true,
		"https://cloudflare-dns.com":                        true,
		"https://1.1.1.1/":                                  true,
		"https://dns.google:8443/dns-query":                 true,
		"https://dns.alidns.com:443/dns-query":              true,
		"https://freedns.controld.com/p0":                   true,
		"https://doh.cleanbrowsing.org/doh/security-filter": true,
		"https://doh.applied-privacy.net/query":             true,
		"https://unknown.example/dns-query":                 false,
		"https://id.unknown.example/":                       false,
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
		for _, code := range []string{"fingerprint_mismatch", "dns_no_resolver"} {
			if _, found := warned(imp.Warnings, code); found {
				t.Errorf("%s: %s says nothing that is new or true: %+v", doh, code, imp.Warnings)
			}
		}
		if raw := resolverJSON(t, imp); strings.Contains(raw, "canary") {
			t.Errorf("%s: the imported set holds a private part: %s", doh, raw)
		}
	}
}
