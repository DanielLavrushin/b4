package tables

import (
	"reflect"
	"sort"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tproxy"
)

func bridgeResetReadiness(t *testing.T) {
	t.Helper()
	known, v4, v6 := telegramBridgeReported.Load(), telegramBridgeV4.Load(), telegramBridgeV6.Load()
	t.Cleanup(func() {
		telegramBridgeReported.Store(known)
		telegramBridgeV4.Store(v4)
		telegramBridgeV6.Store(v6)
	})
	telegramBridgeReported.Store(false)
	telegramBridgeV4.Store(false)
	telegramBridgeV6.Store(false)

	probe := tproxyCapabilityProbe
	tproxyCapMu.Lock()
	cache := tproxyCapCache
	tproxyCapCache = map[string]tproxyCapability{}
	tproxyCapMu.Unlock()
	t.Cleanup(func() {
		tproxyCapabilityProbe = probe
		tproxyCapMu.Lock()
		tproxyCapCache = cache
		tproxyCapMu.Unlock()
		telegramBridgeNoTProxy.Store(false)
	})
	tproxyCapabilityProbe = func(string) ([]string, bool) { return nil, true }
}

func bridgeTestConfig(ipv6 bool) *config.Config {
	cfg := familyTestConfig(true, ipv6)
	cfg.System.MTProto.Bridge.Enabled = true
	return cfg
}

func bridgeAddedFamily(added []staticCall, name string) []string {
	var out []string
	for _, c := range added {
		if c.set == name {
			out = append(out, c.ips...)
		}
	}
	sort.Strings(out)
	return out
}

func builtinFamily(v6 bool) []string {
	var out []string
	for _, p := range config.CurrentTelegramCIDRs().All {
		isV6 := false
		for _, ch := range p {
			if ch == ':' {
				isV6 = true
				break
			}
		}
		if isV6 == v6 {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func TestRoutingSyncInstallsTheTelegramBridgeWithoutASet(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	staticResetGlobals(t)
	bridgeResetReadiness(t)

	cfg := bridgeTestConfig(false)
	var added, deleted []staticCall
	routeEngine = staticSyncBackend(&added, &deleted)
	RoutingSyncConfig(cfg)

	st, ok := routeRuleCache[config.TelegramBridgeSetID]
	if !ok {
		t.Fatal("the bridge switch installed no routing state")
	}
	if st.mode != config.RoutingModeMTProtoWS || st.mark != config.TelegramBridgeMark {
		t.Errorf("bridge state mode=%s mark=0x%x, want mtproto-ws with the pinned mark", st.mode, st.mark)
	}
	if want := tproxy.PortFor(config.TelegramBridgeMark); st.tproxyPort != want {
		t.Errorf("bridge diverts to port %d, the listener binds %d", st.tproxyPort, want)
	}
	setV4, _ := routeBuildSetNames(config.TelegramBridgeSetID)
	if got, want := bridgeAddedFamily(added, setV4), builtinFamily(false); !reflect.DeepEqual(got, want) {
		t.Errorf("bridge v4 set got %v, want %v", got, want)
	}
	if len(cfg.Sets) != 0 {
		t.Error("the sync must not add the bridge to the set list")
	}

	cfg.System.MTProto.Bridge.Enabled = false
	RoutingSyncConfig(cfg)
	if _, ok := routeRuleCache[config.TelegramBridgeSetID]; ok {
		t.Error("turning the switch off left the bridge installed")
	}
}

func TestRoutingSyncWaitsForTheBridgeListener(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	staticResetGlobals(t)
	bridgeResetReadiness(t)

	cfg := bridgeTestConfig(true)
	var added, deleted []staticCall
	routeEngine = staticSyncBackend(&added, &deleted)
	setV4, setV6 := routeBuildSetNames(config.TelegramBridgeSetID)

	SetTelegramBridgeListener(false, false)
	RoutingSyncConfig(cfg)
	if _, ok := routeRuleCache[config.TelegramBridgeSetID]; ok {
		t.Fatal("rules were installed while the bridge listener is down, Telegram would be diverted to a closed port")
	}

	SetTelegramBridgeListener(true, false)
	RoutingSyncConfig(cfg)
	if _, ok := routeRuleCache[config.TelegramBridgeSetID]; !ok {
		t.Fatal("the bridge was not installed once its v4 listener came up")
	}
	if got := bridgeAddedFamily(added, setV6); len(got) != 0 {
		t.Errorf("v6 ranges were diverted while only the v4 listener runs: %v", got)
	}
	if got, want := bridgeAddedFamily(added, setV4), builtinFamily(false); !reflect.DeepEqual(got, want) {
		t.Errorf("v4 ranges got %v, want %v", got, want)
	}

	added = nil
	SetTelegramBridgeListener(true, true)
	RoutingSyncConfig(cfg)
	if got, want := bridgeAddedFamily(added, setV6), builtinFamily(true); !reflect.DeepEqual(got, want) {
		t.Errorf("v6 ranges after the v6 listener came up got %v, want %v", got, want)
	}
}

func TestTelegramBridgeComesFirstAmongUnscopedRoutingSets(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	staticResetGlobals(t)
	bridgeResetReadiness(t)

	user := staticTestSet(config.RoutingModeProxy, []string{"149.154.160.0/20"})
	cfg := bridgeTestConfig(false)
	cfg.Sets = []*config.SetConfig{user}
	var added, deleted []staticCall
	routeEngine = staticSyncBackend(&added, &deleted)
	RoutingSyncConfig(cfg)

	ordered := routeOrderedRoutingSets(cfg)
	if len(ordered) != 2 || ordered[0].Id != config.TelegramBridgeSetID || ordered[1].Id != user.Id {
		ids := make([]string, 0, len(ordered))
		for _, s := range ordered {
			ids = append(ids, s.Id)
		}
		t.Errorf("jump order %v, want the bridge before the user set", ids)
	}
}

func TestRoutingSyncSkipsTheBridgeWithoutTProxy(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	staticResetGlobals(t)
	bridgeResetReadiness(t)
	probes := 0
	tproxyCapabilityProbe = func(string) ([]string, bool) {
		probes++
		return []string{"nft_tproxy"}, true
	}

	cfg := bridgeTestConfig(false)
	var added, deleted []staticCall
	routeEngine = staticSyncBackend(&added, &deleted)
	RoutingSyncConfig(cfg)
	RoutingSyncConfig(cfg)
	if _, ok := routeRuleCache[config.TelegramBridgeSetID]; ok {
		t.Fatal("the bridge was installed on a kernel without TPROXY, the router's own Telegram would be marked into a closed path")
	}
	if probes != 1 {
		t.Errorf("a missing module must be probed once and cached, probed %d times", probes)
	}

	tproxyCapabilityProbe = func(string) ([]string, bool) { return nil, true }
	if ok, checked, _, _ := TProxyCapability(cfg, true); !ok || !checked {
		t.Fatalf("a forced recheck must see the module, got available=%v checked=%v", ok, checked)
	}
	RoutingSyncConfig(cfg)
	if _, ok := routeRuleCache[config.TelegramBridgeSetID]; !ok {
		t.Error("the bridge was not installed after the recheck found TPROXY")
	}
}

func TestProxySetsLeaveRouterTrafficAloneUnderAnAllowList(t *testing.T) {
	bridgeResetReadiness(t)
	cfg := bridgeTestConfig(false)
	set := cfg.TelegramBridgeSet()
	if routeProxySourceScoped(cfg, set) {
		t.Fatal("an unscoped bridge with device filtering off must include the router's own traffic")
	}
	cfg.Queue.Devices.Enabled = true
	cfg.Queue.Devices.Devices = []config.Device{{MAC: "AA:BB:CC:DD:EE:FF", Selected: true}}
	if !routeProxySourceScoped(cfg, set) {
		t.Error("with an allow list the router is not a selected device, its traffic must keep the normal route")
	}
	if st := buildRouteState(cfg, set); !st.srcScoped || routeWantsOutputJump(st) {
		t.Errorf("route state srcScoped=%v wantsOutput=%v under an allow list", st.srcScoped, routeWantsOutputJump(st))
	}
}
