package handler

import (
	"net/http"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub/hubtest"
	"github.com/daniellavrushin/b4/hubwire"
)

func watchASNRefreshes(t *testing.T, ready func() bool) func() bool {
	t.Helper()
	drainASNRefresh()
	done := make(chan struct{})
	var mu sync.Mutex
	saw := false
	go func() {
		for {
			select {
			case <-done:
				return
			case <-config.ASNRefreshRequests():
				if ready() {
					mu.Lock()
					saw = true
					mu.Unlock()
				}
			}
		}
	}()
	t.Cleanup(func() { close(done) })
	return func() bool {
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			ok := saw
			mu.Unlock()
			if ok || time.Now().After(deadline) {
				return ok
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func configReferencesASN(cfg *config.Config, id string) bool {
	for _, set := range cfg.Sets {
		if set != nil && slices.Contains(set.Targets.ASNs, id) {
			return true
		}
	}
	return false
}

func hubASNSet(name string, asns ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = name
	set.Targets.ASNs = asns
	set.Fragmentation.Strategy = "tls"
	set.Faking.SNI = true
	set.Faking.TTL = 5
	return set
}

func asnWarning(ws []hubwire.Warning) ([]string, bool) {
	for _, w := range ws {
		if w.Code != "asn_unresolved" {
			continue
		}
		var out []string
		switch v := w.Params["asns"].(type) {
		case []string:
			out = v
		case []interface{}:
			for _, item := range v {
				s, _ := item.(string)
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func TestHubApplyAcceptsASetThatTargetsOnlyASNs(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	env := newHubEnv(t)
	shared := hubASNSet("Telegram", "62041", "44907")
	cs, _ := hubtest.CatalogueSet(t, "tg-1", 1, &shared, nil)
	env.publish(t, cs)
	refreshedAfterSave := watchASNRefreshes(t, func() bool { return configReferencesASN(env.api.getCfg(), "44907") })

	rec := postJSON(t, env.mux, "/api/hub/sets/tg-1/apply", map[string]interface{}{})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("a set whose only targets are ASNs must apply, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubApplyResponse
	decodeInto(t, rec, &resp)
	unresolved, ok := asnWarning(resp.Warnings)
	if !ok || !reflect.DeepEqual(unresolved, []string{"44907"}) {
		t.Errorf("the ASN with no known prefixes must be reported: %+v", resp.Warnings)
	}

	applied := env.localSet(resp.ID)
	if applied == nil {
		t.Fatalf("the applied set was not saved: %+v", env.api.getCfg().Sets)
	}
	if !reflect.DeepEqual(applied.Targets.ASNs, []string{"62041", "44907"}) {
		t.Errorf("the ASN references must be kept: %v", applied.Targets.ASNs)
	}
	if !reflect.DeepEqual(applied.Targets.IpsToMatch, telegramPrefixes) {
		t.Errorf("the resolved ASN must expand into the match list: %v", applied.Targets.IpsToMatch)
	}
	if state := hubStateOf(env.api.getCfg(), applied); state != HubStateUnmodified {
		t.Errorf("a freshly applied ASN set must read as unmodified, got %q", state)
	}
	if !refreshedAfterSave() {
		t.Error("the refresher must be asked to resolve the unknown ASN once the set is saved, or it can miss it until the next hourly pass")
	}

	rec = getJSON(t, env.mux, "/api/hub/sets/tg-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var view HubSet
	decodeInto(t, rec, &view)
	if !reflect.DeepEqual(view.Targets.ASNs, []string{"62041", "44907"}) || view.Targets.IPCount != 0 {
		t.Errorf("the hub set view must list the ASNs: %+v", view.Targets)
	}
}

func TestHubApplyReplaceKeepsTheHubCopysASNs(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	env := newHubEnv(t)
	shared := hubASNSet("Telegram", "62041")
	shared.Targets.SNIDomains = []string{"telegram.org"}
	cs, _ := hubtest.CatalogueSet(t, "tg-2", 2, &shared, nil)
	env.publish(t, cs)

	local := hubASNSet("Telegram here", "62041")
	local.Id = "local-1"
	local.Enabled = false
	local.Faking.TTL = 9
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{&local} })

	rec := postJSON(t, env.mux, "/api/hub/sets/tg-2/apply", map[string]interface{}{"replace": "local-1"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubApplyResponse
	decodeInto(t, rec, &resp)
	if _, ok := asnWarning(resp.Warnings); ok {
		t.Errorf("a resolved ASN must not be reported: %+v", resp.Warnings)
	}
	cfg := env.api.getCfg()
	if len(cfg.Sets) != 1 || cfg.Sets[0].Id != "local-1" {
		t.Fatalf("the replaced set must keep its id and place: %+v", cfg.Sets)
	}
	replaced := cfg.Sets[0]
	if replaced.Enabled || replaced.Faking.TTL != 5 {
		t.Errorf("replace keeps the enabled flag and takes the hub strategy: enabled=%v ttl=%d", replaced.Enabled, replaced.Faking.TTL)
	}
	if !reflect.DeepEqual(replaced.Targets.ASNs, []string{"62041"}) || !reflect.DeepEqual(replaced.Targets.SNIDomains, []string{"telegram.org"}) {
		t.Errorf("the hub copy's targets must be applied: %+v", replaced.Targets)
	}
	if !reflect.DeepEqual(replaced.Targets.IpsToMatch, telegramPrefixes) {
		t.Errorf("the replaced set must keep matching the ASN's prefixes: %v", replaced.Targets.IpsToMatch)
	}
	if replaced.Hub == nil || replaced.Hub.ID != "tg-2" || hubStateOf(cfg, replaced) != HubStateUnmodified {
		t.Errorf("the replaced set must carry the hub stamp and read as unmodified: %+v", replaced.Hub)
	}
}

func TestHubApplyStillRefusesASetWithNoTargets(t *testing.T) {
	useAsnStore(t)
	env := newHubEnv(t)
	shared := hubASNSet("Nothing")
	shared.Targets.GeoSiteCategories = []string{"youtube"}
	cs, _ := hubtest.CatalogueSet(t, "none-1", 1, &shared, nil)
	env.publish(t, cs)

	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/none-1/apply", map[string]interface{}{}), http.StatusBadRequest, "no_targets")
}

func TestHubImportWarnsAboutUnresolvedASNs(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	mux, _ := hubAPI(t)
	set := hubASNSet("Telegram", "AS62041", "44907")
	env, _, err := hubwire.Build(&set, hubwire.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, mux, "/api/hub/import", env)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubImportResponse
	decodeInto(t, rec, &resp)
	if resp.Set == nil || !reflect.DeepEqual(resp.Set.Targets.ASNs, []string{"62041", "44907"}) {
		t.Fatalf("the imported set must carry the ASNs: %+v", resp.Set)
	}
	unresolved, ok := asnWarning(resp.Warnings)
	if !ok || !reflect.DeepEqual(unresolved, []string{"44907"}) {
		t.Errorf("the ASN with no known prefixes must be reported: %+v", resp.Warnings)
	}
}

func TestHubASNWarningsNameOnlyUnresolvedASNs(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	set := hubASNSet("x", "62041")
	if got := hubASNWarnings(&set); got != nil {
		t.Errorf("a resolved ASN needs no warning: %+v", got)
	}
	set.Targets.ASNs = []string{"15169", "62041", "13335"}
	got, ok := asnWarning(hubASNWarnings(&set))
	if !ok || !reflect.DeepEqual(got, []string{"15169", "13335"}) {
		t.Errorf("expected the two unknown ASNs in order, got %v", got)
	}
	empty := hubASNSet("y")
	if got := hubASNWarnings(&empty); got != nil {
		t.Errorf("a set without ASNs needs no warning: %+v", got)
	}
}
