package uicheck

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var apiEntryRe = regexp.MustCompile(`(?s)\n  delete:(.*?)\n  [a-zA-Z]+:`)

func TestPayloadDeleteRequestEncodesItsParameters(t *testing.T) {
	m := apiEntryRe.FindStringSubmatch(readUI(t, "api/capture.ts"))
	if m == nil {
		t.Fatal("api/capture.ts has no captureApi.delete entry")
	}
	entry := m[1]
	for _, raw := range []string{"${protocol}", "${domain}"} {
		if strings.Contains(entry, raw) {
			t.Errorf("captureApi.delete puts %s into the URL unencoded: %s", raw, strings.TrimSpace(entry))
		}
	}
	if !strings.Contains(entry, "URLSearchParams") {
		t.Errorf("captureApi.delete does not build its query with URLSearchParams: %s", strings.TrimSpace(entry))
	}

	for _, name := range []string{"a&b", "a#b", "a+b", "50%off"} {
		query := url.Values{"protocol": {"tls"}, "domain": {name}}.Encode()
		got, err := url.ParseQuery(query)
		if err != nil {
			t.Errorf("%q: the form-encoded query %q does not parse: %v", name, query, err)
			continue
		}
		if got.Get("domain") != name || got.Get("protocol") != "tls" {
			t.Errorf("%q: the form-encoded query %q reads back as protocol %q, domain %q", name, query, got.Get("protocol"), got.Get("domain"))
		}
	}
}
