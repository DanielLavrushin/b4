package handler

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func putGeoIP(t *testing.T, mux *http.ServeMux, body map[string]any) (*AddIpResponse, *APIError, int) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := serve(mux, http.MethodPut, "/api/geoip", string(raw))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("every answer is JSON, got %q: %s", ct, rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		var ae APIError
		decodeInto(t, rec, &ae)
		return nil, &ae, rec.Code
	}
	var out AddIpResponse
	decodeInto(t, rec, &out)
	return &out, nil, rec.Code
}

func TestPutGeoIPAddsValidatedAddressesToASet(t *testing.T) {
	useAsnStore(t)
	target := asnSet("t")
	target.Targets.IPs = []string{"198.51.100.0/24"}
	target.Targets.IpsToMatch = []string{"198.51.100.0/24"}
	api, mux := asnAPI(t, target)

	out, ae, code := putGeoIP(t, mux, map[string]any{"set_id": "t", "cidr": []string{" 203.0.113.77/24 ", "198.51.100.0/24", "2001:db8::1", "::ffff:192.0.2.1", "203.0.113.0/24", ""}})
	if code != http.StatusOK {
		t.Fatalf("%d %+v", code, ae)
	}
	got := api.getCfg().GetSetById("t").Targets
	want := []string{"198.51.100.0/24", "203.0.113.0/24", "2001:db8::1", "192.0.2.1"}
	if !slices.Equal(got.IPs, want) {
		t.Fatalf("addresses are masked, unmapped and deduplicated: %v", got.IPs)
	}
	if !slices.Equal(got.IpsToMatch, want) {
		t.Errorf("the running match list follows: %v", got.IpsToMatch)
	}
	if !out.Success || out.SetId != "t" || out.TotalCidrs != 4 || out.AddedCidrs != 3 || out.TotalASNs != 0 {
		t.Errorf("response: %+v", out)
	}
}

func TestPutGeoIPAddsASNsAndAsksForUnknownOnes(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	api, mux := asnAPI(t, asnSet("t", "13335"))
	drainASNRefresh()

	out, ae, code := putGeoIP(t, mux, map[string]any{"set_id": "t", "asns": []string{"AS62041", "asn15169", "62041", "13335"}})
	if code != http.StatusOK {
		t.Fatalf("%d %+v", code, ae)
	}
	got := api.getCfg().GetSetById("t").Targets
	if !slices.Equal(got.ASNs, []string{"13335", "62041", "15169"}) {
		t.Fatalf("ASNs are normalized and appended once: %v", got.ASNs)
	}
	for _, p := range telegramPrefixes {
		if !slices.Contains(got.IpsToMatch, p) {
			t.Errorf("a known ASN matches at once, missing %s in %v", p, got.IpsToMatch)
		}
	}
	if out.AddedASNs != 2 || out.TotalASNs != 3 || !slices.Equal(out.UnresolvedASNs, []string{"15169", "13335"}) {
		t.Errorf("response: %+v", out)
	}
	if !drainASNRefresh() {
		t.Error("an unknown ASN is handed to the refresher")
	}
}

func TestPutGeoIPRefusesInvalidEntriesWithAJSONError(t *testing.T) {
	useAsnStore(t)
	api, mux := asnAPI(t, asnSet("t"))
	before := api.getCfg()

	_, ae, code := putGeoIP(t, mux, map[string]any{"set_id": "t", "cidr": []string{"10.0.0.0/8", "300.1.1.1", "example.com"}})
	if code != http.StatusBadRequest || ae.Code != "cidr_invalid" || len(ae.Fields) != 2 || ae.Fields[0].Path != "cidr[1]" || ae.Fields[1].Params["value"] != "example.com" {
		t.Fatalf("every bad address is named: %d %+v", code, ae)
	}
	_, ae, code = putGeoIP(t, mux, map[string]any{"set_id": "t", "asns": []string{"AS64500"}})
	if code != http.StatusBadRequest || ae.Code != "asn_invalid" {
		t.Fatalf("a reserved ASN is refused: %d %+v", code, ae)
	}
	_, ae, code = putGeoIP(t, mux, map[string]any{"set_id": "t", "cidr": []string{"nope"}, "asns": []string{"nope"}})
	if code != http.StatusBadRequest || ae.Code != "validation_failed" || len(ae.Fields) != 2 {
		t.Fatalf("mixed errors: %d %+v", code, ae)
	}
	_, ae, code = putGeoIP(t, mux, map[string]any{"set_id": "t", "cidr": []string{" "}})
	if code != http.StatusBadRequest || ae.Code != "bad_request" {
		t.Fatalf("nothing to add: %d %+v", code, ae)
	}
	if rec := serve(mux, http.MethodPut, "/api/geoip", "{"); rec.Code != http.StatusBadRequest || errorCode(t, rec) != "invalid_json" {
		t.Fatalf("bad JSON: %d %s", rec.Code, rec.Body.String())
	}
	_, ae, code = putGeoIP(t, mux, map[string]any{"set_id": "missing", "cidr": []string{"1.1.1.1"}})
	if code != http.StatusNotFound || ae.Code != "not_found" {
		t.Fatalf("unknown set: %d %+v", code, ae)
	}
	if api.getCfg() != before {
		t.Error("a refused request changes nothing")
	}
}

func TestPutGeoIPCapsTheListOfFieldErrors(t *testing.T) {
	useAsnStore(t)
	_, mux := asnAPI(t, asnSet("t"))
	junk := make([]string, 500)
	for i := range junk {
		junk[i] = "junk"
	}
	_, ae, _ := putGeoIP(t, mux, map[string]any{"set_id": "t", "cidr": junk})
	if len(ae.Fields) != maxTargetFieldErrors || !strings.Contains(ae.Message, "500") {
		t.Fatalf("the error stays small but counts everything: %d fields, %q", len(ae.Fields), ae.Message)
	}
}

func TestPutGeoIPCreatesANewSetFirst(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	api, mux := asnAPI(t, asnSet("old"))

	out, ae, code := putGeoIP(t, mux, map[string]any{"set_name": "Telegram", "cidr": []string{"95.161.64.0/20"}, "asns": []string{"62041"}})
	if code != http.StatusOK {
		t.Fatalf("%d %+v", code, ae)
	}
	cfg := api.getCfg()
	if len(cfg.Sets) != 2 || cfg.Sets[0].Id != out.SetId || cfg.Sets[0].Name != "Telegram" || cfg.Sets[1].Id != "old" {
		t.Fatalf("the new set is prepended: %+v", cfg.Sets)
	}
	created := cfg.Sets[0].Targets
	if !slices.Equal(created.IPs, []string{"95.161.64.0/20"}) || !slices.Equal(created.ASNs, []string{"62041"}) {
		t.Fatalf("targets: %+v", created)
	}
	if want := append(append([]string{}, telegramPrefixes...), "95.161.64.0/20"); !slices.Equal(created.IpsToMatch, want) {
		t.Errorf("expanded like the compiler (ASN prefixes, then manual IPs): %v", created.IpsToMatch)
	}

	out, _, _ = putGeoIP(t, mux, map[string]any{"set_id": config.CreateSetSentinel, "cidr": []string{"1.1.1.1"}})
	if got := api.getCfg().Sets[0]; got.Id != out.SetId || got.Name != "Set 3" {
		t.Errorf("an unnamed set is numbered: %+v", got.Name)
	}
}

func TestPutGeoIPKeepsAConcurrentSave(t *testing.T) {
	useAsnStore(t)
	api, mux := asnAPI(t, asnSet("t"), asnSet("other"))

	rec := serveWhileLocked(t, mux, http.MethodPut, "/api/geoip", `{"set_id":"t","cidr":["1.1.1.1"]}`, func() {
		storeConcurrently(t, api, func(c *config.Config) { c.GetSetById("other").TCP.Seg2Delay = 42 })
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	cfg := api.getCfg()
	if cfg.GetSetById("other").TCP.Seg2Delay != 42 {
		t.Error("a save made while the request waited is kept")
	}
	if !slices.Equal(cfg.GetSetById("t").Targets.IPs, []string{"1.1.1.1"}) {
		t.Error("the address is added")
	}
}

func TestPutGeoIPRefreshesTheFirewallWhenIpsetsChange(t *testing.T) {
	useAsnStore(t)
	dup := asnSet("dup")
	dup.TCP.Duplicate.Enabled = true
	dup.TCP.Duplicate.Count = 2
	_, mux := asnAPI(t, dup)
	refreshed := countRefreshes(t)

	if _, ae, code := putGeoIP(t, mux, map[string]any{"set_id": "dup", "cidr": []string{"8.8.8.0/24"}}); code != http.StatusOK {
		t.Fatalf("%d %+v", code, ae)
	}
	if *refreshed != 1 {
		t.Fatalf("the duplicate ipset must follow the new address: %d refreshes", *refreshed)
	}
}

func TestPutGeoIPWithNothingNewWritesNothing(t *testing.T) {
	useAsnStore(t)
	target := asnSet("t", "62041")
	target.Targets.IPs = []string{"1.1.1.1"}
	api, mux := asnAPI(t, target)
	before := api.getCfg()

	out, ae, code := putGeoIP(t, mux, map[string]any{"set_id": "t", "cidr": []string{"1.1.1.1"}, "asns": []string{"AS62041"}})
	if code != http.StatusOK || out.AddedCidrs != 0 || out.AddedASNs != 0 || out.TotalCidrs != 1 || out.TotalASNs != 1 {
		t.Fatalf("%d %+v %+v", code, out, ae)
	}
	if api.getCfg() != before {
		t.Error("nothing to add, nothing written")
	}
}

func TestPutGeoIPLogsACountNotTheList(t *testing.T) {
	useAsnStore(t)
	buf := captureLog(t)
	_, mux := asnAPI(t, asnSet("t"))
	putGeoIP(t, mux, map[string]any{"set_id": "t", "cidr": []string{"203.0.113.0/24", "198.51.100.0/24"}})
	got := buf.String()
	if strings.Contains(got, "203.0.113.0/24") || !strings.Contains(got, "Added 2 addresses and 0 ASNs") {
		t.Fatalf("the log carries a count, not the addresses:\n%s", got)
	}
}
