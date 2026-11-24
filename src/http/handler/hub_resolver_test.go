package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestASharedSetLeavesAPersonalResolverOut(t *testing.T) {
	env := newHubEnv(t)
	local := hubStrategySet("My strategy", "example.com")
	local.Id = "local-1"
	local.DNS.Enabled = true
	local.DNS.DoHURL = "https://canaryuser:canarypass@dns.nextdns.io/canaryid?token=canaryquery"
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{&local} })

	rec := postJSON(t, env.mux, "/api/hub/envelope", local)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "canary") {
		t.Errorf("the prepared set holds a private part of the resolver: %s", rec.Body.String())
	}
	var prepared HubEnvelopeResponse
	decodeInto(t, rec, &prepared)
	found := false
	for _, s := range prepared.Report.Stripped {
		found = found || s.Path == "dns.doh_url" && s.Reason == "personal_resolver"
	}
	if !found {
		t.Errorf("the share section must list the resolver as left out: %+v", prepared.Report)
	}

	rec = postJSON(t, env.mux, "/api/hub/share", HubShareRequest{SetID: "local-1"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	records := env.hub.Records()
	if len(records) != 1 || strings.Contains(string(records[0].Body), "canary") {
		t.Errorf("the published set must not hold a private part of the resolver: %+v", records)
	}
	stamped := env.localSet("local-1")
	if stamped == nil || stamped.DNS.DoHURL != local.DNS.DoHURL {
		t.Fatalf("the set on this router keeps its resolver, got %+v", stamped)
	}
	if hubStateOf(env.api.getCfg(), stamped) != HubStateUnmodified {
		t.Error("a set just shared reads as unmodified")
	}
}
