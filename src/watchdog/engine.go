package watchdog

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/sni"
)

type EngineView interface {
	Owner(host string) *config.SetConfig
	EscalatedTo(host string) string
	ClearEscalation(host string)
}

type poolEngine struct {
	pool   *nfq.Pool
	cfgPtr *atomic.Pointer[config.Config]

	mu          sync.Mutex
	fallbackCfg *config.Config
	fallback    *sni.SuffixSet
}

func NewEngineView(pool *nfq.Pool, cfgPtr *atomic.Pointer[config.Config]) EngineView {
	return &poolEngine{pool: pool, cfgPtr: cfgPtr}
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func (e *poolEngine) matcher() *sni.SuffixSet {
	if e.pool != nil {
		if m := e.pool.GetMatcher(); m != nil {
			return m
		}
	}
	if e.cfgPtr == nil {
		return nil
	}
	cfg := e.cfgPtr.Load()
	if cfg == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fallback == nil || e.fallbackCfg != cfg {
		e.fallback = sni.NewSuffixSet(cfg.Sets)
		e.fallbackCfg = cfg
	}
	return e.fallback
}

func (e *poolEngine) Owner(host string) *config.SetConfig {
	host = normalizeHost(host)
	if host == "" {
		return nil
	}
	m := e.matcher()
	if m == nil {
		return nil
	}
	if matched, set := m.MatchSNIWithSourceTLS(host, "", 0, 0); matched {
		return set
	}
	return nil
}

func (e *poolEngine) EscalatedTo(host string) string {
	if e.pool == nil {
		return ""
	}
	host = normalizeHost(host)
	for _, esc := range e.pool.GetEscalations() {
		if normalizeHost(esc.Host) == host {
			return esc.ToSet
		}
	}
	return ""
}

func (e *poolEngine) ClearEscalation(host string) {
	if e.pool == nil {
		return
	}
	e.pool.ClearEscalation(host)
	if lower := normalizeHost(host); lower != host {
		e.pool.ClearEscalation(lower)
	}
}
