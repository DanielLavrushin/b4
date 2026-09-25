package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
	"github.com/daniellavrushin/b4/tables"
)

type BridgeListenerInfo struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	V4      bool   `json:"v4"`
	V6      bool   `json:"v6"`
	Active  int64  `json:"active"`
	Error   string `json:"error,omitempty"`
	V6Error string `json:"v6_error,omitempty"`
}

type BridgeTProxyInfo struct {
	Checked   bool     `json:"checked"`
	Available bool     `json:"available"`
	Missing   []string `json:"missing"`
	Packages  []string `json:"packages"`
}

type BridgeLegacySet struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type TelegramBridgeStatus struct {
	Success       bool                       `json:"success"`
	Enabled       bool                       `json:"enabled"`
	Addresses     mtproto.TelegramCIDRStatus `json:"addresses"`
	Listener      BridgeListenerInfo         `json:"listener"`
	RuleInstalled bool                       `json:"rule_installed"`
	TProxy        BridgeTProxyInfo           `json:"tproxy"`
	SkipSetup     bool                       `json:"skip_setup"`
	QueueMode     string                     `json:"queue_mode"`
	IPv6Enabled   bool                       `json:"ipv6_enabled"`
	LegacySets    []BridgeLegacySet          `json:"legacy_sets"`
	Stats         mtproto.BridgeStats        `json:"stats"`
}

var bridgeListenerFunc func() BridgeListenerInfo

var bridgeTProxyProbe = func(cfg *config.Config, recheck bool) BridgeTProxyInfo {
	ok, checked, missing, packages := tables.TProxyCapability(cfg, recheck)
	return bridgeTProxyResult(ok, checked, missing, packages)
}

var bridgeTProxyCached = func(cfg *config.Config) BridgeTProxyInfo {
	ok, checked, missing, packages := tables.TProxyCapabilityCached(cfg)
	return bridgeTProxyResult(ok, checked, missing, packages)
}

func bridgeTProxyResult(ok, checked bool, missing, packages []string) BridgeTProxyInfo {
	if missing == nil {
		missing = []string{}
	}
	if packages == nil {
		packages = []string{}
	}
	return BridgeTProxyInfo{Checked: checked, Available: ok, Missing: missing, Packages: packages}
}

func SetBridgeListenerFunc(fn func() BridgeListenerInfo) {
	bridgeListenerFunc = fn
}

func bridgeTProxyInfo(cfg *config.Config, recheck, probe bool) BridgeTProxyInfo {
	if recheck || probe {
		return bridgeTProxyProbe(cfg, recheck)
	}
	return bridgeTProxyCached(cfg)
}

func legacyBridgeSets(cfg *config.Config) []BridgeLegacySet {
	out := []BridgeLegacySet{}
	for _, set := range cfg.Sets {
		if set == nil || !set.Routing.Enabled || set.Routing.Mode != config.RoutingModeMTProtoWS {
			continue
		}
		out = append(out, BridgeLegacySet{ID: set.Id, Name: set.Name, Enabled: set.Enabled})
	}
	return out
}

func buildTelegramBridgeStatus(cfg *config.Config, recheck, probe bool) TelegramBridgeStatus {
	enabled := cfg.TelegramBridgeEnabled()
	st := TelegramBridgeStatus{
		Success:     true,
		Enabled:     enabled,
		Addresses:   mtproto.TelegramCIDRState(),
		SkipSetup:   cfg.System.Tables.SkipSetup,
		QueueMode:   cfg.Queue.Mode,
		IPv6Enabled: cfg.Queue.IPv6Enabled,
		LegacySets:  legacyBridgeSets(cfg),
		TProxy:      bridgeTProxyInfo(cfg, recheck, probe && enabled),
	}
	if bridgeListenerFunc != nil {
		st.Listener = bridgeListenerFunc()
	}
	if !cfg.Queue.IPv6Enabled {
		st.Listener.V6Error = ""
	}
	st.RuleInstalled = tables.RoutingSetInstalled(config.TelegramBridgeSetID)
	if b, ok := globalMTProtoBridge.(interface{ Stats() mtproto.BridgeStats }); ok {
		st.Stats = b.Stats()
	}
	return st
}

// @Summary Telegram bridge status
// @Description State of the Settings switch that sends Telegram through the WebSocket bridge: address list, listener, firewall rule, kernel support and relay counters. check=1 re-runs the kernel TPROXY check.
// @Tags MTProto
// @Produce json
// @Param check query string false "1 to re-run the kernel TPROXY check"
// @Success 200 {object} TelegramBridgeStatus
// @Security BearerAuth
// @Router /mtproto/bridge [get]
func (api *API) handleTelegramBridge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cfg := api.getCfg()
	recheck := r.URL.Query().Get("check") == "1"
	st := buildTelegramBridgeStatus(cfg, recheck, true)
	if recheck && st.Enabled && st.TProxy.Available && st.Listener.Running && !st.RuleInstalled {
		tables.RoutingResyncLatest()
	}
	sendResponse(w, st)
}

// @Summary Refresh the Telegram bridge address list
// @Description Downloads Telegram's address ranges again and returns the bridge status.
// @Tags MTProto
// @Produce json
// @Success 200 {object} TelegramBridgeStatus
// @Security BearerAuth
// @Router /mtproto/bridge/refresh [post]
func (api *API) handleTelegramBridgeRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cfg := api.getCfg()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 45*time.Second)
	defer cancel()
	if err := mtproto.RefreshTelegramCIDRsNow(ctx, cfg); err != nil {
		log.Warnf("Telegram bridge: manual address list refresh failed: %v", err)
	}
	sendResponse(w, buildTelegramBridgeStatus(api.getCfg(), false, true))
}
