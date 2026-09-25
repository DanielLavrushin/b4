package tables

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var (
	telegramBridgeReported atomic.Bool
	telegramBridgeV4       atomic.Bool
	telegramBridgeV6       atomic.Bool
	telegramBridgeNoTProxy atomic.Bool
)

const tproxyNegativeRecheck = 5 * time.Minute

type tproxyCapability struct {
	usable   bool
	probed   bool
	missing  []string
	packages []string
	at       time.Time
}

var (
	tproxyCapMu    sync.Mutex
	tproxyCapCache = map[string]tproxyCapability{}
)

var tproxyCapabilityProbe = func(backend string) (missing []string, probed bool) {
	switch backend {
	case backendNFTables:
		return tproxyMissingNft()
	case backendIPTablesLegacy:
		return tproxyMissingIpt(true)
	default:
		return tproxyMissingIpt(false)
	}
}

func tproxyCapabilityFor(backend string, recheck bool) tproxyCapability {
	tproxyCapMu.Lock()
	defer tproxyCapMu.Unlock()
	if c, ok := tproxyCapCache[backend]; ok && !recheck && (c.usable || time.Since(c.at) < tproxyNegativeRecheck) {
		return c
	}
	missing, probed := tproxyCapabilityProbe(backend)
	c := tproxyCapability{
		usable:   !probed || len(missing) == 0,
		probed:   probed,
		missing:  missing,
		packages: kmodPkgsFor(missing),
		at:       time.Now(),
	}
	tproxyCapCache[backend] = c
	return c
}

func routeBackendCapKey(be routeBackend) string {
	if be.name() == backendNFTables {
		return backendNFTables
	}
	if isLegacyIptBackend(be) {
		return backendIPTablesLegacy
	}
	return backendIPTables
}

func TProxyCapability(cfg *config.Config, recheck bool) (available, checked bool, missing, packages []string) {
	if recheck {
		tproxyCapMu.Lock()
		tproxyCapCache = map[string]tproxyCapability{}
		tproxyCapMu.Unlock()
	}
	c := tproxyCapabilityFor(detectFirewallBackend(cfg), false)
	return c.probed && c.usable, c.probed, c.missing, c.packages
}

func TProxyCapabilityCached(cfg *config.Config) (available, checked bool, missing, packages []string) {
	tproxyCapMu.Lock()
	defer tproxyCapMu.Unlock()
	c, ok := tproxyCapCache[detectFirewallBackend(cfg)]
	if !ok {
		return false, false, nil, nil
	}
	return c.probed && c.usable, c.probed, c.missing, c.packages
}

func telegramBridgeTProxyUsable(be routeBackend) bool {
	c := tproxyCapabilityFor(routeBackendCapKey(be), false)
	if c.usable {
		telegramBridgeNoTProxy.Store(false)
		return true
	}
	if !telegramBridgeNoTProxy.Swap(true) {
		log.Errorf("Telegram bridge: the firewall does not support %s, so no Telegram traffic is diverted to the bridge. %s",
			strings.Join(c.missing, ", "), kmodMissingHint(c.missing))
	}
	return false
}

func SetTelegramBridgeListener(v4, v6 bool) bool {
	wasKnown := telegramBridgeReported.Swap(true)
	oldV4 := telegramBridgeV4.Swap(v4)
	oldV6 := telegramBridgeV6.Swap(v6)
	return !wasKnown || oldV4 != v4 || oldV6 != v6
}

func telegramBridgeListenerFamilies() (bool, bool) {
	if !telegramBridgeReported.Load() {
		return true, true
	}
	return telegramBridgeV4.Load(), telegramBridgeV6.Load()
}

func telegramBridgeListenerUp() bool {
	v4, v6 := telegramBridgeListenerFamilies()
	return v4 || v6
}

func RoutingResyncLatest() {
	go func() {
		routePhaseMu.Lock()
		defer routePhaseMu.Unlock()
		if pending := routingSyncRetryConfig(); pending != nil && pending != routingSyncedConfig() {
			return
		}
		if cfg := routingSyncedConfig(); cfg != nil {
			routingSyncConfig(cfg)
		}
	}()
}

func RoutingSetInstalled(setID string) bool {
	routeMu.Lock()
	defer routeMu.Unlock()
	_, ok := routeRuleCache[setID]
	return ok
}

func telegramBridgeMarkMatch() string {
	return fmt.Sprintf("0x%x/0x%x", config.TelegramBridgeMark, config.PerSetRouteMarkBits)
}

func routeProxySourceScoped(cfg *config.Config, set *config.SetConfig) bool {
	return routeSetIsSourceScoped(set) || routeSetDeviceGate(cfg, set).isWhitelist()
}

func selfDialNoDPIMarkMatch() string {
	return fmt.Sprintf("0x%x/0x%x", config.SelfDialNoDPIBit, config.SelfDialNoDPIBit)
}
