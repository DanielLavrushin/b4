package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/detector"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/netprobe"
)

func TestAResolverB4OffersCanBeShared(t *testing.T) {
	var suggested []struct {
		URL string `json:"url"`
	}
	raw, err := os.ReadFile("../ui/src/assets/doh.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &suggested); err != nil {
		t.Fatal(err)
	}
	offered := slices.Clone(netprobe.WireDoHServers)
	for _, s := range suggested {
		offered = append(offered, s.URL)
	}
	for _, s := range detector.EmbeddedLists().DNSServers {
		if s.Kind == "doh" {
			offered = append(offered, s.Address)
		}
	}
	if len(netprobe.WireDoHServers) == 0 || len(suggested) == 0 || len(offered) <= len(netprobe.WireDoHServers)+len(suggested) {
		t.Fatalf("a list of resolvers is empty: %v", offered)
	}
	for _, doh := range offered {
		set := hubStrategySet("My strategy", "example.com")
		set.DNS.Enabled = true
		set.DNS.DoHURL = doh
		env, report, err := hubwire.Build(&set, hubwire.BuildOptions{})
		if err != nil {
			t.Fatalf("%s: %v", doh, err)
		}
		if dns, _ := env.Set["dns"].(map[string]interface{}); dns["doh_url"] != doh {
			t.Errorf("%s: b4 offers this resolver and leaves it out of a shared set: %+v", doh, report)
		}
	}
}

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
