package mtproto

import (
	"testing"
)

func TestDecodeCFDomain_MatchesPythonReference(t *testing.T) {
	cases := map[string]string{
		// known good from user log: mkuosckvso.com decodes to cakeisalie.co.uk
		"mkuosckvso.com": "cakeisalie.co.uk",
	}
	for in, want := range cases {
		got := decodeCFDomain(in)
		if got != want {
			t.Errorf("decodeCFDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeCFDomain_PassesThroughNonDotCom(t *testing.T) {
	if got := decodeCFDomain("already.co.uk"); got != "already.co.uk" {
		t.Errorf("expected passthrough for non-.com, got %q", got)
	}
}

func TestDefaultCFProxyDomains_AllValid(t *testing.T) {
	pool := defaultCFProxyDomains()
	if len(pool) != len(defaultCFProxyEncoded) {
		t.Fatalf("default pool size mismatch: %d vs %d", len(pool), len(defaultCFProxyEncoded))
	}
	for _, d := range pool {
		if !isValidCFDomain(d) {
			t.Errorf("default decoded domain failed validation: %q", d)
		}
	}
}

func TestIsValidCFDomain(t *testing.T) {
	good := []string{"example.com", "kws1.cakeisalie.co.uk", "a.b.co"}
	bad := []string{"", ".com", "example.", "a..b", "-bad.com", "bad-.com", "x.1", "x." + string(rune('a'-1))}
	for _, d := range good {
		if !isValidCFDomain(d) {
			t.Errorf("expected valid: %q", d)
		}
	}
	for _, d := range bad {
		if isValidCFDomain(d) {
			t.Errorf("expected invalid: %q", d)
		}
	}
}

func TestBalancer_PinAndRotation(t *testing.T) {
	b := newCFBalancer()
	if b.size() != len(defaultCFProxyEncoded) {
		t.Fatalf("expected default pool size %d, got %d", len(defaultCFProxyEncoded), b.size())
	}
	domains := b.domainsForDC(2)
	if len(domains) != b.size() {
		t.Fatalf("expected %d domains for DC2, got %d", b.size(), len(domains))
	}
	// pin a domain different from the randomly-seeded current one and verify
	// it's listed first. Picking != current avoids a flake when seedPerDC
	// happened to seed our target as DC2's active domain already.
	target := domains[0]
	if target == b.perDC[2] {
		target = domains[len(domains)-1]
	}
	if !b.pin(2, target) {
		t.Fatal("first pin should report change=true")
	}
	if b.pin(2, target) {
		t.Fatal("idempotent pin should report change=false")
	}
	got := b.domainsForDC(2)
	if got[0] != target {
		t.Errorf("expected pinned %q first, got %q", target, got[0])
	}
}

func TestBalancer_UpdateDomainsList_ReplacesPool(t *testing.T) {
	b := newCFBalancer()
	newList := []string{"a.co.uk", "b.co.uk", "c.co.uk"}
	b.updateDomainsList(newList)
	if b.size() != 3 {
		t.Errorf("expected size 3 after update, got %d", b.size())
	}
	// unchanged update is a no-op
	b.updateDomainsList([]string{"c.co.uk", "b.co.uk", "a.co.uk"}) // same set
	if b.size() != 3 {
		t.Errorf("set-equivalent update should not change pool, got size %d", b.size())
	}
}

func TestNormalizeCFDomains_LowercaseTrimDedupe(t *testing.T) {
	in := []string{"A.com", "  b.co.uk  ", "a.com", "invalid..", "c.com"}
	out := normalizeCFDomains(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 valid domains, got %v", out)
	}
	if out[0] != "a.com" {
		t.Errorf("expected lowercase first, got %q", out[0])
	}
}
