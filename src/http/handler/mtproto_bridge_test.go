package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
)

func bridgeTestAPI(t *testing.T, cfg *config.Config) (*API, *http.ServeMux) {
	t.Helper()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	api := &API{cfgPtr: testCfgPtr(cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterMTProtoApi()

	prevProbe, prevCached, prevListener, prevNetfilter, prevFamilies := bridgeTProxyProbe, bridgeTProxyCached, bridgeListenerFunc, readBridgeNetfilter, routingSetFamilies
	t.Cleanup(func() {
		bridgeTProxyProbe, bridgeTProxyCached, bridgeListenerFunc, readBridgeNetfilter, routingSetFamilies = prevProbe, prevCached, prevListener, prevNetfilter, prevFamilies
	})
	readBridgeNetfilter = func() tables.BridgeNetfilter { return tables.BridgeNetfilter{} }
	routingSetFamilies = func(string) (bool, bool, bool) { return false, false, false }
	bridgeTProxyCached = func(*config.Config) BridgeTProxyInfo {
		return BridgeTProxyInfo{Missing: []string{}, Packages: []string{}}
	}
	bridgeListenerFunc = func() BridgeListenerInfo {
		return BridgeListenerInfo{Running: true, Port: 13443, V4: true, Active: 2, V6Error: "tproxy v6 listen [::]:13443: address family not supported"}
	}
	return api, mux
}

func getBridgeStatus(t *testing.T, mux *http.ServeMux, query string) TelegramBridgeStatus {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/mtproto/bridge"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var st TelegramBridgeStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return st
}

func TestTelegramBridgeStatus(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.MTProto.Bridge.Enabled = true
	legacy := config.NewSetConfig()
	legacy.Id, legacy.Name = "legacy", "telegram-ws"
	legacy.Routing.Enabled = true
	legacy.Routing.Mode = config.RoutingModeMTProtoWS
	dpi := config.NewSetConfig()
	dpi.Id, dpi.Name = "dpi", "youtube"
	cfg.Sets = []*config.SetConfig{&legacy, &dpi}
	_, mux := bridgeTestAPI(t, &cfg)

	var probes []bool
	bridgeTProxyProbe = func(_ *config.Config, recheck bool) BridgeTProxyInfo {
		probes = append(probes, recheck)
		return BridgeTProxyInfo{Checked: true, Available: false, Missing: []string{"nft_tproxy"}, Packages: []string{"kmod-nft-tproxy"}}
	}

	st := getBridgeStatus(t, mux, "")
	if !st.Success || !st.Enabled {
		t.Errorf("success=%v enabled=%v", st.Success, st.Enabled)
	}
	if st.Listener.Port != 13443 || !st.Listener.Running || st.Listener.Active != 2 {
		t.Errorf("listener %+v", st.Listener)
	}
	if st.Addresses.Total != len(config.CurrentTelegramCIDRs().All) || st.Addresses.Source == "" {
		t.Errorf("addresses %+v", st.Addresses)
	}
	if len(st.LegacySets) != 1 || st.LegacySets[0].ID != "legacy" || st.LegacySets[0].Name != "telegram-ws" {
		t.Errorf("legacy sets %+v", st.LegacySets)
	}
	if !st.TProxy.Checked || st.TProxy.Available || st.TProxy.Packages[0] != "kmod-nft-tproxy" {
		t.Errorf("tproxy %+v", st.TProxy)
	}

	if st.Listener.V6Error != "" {
		t.Errorf("an IPv6 listen error was reported while IPv6 support is off: %q", st.Listener.V6Error)
	}

	getBridgeStatus(t, mux, "?check=1")
	if len(probes) != 2 || probes[0] || !probes[1] {
		t.Errorf("a plain status read uses the cached probe and check=1 forces a new one, got %v", probes)
	}

	mcp := buildTelegramBridgeStatus(&cfg, false, false)
	if len(probes) != 2 {
		t.Errorf("the MCP status must never run the firewall probe, probes %v", probes)
	}
	if mcp.TProxy.Checked {
		t.Errorf("the MCP status must report the cached probe only, got %+v", mcp.TProxy)
	}
}

func TestTelegramBridgeStatusDoesNotProbeWhileOff(t *testing.T) {
	cfg := config.NewConfig()
	_, mux := bridgeTestAPI(t, &cfg)
	bridgeTProxyProbe = func(*config.Config, bool) BridgeTProxyInfo {
		t.Error("the kernel probe ran while the bridge is off and nobody asked for it")
		return BridgeTProxyInfo{}
	}
	st := getBridgeStatus(t, mux, "")
	if st.Enabled || st.TProxy.Checked {
		t.Errorf("enabled=%v tproxy=%+v", st.Enabled, st.TProxy)
	}
	if st.LegacySets == nil || st.TProxy.Missing == nil || st.TProxy.Packages == nil {
		t.Error("lists must be empty arrays, not null")
	}
}

func TestTelegramBridgeStatusNamesBridgeNetfilter(t *testing.T) {
	cfg := config.NewConfig()
	_, mux := bridgeTestAPI(t, &cfg)
	bridgeTProxyProbe = func(*config.Config, bool) BridgeTProxyInfo {
		return BridgeTProxyInfo{Missing: []string{}, Packages: []string{}}
	}
	readBridgeNetfilter = func() tables.BridgeNetfilter {
		return tables.BridgeNetfilter{GlobalV4: true, GlobalV6: true, BridgesV4: []string{"br-lan"}, BridgesV6: []string{"br-lan", "br-v6"}}
	}
	var ipv4, ipv6, installed bool
	routingSetFamilies = func(id string) (bool, bool, bool) {
		if id != config.TelegramBridgeSetID {
			t.Errorf("asked for the families of set %q", id)
		}
		return ipv4, ipv6, installed
	}

	ipv4, ipv6, installed = true, true, true
	if st := getBridgeStatus(t, mux, ""); len(st.BridgeNetfilter) != 0 {
		t.Errorf("with the switch off the card has nothing to warn about, got %v", st.BridgeNetfilter)
	}

	cfg.System.MTProto.Bridge.Enabled = true
	ipv4, ipv6, installed = false, false, false
	if st := getBridgeStatus(t, mux, ""); len(st.BridgeNetfilter) != 0 {
		t.Errorf("without an installed bridge rule there is nothing to warn about, got %v", st.BridgeNetfilter)
	}

	ipv4, ipv6, installed = true, false, true
	if st := getBridgeStatus(t, mux, ""); !slices.Equal(st.BridgeNetfilter, []string{"br-lan"}) {
		t.Errorf("with the IPv6 listener down only the IPv4 bridges matter, got %v", st.BridgeNetfilter)
	}

	ipv4, ipv6, installed = true, true, true
	if st := getBridgeStatus(t, mux, ""); !slices.Equal(st.BridgeNetfilter, []string{"br-lan", "br-v6"}) {
		t.Errorf("with both families installed every affected bridge matters, got %v", st.BridgeNetfilter)
	}
}

func TestMTProtoConfigPostKeepsTheBridgeSwitch(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.MTProto.Bridge.Enabled = true
	api, mux := bridgeTestAPI(t, &cfg)

	post := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/mtproto/config", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
	}

	post(`{"enabled":false,"port":3128}`)
	if !api.getCfg().System.MTProto.Bridge.Enabled {
		t.Error("a request without the bridge block turned the bridge off")
	}
	post(`{"enabled":false,"port":3128,"bridge":{"enabled":false}}`)
	if api.getCfg().System.MTProto.Bridge.Enabled {
		t.Error("an explicit bridge block was ignored")
	}
}
