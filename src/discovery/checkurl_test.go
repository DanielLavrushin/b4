package discovery

import "testing"

func TestUpgradeCheckURLTurnsATinyHTTPErrorIntoHTTPS(t *testing.T) {
	di := DomainInput{Domain: "web.whatsapp.com", CheckURL: "http://web.whatsapp.com/"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0})
	if !ok {
		t.Fatal("an http:// URL that the origin answers with 403 and an empty body can never pass, it must be upgraded")
	}
	if got != "https://web.whatsapp.com/" {
		t.Fatalf("got %q, want https://web.whatsapp.com/", got)
	}
}

func TestUpgradeCheckURLKeepsHostAndPath(t *testing.T) {
	di := DomainInput{Domain: "example.com", CheckURL: "http://example.com:8080/check/path?x=1"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 404, BytesRead: 12})
	if !ok || got != "https://example.com:8080/check/path?x=1" {
		t.Fatalf("got (%q, %v), want the same host and path under https", got, ok)
	}
}

func TestUpgradeCheckURLLeavesAWorkingHTTPPageAlone(t *testing.T) {
	di := DomainInput{Domain: "example.com", CheckURL: "http://example.com/"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusComplete, StatusCode: 200, BytesRead: 5000})
	if ok || got != di.CheckURL {
		t.Fatalf("got (%q, %v), want the user's URL untouched", got, ok)
	}
}

func TestUpgradeCheckURLLeavesHTTPSAlone(t *testing.T) {
	di := DomainInput{Domain: "example.com", CheckURL: "https://example.com/"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0})
	if ok || got != di.CheckURL {
		t.Fatalf("got (%q, %v), want the explicit https URL untouched", got, ok)
	}
}

func TestUpgradeCheckURLLeavesATransportDropAlone(t *testing.T) {
	di := DomainInput{Domain: "example.com", CheckURL: "http://example.com/"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 0, BytesRead: 0})
	if ok || got != di.CheckURL {
		t.Fatalf("got (%q, %v), want no upgrade when the origin never answered", got, ok)
	}
}

func TestUpgradeCheckURLLeavesABlockPageAlone(t *testing.T) {
	di := DomainInput{Domain: "example.com", CheckURL: "http://example.com/"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 451, BytesRead: 0})
	if ok || got != di.CheckURL {
		t.Fatalf("got (%q, %v), want no upgrade for an ISP block page, that is the path answering, not the origin", got, ok)
	}
}

func TestUpgradeCheckURLLeavesAFullBodyAlone(t *testing.T) {
	di := DomainInput{Domain: "example.com", CheckURL: "http://example.com/"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 403, BytesRead: minSuccessBytes})
	if ok || got != di.CheckURL {
		t.Fatalf("got (%q, %v), want no upgrade when the 403 carried a real body", got, ok)
	}
}

func TestUpgradeCheckURLLeavesABlockPageBodyAlone(t *testing.T) {
	di := DomainInput{Domain: "example.com", CheckURL: "http://example.com/"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 403, BytesRead: 512, Error: "ISP block page detected in response"})
	if ok {
		t.Fatalf("got (%q, %v), want no upgrade when a 403 carried an ISP block page, the origin never answered", got, ok)
	}
	got, ok = upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 403, BytesRead: 512, Error: "all 2 IPs failed: ISP block page detected in response"})
	if ok || got != di.CheckURL {
		t.Fatalf("got (%q, %v), want no upgrade when a 403 carried an ISP block page, the origin never answered", got, ok)
	}
}

func TestUpgradeCheckURLSeesThroughTheAllIPsFailedWrapper(t *testing.T) {
	di := DomainInput{Domain: "example.org", CheckURL: "http://example.org/"}
	got, ok := upgradeCheckURL(di, CheckResult{Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "all 2 IPs failed: insufficient data: 0 bytes"})
	if !ok || got != "https://example.org/" {
		t.Fatalf("got (%q, %v), want the upgrade when every pinned IP answered the same tiny 403", got, ok)
	}
}

func TestRewriteDeadEndCheckURLsTouchesOnlyTheDeadEndDomain(t *testing.T) {
	inputs := []DomainInput{
		{Domain: "dead.example", CheckURL: "http://dead.example/"},
		{Domain: "fine.example", CheckURL: "https://fine.example/"},
	}
	ds := &DiscoverySuite{
		CheckSuite: NewCheckSuite(inputs),
		domainResults: map[string]*DomainDiscoveryResult{
			"dead.example": {Domain: "dead.example", Url: "http://dead.example/", Results: map[string]*DomainPresetResult{
				presetNoBypass:  {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "all 2 IPs failed: insufficient data: 0 bytes"},
				"combo-pastseq": {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "insufficient data: 0 bytes"},
			}},
			"fine.example": {Domain: "fine.example", Url: "https://fine.example/", Results: map[string]*DomainPresetResult{
				presetNoBypass:  {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "insufficient data: 0 bytes"},
				"combo-pastseq": {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "insufficient data: 0 bytes"},
			}},
		},
	}
	ds.TotalChecks = 40

	upgraded := ds.rewriteDeadEndCheckURLs(3)

	if len(upgraded) != 1 || upgraded[0] != "dead.example" {
		t.Fatalf("upgraded = %v, want only dead.example", upgraded)
	}
	if ds.Domains[0].CheckURL != "https://dead.example/" {
		t.Fatalf("Domains[0].CheckURL = %q, want https://dead.example/", ds.Domains[0].CheckURL)
	}
	if ds.CheckURL != "https://dead.example/" {
		t.Fatalf("primary CheckURL = %q, want https://dead.example/", ds.CheckURL)
	}
	if ds.domainResults["dead.example"].Url != "https://dead.example/" {
		t.Fatalf("dead.example Url = %q, want https://dead.example/", ds.domainResults["dead.example"].Url)
	}
	if ds.Domains[1].CheckURL != "https://fine.example/" || ds.domainResults["fine.example"].Url != "https://fine.example/" {
		t.Fatalf("fine.example changed: CheckURL %q, Url %q", ds.Domains[1].CheckURL, ds.domainResults["fine.example"].Url)
	}
	if ds.TotalChecks != 43 {
		t.Fatalf("TotalChecks = %d, want 43 (the phase-1 re-run for the upgraded domain)", ds.TotalChecks)
	}
}

func TestRewriteDeadEndCheckURLsLeavesASecondaryPrimaryURLAlone(t *testing.T) {
	inputs := []DomainInput{
		{Domain: "primary.example", CheckURL: "https://primary.example/"},
		{Domain: "dead.example", CheckURL: "http://dead.example/"},
	}
	ds := &DiscoverySuite{
		CheckSuite: NewCheckSuite(inputs),
		domainResults: map[string]*DomainDiscoveryResult{
			"primary.example": {Domain: "primary.example", Url: "https://primary.example/", Results: map[string]*DomainPresetResult{
				presetNoBypass: {Status: CheckStatusComplete, StatusCode: 200, BytesRead: 9000},
			}},
			"dead.example": {Domain: "dead.example", Url: "http://dead.example/", Results: map[string]*DomainPresetResult{
				presetNoBypass:  {Status: CheckStatusFailed, StatusCode: 404, BytesRead: 3, Error: "insufficient data: 3 bytes"},
				"combo-pastseq": {Status: CheckStatusFailed, StatusCode: 404, BytesRead: 3, Error: "insufficient data: 3 bytes"},
			}},
		},
	}

	upgraded := ds.rewriteDeadEndCheckURLs(3)

	if len(upgraded) != 1 || upgraded[0] != "dead.example" {
		t.Fatalf("upgraded = %v, want only dead.example", upgraded)
	}
	if ds.CheckURL != "https://primary.example/" {
		t.Fatalf("primary CheckURL = %q, want it untouched when the dead end is a secondary domain", ds.CheckURL)
	}
	if ds.Domains[1].CheckURL != "https://dead.example/" {
		t.Fatalf("Domains[1].CheckURL = %q, want https://dead.example/", ds.Domains[1].CheckURL)
	}
}

func TestDeadEndAnswerNeedsTheSameErrorUnderEveryStrategy(t *testing.T) {
	uniform := map[string]*DomainPresetResult{
		presetNoBypass:  {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "insufficient data: 0 bytes"},
		"combo-pastseq": {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "insufficient data: 0 bytes"},
		"disorder":      {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "insufficient data: 0 bytes"},
	}
	if r, ok := deadEndAnswer(uniform); !ok || r.StatusCode != 403 {
		t.Fatalf("got (%+v, %v), want the shared 403 when every strategy sees the same tiny error", r, ok)
	}

	injected := map[string]*DomainPresetResult{
		presetNoBypass:  {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 100, Error: "insufficient data: 100 bytes"},
		"combo-pastseq": {Status: CheckStatusComplete, StatusCode: 200, BytesRead: 20000},
	}
	if _, ok := deadEndAnswer(injected); ok {
		t.Fatal("a strategy that got past the 403 proves the DPI injected it, the http URL must stay")
	}

	dropped := map[string]*DomainPresetResult{
		presetNoBypass:  {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "insufficient data: 0 bytes"},
		"combo-pastseq": {Status: CheckStatusFailed, StatusCode: 0, Error: "TLS handshake timed out (drop)"},
	}
	if _, ok := deadEndAnswer(dropped); ok {
		t.Fatal("a strategy that changed the failure shows the path reacts to it, the http URL must stay")
	}

	alone := map[string]*DomainPresetResult{
		presetNoBypass: {Status: CheckStatusFailed, StatusCode: 403, BytesRead: 0, Error: "insufficient data: 0 bytes"},
	}
	if _, ok := deadEndAnswer(alone); ok {
		t.Fatal("one unbypassed fetch is not evidence about the origin")
	}
}

func TestCheckURLPortIsThePortTheCheckDials(t *testing.T) {
	for raw, want := range map[string]int{
		"https://web.whatsapp.com/":         443,
		"http://web.whatsapp.com/":          80,
		"HTTP://web.whatsapp.com/":          80,
		"https://example.com:8443/check":    8443,
		"http://example.com:8080/check?x=1": 8080,
		"":                                  443,
	} {
		if got := checkURLPort(raw); got != want {
			t.Errorf("checkURLPort(%q) = %d, want %d, the gateway probe must test the port the check dials", raw, got, want)
		}
	}
}
