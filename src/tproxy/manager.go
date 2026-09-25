package tproxy

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/socks5"
)

type Manager struct {
	mu            sync.Mutex
	listeners     map[string]*Listener
	resolver      NameSource
	mtprotoBridge MTProtoBridge
	ctx           context.Context
	cancel        context.CancelFunc

	startErr   map[string]string
	lastCfg    *config.Config
	retryTimer *time.Timer
	retryDelay time.Duration
	onBridge   func(v4, v6, retried bool)
}

const (
	listenerRetryBase = 5 * time.Second
	listenerRetryMax  = 5 * time.Minute
)

type ListenerStatus struct {
	Running bool
	Port    int
	V4      bool
	V6      bool
	Active  int64
	Error   string
	V6Error string
}

func (m *Manager) SetTelegramBridgeHook(fn func(v4, v6, retried bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onBridge = fn
}

func (m *Manager) ListenerStatus(setID string, port int) ListenerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := ListenerStatus{Port: port, Error: m.startErr[setID]}
	l, ok := m.listeners[setID]
	if !ok {
		return st
	}
	st.Port = l.Port
	st.V4 = l.lnV4 != nil
	st.V6 = l.lnV6 != nil
	st.Running = st.V4 || st.V6
	st.Active = l.Active()
	if !st.V6 {
		st.V6Error = l.v6Err
	}
	return st
}

func (m *Manager) SetMTProtoBridge(b MTProtoBridge) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mtprotoBridge = b
	for _, l := range m.listeners {
		l.Bridge = b
	}
}

func NewManager(resolver NameSource) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		listeners: make(map[string]*Listener),
		startErr:  make(map[string]string),
		resolver:  resolver,
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (m *Manager) SetResolver(r NameSource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolver = r
	for _, l := range m.listeners {
		l.Names = r
	}
	m.syncNamesWantedLocked()
}

func (m *Manager) syncNamesWantedLocked() {
	if m.resolver == nil {
		return
	}
	wanted := false
	for _, l := range m.listeners {
		if l.UseDomain && !l.MTProtoWS {
			wanted = true
			break
		}
	}
	m.resolver.WantNames(wanted)
}

func (m *Manager) SyncConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncLocked(cfg, false)
}

func (m *Manager) retryFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retryTimer = nil
	if m.lastCfg == nil || m.ctx.Err() != nil {
		return
	}
	m.syncLocked(m.lastCfg, true)
}

func (m *Manager) scheduleRetryLocked() {
	if m.retryTimer != nil {
		return
	}
	if m.retryDelay <= 0 {
		m.retryDelay = listenerRetryBase
	} else if m.retryDelay < listenerRetryMax {
		m.retryDelay *= 2
		if m.retryDelay > listenerRetryMax {
			m.retryDelay = listenerRetryMax
		}
	}
	m.retryTimer = time.AfterFunc(m.retryDelay, m.retryFailed)
}

func (m *Manager) syncLocked(cfg *config.Config, retried bool) {
	m.lastCfg = cfg
	bypassMark := proxyBypassMark(cfg)

	routingSets := cfg.RoutingSets()
	desired := make(map[string]*config.SetConfig, len(routingSets))
	for _, set := range routingSets {
		if set == nil || !set.Enabled || !set.RoutingDivertsPackets() {
			continue
		}
		if !config.RoutingUsesTProxy(set.Routing.Mode) {
			continue
		}
		desired[set.Id] = set
	}

	for id, l := range m.listeners {
		set, keep := desired[id]
		if !keep {
			log.Infof("tproxy: stopping listener for removed set %q", l.SetName)
			_ = l.Stop()
			delete(m.listeners, id)
			continue
		}
		mark := effectiveMark(set)
		port := PortFor(mark)
		desiredHost := set.Routing.Upstream.Host
		if desiredHost == "" {
			desiredHost = "127.0.0.1"
		}
		isMTWS := set.Routing.Mode == config.RoutingModeMTProtoWS
		if l.Port != port ||
			l.MTProtoWS != isMTWS ||
			l.Upstream.Host != desiredHost ||
			l.Upstream.Port != set.Routing.Upstream.Port ||
			l.Upstream.Username != set.Routing.Upstream.Username ||
			l.Upstream.Password != set.Routing.Upstream.Password ||
			l.Upstream.BypassMark != bypassMark ||
			l.UseDomain != set.Routing.Upstream.UseDomain ||
			l.UDP != set.Routing.Upstream.UDP ||
			l.FailOpen != set.Routing.Upstream.FailOpen {
			log.Infof("tproxy: restarting listener for set %q (config changed)", set.Name)
			_ = l.Stop()
			delete(m.listeners, id)
			continue
		}
		l.set.Store(set)
	}

	for id, set := range desired {
		if _, ok := m.listeners[id]; ok {
			continue
		}
		mark := effectiveMark(set)
		port := PortFor(mark)
		host := set.Routing.Upstream.Host
		if host == "" {
			host = "127.0.0.1"
		}
		l := &Listener{
			SetID:   set.Id,
			SetName: set.Name,
			Port:    port,
			Upstream: socks5.ClientConfig{
				Host:       host,
				Port:       set.Routing.Upstream.Port,
				Username:   set.Routing.Upstream.Username,
				Password:   set.Routing.Upstream.Password,
				Timeout:    10 * time.Second,
				BypassMark: bypassMark,
			},
			UseDomain: set.Routing.Upstream.UseDomain,
			UDP:       set.Routing.Upstream.UDP,
			FailOpen:  set.Routing.Upstream.FailOpen,
			Names:     m.resolver,
			MTProtoWS: set.Routing.Mode == config.RoutingModeMTProtoWS,
			Bridge:    m.mtprotoBridge,
			guard:     newLoopGuard(host, set.Routing.Upstream.Port),
		}
		l.set.Store(set)
		if err := l.Start(m.ctx); err != nil {
			msg := err.Error()
			if m.startErr[id] != msg {
				log.Errorf("tproxy: failed to start listener for set %q: %v, it will be retried", set.Name, err)
			} else {
				log.Tracef("tproxy: listener for set %q still cannot start: %v", set.Name, err)
			}
			m.startErr[id] = msg
			continue
		}
		if _, failed := m.startErr[id]; failed {
			log.Infof("tproxy: listener for set %q started on port %d after an earlier failure", set.Name, port)
		}
		delete(m.startErr, id)
		m.listeners[id] = l
	}
	for id := range m.startErr {
		if _, ok := desired[id]; !ok {
			delete(m.startErr, id)
		}
	}
	if len(m.startErr) > 0 {
		m.scheduleRetryLocked()
	} else {
		m.retryDelay = 0
		if m.retryTimer != nil {
			m.retryTimer.Stop()
			m.retryTimer = nil
		}
	}
	m.syncNamesWantedLocked()
	m.reportBridgeLocked(retried)
}

func (m *Manager) reportBridgeLocked(retried bool) {
	if m.onBridge == nil {
		return
	}
	v4, v6 := false, false
	if l, ok := m.listeners[config.TelegramBridgeSetID]; ok {
		v4 = l.lnV4 != nil
		v6 = l.lnV6 != nil
	}
	m.onBridge(v4, v6, retried)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.retryTimer != nil {
		m.retryTimer.Stop()
		m.retryTimer = nil
	}
	for id, l := range m.listeners {
		_ = l.Stop()
		delete(m.listeners, id)
	}
	m.syncNamesWantedLocked()
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Manager) UpstreamHealth() []UpstreamHealth {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]UpstreamHealth, 0, len(m.listeners))
	for _, l := range m.listeners {
		if l.MTProtoWS {
			continue
		}
		out = append(out, l.Health())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SetName < out[j].SetName })
	return out
}

func (m *Manager) PortForSet(set *config.SetConfig) int {
	if set == nil {
		return 0
	}
	return PortFor(effectiveMark(set))
}

func effectiveMark(set *config.SetConfig) uint32 {
	if set == nil {
		return 0
	}
	return MarkForSet(set.Id, set.Routing.FWMark)
}

// proxyBypassMark returns the SO_MARK value the listener uses on its outbound
// SOCKS5 dial and on a fail-open direct dial, so those packets bypass b4's
// proxy-mode OUTPUT mark rule and don't loop back into TPROXY. It mirrors
// tables.SelfDialMark, which the routing chains return on and nothing else does
// - the queue mark would have carried them past b4's own DPI bypass as well,
// which is the one place a fail-open dial needs it most.
func proxyBypassMark(cfg *config.Config) uint32 {
	if cfg == nil {
		return config.SelfDialMark
	}
	return cfg.SelfDialMark()
}
