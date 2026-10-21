package handler

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub/hubtest"
)

const hubL7Ports = "19294-19344,50000-50100"

func hubL7Set(name string, classes ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = name
	set.UDP.DPortFilter = hubL7Ports
	set.UDP.FakePayloadFile = config.FakePayloadAutoSTUN
	set.UDP.FilterL7 = classes
	return set
}

func TestHubApplyAcceptsASetThatSelectsUDPProtocols(t *testing.T) {
	env := newHubEnv(t)
	shared := hubL7Set("Discord voice", config.UDPL7Discord, config.UDPL7STUN)
	cs, _ := hubtest.CatalogueSet(t, "dc-1", 1, &shared, nil)
	env.publish(t, cs)

	rec := postJSON(t, env.mux, "/api/hub/sets/dc-1/apply", map[string]interface{}{})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("a set that selects UDP traffic by protocol must apply, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubApplyResponse
	decodeInto(t, rec, &resp)
	for _, w := range resp.Warnings {
		if w.Code == "no_targets" {
			t.Errorf("a protocol selection is not a set without targets: %+v", resp.Warnings)
		}
	}

	cfg := env.api.getCfg()
	applied := env.localSet(resp.ID)
	if applied == nil || len(cfg.Sets) != 1 {
		t.Fatalf("the applied set was not saved: %+v", cfg.Sets)
	}
	if !applied.Enabled || applied.Name != "Discord voice" {
		t.Errorf("a new hub set is saved enabled under its title: enabled=%v name=%q", applied.Enabled, applied.Name)
	}
	if !reflect.DeepEqual(applied.UDP.FilterL7, []string{config.UDPL7Discord, config.UDPL7STUN}) || applied.UDP.DPortFilter != hubL7Ports {
		t.Errorf("the protocol selection and its ports must be applied: %+v", applied.UDP)
	}
	if applied.DeclaresDestinationTargets() {
		t.Errorf("the applied set must stay without targets: %+v", applied.Targets)
	}
	if applied.Hub == nil || applied.Hub.ID != "dc-1" || hubStateOf(cfg, applied) != HubStateUnmodified {
		t.Errorf("the applied set must carry the hub stamp and read as unmodified: %+v", applied.Hub)
	}
	reordered := *applied
	reordered.UDP.FilterL7 = []string{config.UDPL7STUN, config.UDPL7Discord}
	if state := hubStateOf(cfg, &reordered); state != HubStateUnmodified {
		t.Errorf("the same protocols in another order leave the set unmodified, got %s", state)
	}

	rec = getJSON(t, env.mux, "/api/hub/sets/dc-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var view HubSet
	decodeInto(t, rec, &view)
	if !reflect.DeepEqual(view.Targets.UDPL7, []string{config.UDPL7Discord, config.UDPL7STUN}) || len(view.Targets.Domains) != 0 {
		t.Errorf("the hub set view must name the protocols it selects: %+v", view.Targets)
	}

	var list HubSetsResponse
	decodeInto(t, getJSON(t, env.mux, "/api/hub/sets"), &list)
	if list.Total != 1 || len(list.Sets) != 1 || list.Sets[0].ID != "dc-1" {
		t.Errorf("the listing without a domain must show the set: %+v", list)
	}
	decodeInto(t, getJSON(t, env.mux, "/api/hub/sets?domain=discord.com"), &list)
	if list.Total != 0 {
		t.Errorf("a domain lookup finds only sets that cover the domain: %+v", list)
	}
}

func TestHubApplyAndImportRefuseAProtocolSelectionThatAlsoFiltersTCPPorts(t *testing.T) {
	env := newHubEnv(t)
	shared := hubL7Set("Discord voice", config.UDPL7Discord)
	shared.TCP.DPortFilter = "443,2053,8443"
	cs, _ := hubtest.CatalogueSet(t, "dc-tcp", 1, &shared, nil)
	env.publish(t, cs)

	for path, body := range map[string]interface{}{
		"/api/hub/sets/dc-tcp/apply": map[string]interface{}{},
		"/api/hub/import":            cs.ToEnvelope(),
	} {
		rec := postJSON(t, env.mux, path, body)
		expectCode(t, rec, http.StatusBadRequest, "bad_request")
		var refusal APIError
		decodeInto(t, rec, &refusal)
		if !strings.Contains(refusal.Message, "tcp.dport_filter") || !strings.Contains(refusal.Message, "TCP port filter") {
			t.Errorf("%s: the refusal must name the TCP port filter the config refuses on such a set: %q", path, refusal.Message)
		}
	}
	if sets := env.api.getCfg().Sets; len(sets) != 0 {
		t.Errorf("a set whose TCP ports would catch every TCP connection on them must not be saved: %+v", sets)
	}
}

func TestHubApplyRefusesASetThatSelectsOnlyUnknownProtocols(t *testing.T) {
	env := newHubEnv(t)
	shared := hubL7Set("WireGuard", "wireguard")
	cs, _ := hubtest.CatalogueSet(t, "wg-1", 1, &shared, nil)
	env.publish(t, cs)

	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/wg-1/apply", map[string]interface{}{}), http.StatusBadRequest, "no_targets")
	if sets := env.api.getCfg().Sets; len(sets) != 0 {
		t.Errorf("a set that selects only protocols this device does not match must not be saved: %+v", sets)
	}
}

func TestHubApplyStillRefusesAUDPSetWithoutProtocolSelection(t *testing.T) {
	env := newHubEnv(t)
	shared := hubL7Set("Ports only")
	cs, _ := hubtest.CatalogueSet(t, "ports-1", 1, &shared, nil)
	env.publish(t, cs)

	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/ports-1/apply", map[string]interface{}{}), http.StatusBadRequest, "no_targets")
	if sets := env.api.getCfg().Sets; len(sets) != 0 {
		t.Errorf("a refused set must not be saved: %+v", sets)
	}

	rec := getJSON(t, env.mux, "/api/hub/sets/ports-1")
	var view HubSet
	decodeInto(t, rec, &view)
	if view.Targets.UDPL7 == nil || len(view.Targets.UDPL7) != 0 {
		t.Errorf("a set without protocol selection must report an empty list, not null: %#v", view.Targets.UDPL7)
	}
}
