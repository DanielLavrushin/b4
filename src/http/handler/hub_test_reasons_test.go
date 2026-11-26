package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub/hubtest"
	"github.com/daniellavrushin/b4/watchdog"
)

func uiErrorText(t *testing.T, locale, code string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "ui", "src", "i18n", locale+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Errors map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	text, _ := doc.Errors[code].(string)
	return text
}

func expectTranslatedField(t *testing.T, fields []FieldError, code string, params map[string]any) {
	t.Helper()
	if len(fields) != 1 {
		t.Fatalf("want one field error %s, got %+v", code, fields)
	}
	f := fields[0]
	if f.Code != code || f.Path != "domain" {
		t.Fatalf("field error %s at %q, want %s at domain", f.Code, f.Path, code)
	}
	if len(f.Params) != len(params) {
		t.Errorf("%s carries params %v, want %v", code, f.Params, params)
	}
	for k, v := range params {
		if f.Params[k] != v {
			t.Errorf("%s: param %s is %v, want %v", code, k, f.Params[k], v)
		}
	}
	for _, locale := range []string{"en", "ru"} {
		text := uiErrorText(t, locale, code)
		if text == "" {
			t.Errorf("%s.json has no errors.%s, so the UI shows the English message", locale, code)
			continue
		}
		for k := range params {
			if !strings.Contains(text, "{{"+k+"}}") {
				t.Errorf("%s.json errors.%s leaves out {{%s}}: %q", locale, code, k, text)
			}
		}
	}
}

func expectTranslatedCode(t *testing.T, code string) {
	t.Helper()
	for _, locale := range []string{"en", "ru"} {
		if uiErrorText(t, locale, code) == "" {
			t.Errorf("%s.json has no errors.%s, so the UI shows the English message", locale, code)
		}
	}
}

func hubTestEnvFor(t *testing.T, domains ...string) *hubEnv {
	t.Helper()
	env := newHubEnv(t)
	set := hubStrategySet("Refusals", domains...)
	cs, _ := hubtest.CatalogueSet(t, "refuse-1", 1, &set, nil)
	env.publish(t, cs)
	return env
}

func TestTheHubTestRefusesInTheUIsLanguage(t *testing.T) {
	env := hubTestEnvFor(t, "*.wild.example")
	cases := []struct {
		name, domain, code, field string
		params                    map[string]any
	}{
		{"no plain domain in the set", "", "no_domain", "", nil},
		{"a private address", "192.168.1.1", "bad_request", "private_host", map[string]any{"host": "192.168.1.1"}},
		{"not a bare host name", "exa mple.com", "not_fetched", "invalid_host", map[string]any{"value": "exa mple.com"}},
		{"not written in ASCII", "пример.рф", "not_fetched", "host_not_ascii", map[string]any{"value": "пример.рф"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := postJSON(t, env.mux, "/api/hub/sets/refuse-1/test", HubTestRequest{Domain: c.domain})
			expectCode(t, rec, http.StatusBadRequest, c.code)
			var body APIError
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if c.field == "" {
				if len(body.Fields) != 0 {
					t.Errorf("unexpected field errors %+v", body.Fields)
				}
				expectTranslatedCode(t, c.code)
				return
			}
			expectTranslatedField(t, body.Fields, c.field, c.params)
		})
	}
}

func TestTheHubTestNamesAHubThatCannotServeItInTheUIsLanguage(t *testing.T) {
	env := hubTestEnvFor(t, "example.com")
	env.update(func(cfg *config.Config) { cfg.System.Hub.Enabled = false })
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/refuse-1/test", HubTestRequest{}), http.StatusConflict, "hub_disabled")

	env.update(func(cfg *config.Config) { cfg.System.Hub.Enabled = true })
	SetHubService(nil)
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/refuse-1/test", HubTestRequest{}), http.StatusServiceUnavailable, "hub_unavailable")

	for _, code := range []string{"hub_disabled", "hub_unavailable", "hub_not_configured", "not_found", "invalid_json"} {
		expectTranslatedCode(t, code)
	}
}

func TestANotFetchedReasonReachesTheUIInItsLanguage(t *testing.T) {
	cases := []struct {
		name   string
		err    *watchdog.ErrUnhandledFamily
		code   string
		params map[string]any
	}{
		{"IPv6 only", &watchdog.ErrUnhandledFamily{Host: "six.example", Family: "IPv6"},
			"unhandled_family_ipv6", map[string]any{"host": "six.example"}},
		{"IPv6 only after a redirect", &watchdog.ErrUnhandledFamily{Host: "www.six.example", Family: "IPv6", From: "six.example"},
			"unhandled_family_ipv6_redirect", map[string]any{"host": "www.six.example", "from": "six.example"}},
		{"IPv4 only", &watchdog.ErrUnhandledFamily{Host: "four.example", Family: "IPv4"},
			"unhandled_family_ipv4", map[string]any{"host": "four.example"}},
		{"IPv4 only after a redirect", &watchdog.ErrUnhandledFamily{Host: "www.four.example", Family: "IPv4", From: "four.example"},
			"unhandled_family_ipv4_redirect", map[string]any{"host": "www.four.example", "from": "four.example"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := mcpProbeResultOf("six.example", probeModeThroughB4, watchdog.CheckResult{}, c.err)
			e := hubNotFetched(&r)
			if e.Code != "not_fetched" || e.Message != c.err.Error() {
				t.Errorf("got %s %q, want not_fetched with the English reason for API clients", e.Code, e.Message)
			}
			expectTranslatedField(t, e.Fields, c.code, c.params)
		})
	}
}
