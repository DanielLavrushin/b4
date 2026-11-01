package discovery

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
)

type HostSource string

const HostFromPage HostSource = "page"

type HostContext string

const (
	HostContextRedirect   HostContext = "redirect"
	HostContextBase       HostContext = "base"
	HostContextStylesheet HostContext = "stylesheet"
	HostContextScript     HostContext = "script"
	HostContextImage      HostContext = "image"
	HostContextMedia      HostContext = "media"
	HostContextFrame      HostContext = "frame"
	HostContextCSS        HostContext = "css"
	HostContextPreload    HostContext = "preload"
	HostContextIcon       HostContext = "icon"
	HostContextPreconnect HostContext = "preconnect"
	HostContextInline     HostContext = "inline"
)

var hostContextOrder = []HostContext{
	HostContextStylesheet,
	HostContextScript,
	HostContextImage,
	HostContextMedia,
	HostContextCSS,
	HostContextPreload,
	HostContextRedirect,
	HostContextBase,
	HostContextFrame,
	HostContextIcon,
	HostContextPreconnect,
	HostContextInline,
}

func hostContextRank(c HostContext) int {
	for i, known := range hostContextOrder {
		if known == c {
			return i
		}
	}
	return len(hostContextOrder)
}

type HostVerdict string

const (
	HostNeeded       HostVerdict = "needed"
	HostNotFixed     HostVerdict = "not_fixed"
	HostUnreachable  HostVerdict = "unreachable"
	HostLoadsWithout HostVerdict = "loads_without"
	HostUnresolved   HostVerdict = "unresolved"
	HostCovered      HostVerdict = "covered"
	HostUntested     HostVerdict = "untested"
)

var hostVerdictOrder = []HostVerdict{
	HostNeeded,
	HostNotFixed,
	HostUnreachable,
	HostLoadsWithout,
	HostUnresolved,
	HostCovered,
	HostUntested,
}

func hostVerdictRank(v HostVerdict) int {
	for i, known := range hostVerdictOrder {
		if known == v {
			return i
		}
	}
	return len(hostVerdictOrder)
}

type HostCandidate struct {
	Host     string      `json:"host"`
	URLs     []string    `json:"-"`
	Source   HostSource  `json:"source,omitempty"`
	Context  HostContext `json:"context,omitempty"`
	Blocking bool        `json:"blocking,omitempty"`
	Tracker  bool        `json:"tracker,omitempty"`
}

type HostResult struct {
	HostCandidate
	Verdict   HostVerdict `json:"verdict"`
	Service   string      `json:"service,omitempty"`
	URL       string      `json:"url,omitempty"`
	BytesRead int64       `json:"bytes_read,omitempty"`
	Small     bool        `json:"small,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type HostCheck struct {
	Preset    string       `json:"preset"`
	Hosts     []HostResult `json:"hosts,omitempty"`
	Skipped   string       `json:"skipped,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`
}

type HostTarget struct {
	Preset     string
	Set        *config.SetConfig
	Domains    []string
	Origin     string
	Candidates []HostCandidate
	base       *config.Config
}

const (
	maxHostCandidates = 40
	hostWithTries     = 2
	maxHostWorkers    = 6
)

func hostWorkers() int {
	return min(maxHostWorkers, 2*runtime.NumCPU())
}

func (ds *DiscoverySuite) CheckHosts(ctx context.Context, target HostTarget) []HostResult {
	run := ds.newHostRun([]*HostTarget{&target})
	run.withoutPass(ctx)
	run.withPass(ctx, 0)
	return run.results(0)
}

type withoutOutcome uint8

const (
	withoutPending withoutOutcome = iota
	withoutLoads
	withoutFails
	withoutUnresolved
	withoutUntested
)

type sharedHost struct {
	host     string
	urls     []string
	origin   string
	outcome  withoutOutcome
	url      string
	bytes    int64
	wire     int64
	small    bool
	reserved bool
	err      string
}

type hostEntry struct {
	res    HostResult
	shared *sharedHost
	done   bool
}

type hostRunTarget struct {
	*HostTarget
	entries []*hostEntry
}

type hostRun struct {
	ds      *DiscoverySuite
	targets []*hostRunTarget
	hosts   []*sharedHost
	byName  map[string]*sharedHost
}

func (ds *DiscoverySuite) newHostRun(targets []*HostTarget) *hostRun {
	run := &hostRun{ds: ds, byName: map[string]*sharedHost{}}
	covering := ds.coveringRunDomains(targets)
	live := ds.liveSetCover()
	total := 0
	for _, t := range targets {
		if t.base == nil && t.Set != nil {
			t.base = ds.deployedConfig(t.Set, t.Domains, nil)
		}
		own := coverSet{}
		if t.base != nil && len(t.base.Sets) > 0 {
			own = newCoverSet(t.base.Sets[0].Targets.DomainsToMatch)
		}
		rt := &hostRunTarget{HostTarget: t}
		seen := map[string]bool{}
		var probe []*hostEntry
		for _, c := range t.Candidates {
			host, ok := normalizeHost(c.Host)
			if !ok || seen[host] {
				continue
			}
			seen[host] = true
			c.Host = host
			c.Tracker = c.Tracker || isTracker(host)
			e := &hostEntry{res: HostResult{HostCandidate: c, Service: hostService(host)}}
			rt.entries = append(rt.entries, e)
			switch {
			case own.covers(host), live.covers(host), covering.covers(host):
				e.res.Verdict = HostCovered
				e.done = true
			case t.base == nil:
				e.res.Verdict = HostUntested
				e.res.Error = "the strategy could not be rebuilt as it would be deployed"
				e.done = true
			default:
				probe = append(probe, e)
			}
		}
		sort.SliceStable(probe, func(i, j int) bool {
			a, b := probe[i].res, probe[j].res
			if a.Blocking != b.Blocking {
				return a.Blocking
			}
			return hostContextRank(a.Context) < hostContextRank(b.Context)
		})
		for i, e := range probe {
			if i >= maxHostCandidates {
				e.res.Verdict = HostUntested
				e.res.Error = fmt.Sprintf("over the limit of %d hosts", maxHostCandidates)
				e.done = true
				continue
			}
			e.shared = run.share(e.res.HostCandidate, t.Origin)
			total++
		}
		run.targets = append(run.targets, rt)
	}
	ds.addHostsTotal(total + len(run.hosts))
	return run
}

func (run *hostRun) share(c HostCandidate, origin string) *sharedHost {
	if h := run.byName[c.Host]; h != nil {
		return h
	}
	urls := append([]string(nil), c.URLs...)
	if len(urls) == 0 {
		urls = []string{"https://" + c.Host + "/"}
	}
	h := &sharedHost{host: c.Host, urls: urls, origin: origin}
	run.byName[c.Host] = h
	run.hosts = append(run.hosts, h)
	return h
}

func (run *hostRun) withoutPass(ctx context.Context) {
	ds := run.ds
	if len(run.hosts) == 0 {
		return
	}
	if ctx.Err() != nil {
		run.abandon()
		return
	}
	log.DiscoveryLogf("Page hosts: testing %d hosts without b4", len(run.hosts))
	if err := ds.applyHostConfig(ds.noBypassConfig()); err != nil {
		log.DiscoveryLogf("Page hosts: could not turn b4 off for the test: %v", err)
		run.abandon()
		return
	}
	runHostWorkers(len(run.hosts), func(i int) {
		run.probeWithout(ctx, run.hosts[i])
		ds.hostStepDone()
	})
}

func (run *hostRun) abandon() {
	for _, h := range run.hosts {
		if h.outcome == withoutPending {
			h.outcome = withoutUntested
			run.ds.hostStepDone()
		}
	}
}

func (run *hostRun) probeWithout(ctx context.Context, h *sharedHost) {
	ds := run.ds
	for _, u := range h.urls {
		p := ds.hostRequest(ctx, u, h.origin, false)
		if !p.ok && !p.aborted {
			p = ds.hostRequest(ctx, u, h.origin, false)
		}
		if p.aborted {
			h.outcome = withoutUntested
			return
		}
		if !p.ok {
			h.url, h.bytes, h.err = u, p.bytes, p.err
			if p.failure == hostFailName {
				h.outcome, h.reserved = withoutUnresolved, p.reserved
				log.DiscoveryLogf("  %s: does not resolve without b4 (%s)", h.host, p.err)
				return
			}
			h.outcome = withoutFails
			log.DiscoveryLogf("  %s: blocked without b4 (%s)", h.host, p.err)
			return
		}
		if p.bytes >= h.bytes {
			h.url, h.bytes = u, p.bytes
		}
		h.wire = max(h.wire, p.wire)
		if !p.small {
			h.outcome = withoutLoads
			log.DiscoveryLogf("  %s: loads without b4", h.host)
			return
		}
	}
	h.outcome = withoutLoads
	h.small = true
	log.DiscoveryLogf("  %s: loads without b4, but every sample was small (%d bytes on the wire at most), so a size-dependent block would not show", h.host, h.wire)
}

func (run *hostRun) withPass(ctx context.Context, index int) {
	ds := run.ds
	t := run.targets[index]
	var failing []*hostEntry
	for _, e := range t.entries {
		if e.done {
			continue
		}
		h := e.shared
		switch h.outcome {
		case withoutFails:
			failing = append(failing, e)
			continue
		case withoutLoads:
			e.res.Verdict = HostLoadsWithout
			e.res.URL, e.res.BytesRead, e.res.Small = h.url, h.bytes, h.small
		case withoutUnresolved:
			if !h.reserved && redirectsDNS(t.base) {
				failing = append(failing, e)
				continue
			}
			e.res.Verdict = HostUnresolved
			e.res.Error = h.err
		default:
			e.res.Verdict = HostUntested
		}
		e.done = true
		ds.hostStepDone()
	}
	if len(failing) == 0 {
		return
	}

	hosts := make([]string, 0, len(failing))
	for _, e := range failing {
		hosts = append(hosts, e.res.Host)
	}
	if ctx.Err() == nil {
		log.DiscoveryLogf("Page hosts: testing %d hosts with '%s'", len(hosts), t.Preset)
		if err := ds.applyHostConfig(withHosts(t.base, hosts)); err != nil {
			log.DiscoveryLogf("Page hosts: could not apply '%s' with the hosts: %v", t.Preset, err)
			for _, e := range failing {
				e.res.Error = "the strategy could not be applied with the host"
			}
		} else {
			runHostWorkers(len(failing), func(i int) {
				run.probeWith(ctx, t, failing[i])
			})
		}
	}
	for _, e := range failing {
		if !e.done {
			e.res.Verdict = HostUntested
			e.done = true
			ds.hostStepDone()
		}
	}
}

func (run *hostRun) probeWith(ctx context.Context, t *hostRunTarget, e *hostEntry) {
	ds := run.ds
	h := e.shared
	var p hostProbe
	for try := 0; try < hostWithTries; try++ {
		p = ds.hostRequest(ctx, h.url, t.Origin, true)
		if p.aborted || !p.ok {
			break
		}
	}
	if p.aborted {
		return
	}
	e.res.URL, e.res.BytesRead = h.url, p.bytes
	switch {
	case p.ok:
		e.res.Verdict = HostNeeded
		log.DiscoveryLogf("  %s: loads with '%s'", h.host, t.Preset)
	case p.failure == hostFailBlocked:
		e.res.Verdict = HostNotFixed
		e.res.Error = p.err
		log.DiscoveryLogf("  %s: still blocked with '%s' (%s)", h.host, t.Preset, p.err)
	case p.failure == hostFailName:
		e.res.Verdict = HostUnresolved
		e.res.Error = p.err
		log.DiscoveryLogf("  %s: does not resolve with '%s' (%s)", h.host, t.Preset, p.err)
	default:
		e.res.Verdict = HostUnreachable
		e.res.Error = p.err
		log.DiscoveryLogf("  %s: unreachable with '%s' (%s)", h.host, t.Preset, p.err)
	}
	e.done = true
	ds.hostStepDone()
}

func (run *hostRun) results(index int) []HostResult {
	t := run.targets[index]
	out := make([]HostResult, 0, len(t.entries))
	for _, e := range t.entries {
		if e.res.Verdict == "" {
			e.res.Verdict = HostUntested
		}
		out = append(out, e.res)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return hostVerdictRank(out[i].Verdict) < hostVerdictRank(out[j].Verdict)
	})
	return out
}

func runHostWorkers(n int, work func(i int)) {
	workers := min(hostWorkers(), n)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				work(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func (ds *DiscoverySuite) hostRequest(ctx context.Context, rawURL, origin string, with bool) hostProbe {
	if ctx.Err() != nil {
		return hostProbe{aborted: true}
	}
	if ds.hostProbeFn != nil {
		return ds.hostProbeFn(ctx, rawURL, origin, with)
	}
	return ds.probeHost(ctx, rawURL, origin, with)
}

func (ds *DiscoverySuite) applyHostConfig(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("no configuration to apply")
	}
	if ds.hostConfigFn != nil {
		return ds.hostConfigFn(cfg)
	}
	if err := ds.pool.UpdateConfig(cfg); err != nil {
		return err
	}
	time.Sleep(time.Duration(ds.cfg.System.Checker.ConfigPropagateMs) * time.Millisecond)
	return nil
}

func (ds *DiscoverySuite) noBypassConfig() *config.Config {
	return ds.buildTestConfigMulti(GetPhase1Presets()[0])
}

func redirectsDNS(cfg *config.Config) bool {
	if cfg == nil || len(cfg.Sets) == 0 {
		return false
	}
	dns := cfg.Sets[0].DNS
	return dns.Enabled && (dns.TargetDNS != "" || dns.DoHURL != "")
}

func (ds *DiscoverySuite) deployedConfig(stored *config.SetConfig, domains, hosts []string) *config.Config {
	return withHosts(ds.confirmConfig(stored, domains), hosts)
}

func withHosts(base *config.Config, hosts []string) *config.Config {
	if base == nil || len(base.Sets) == 0 || len(hosts) == 0 {
		return base
	}
	set := *base.Sets[0]
	set.Targets.SNIDomains = append(append([]string(nil), set.Targets.SNIDomains...), hosts...)
	set.Targets.DomainsToMatch = append(append([]string(nil), set.Targets.DomainsToMatch...), hosts...)
	return &config.Config{
		ConfigPath: base.ConfigPath,
		Queue:      base.Queue,
		System:     base.System,
		Sets:       []*config.SetConfig{&set},
	}
}

func (ds *DiscoverySuite) coveringRunDomains(targets []*HostTarget) coverSet {
	var domains []string
	for _, t := range targets {
		domains = append(domains, t.Domains...)
	}
	ds.CheckSuite.mu.RLock()
	for _, g := range ds.StrategyGroups {
		if g.Set != nil && ds.confirmedOn(g.WinnerPreset, g.Domains) {
			domains = append(domains, g.Domains...)
		}
	}
	ds.CheckSuite.mu.RUnlock()
	return newCoverSet(domains)
}

func (ds *DiscoverySuite) liveSetCover() coverSet {
	if !ds.setMode() || ds.cfg == nil {
		return coverSet{}
	}
	set := ds.cfg.GetSetById(ds.SetId)
	if set == nil {
		return coverSet{}
	}
	return newCoverSet(append(append([]string(nil), set.Targets.SNIDomains...), set.Targets.DomainsToMatch...))
}

type coverSet struct {
	names   map[string]bool
	regexps []string
}

func newCoverSet(entries []string) coverSet {
	c := coverSet{names: map[string]bool{}}
	for _, entry := range entries {
		value, isRegex := sni.ParseDomainEntry(entry)
		switch {
		case value == "":
		case isRegex:
			c.regexps = append(c.regexps, value)
		default:
			c.names[value] = true
		}
	}
	return c
}

func (c coverSet) covers(host string) bool {
	for name := host; name != ""; {
		if c.names[name] {
			return true
		}
		dot := strings.IndexByte(name, '.')
		if dot < 0 {
			break
		}
		name = name[dot+1:]
	}
	for _, re := range c.regexps {
		if rel, _ := sni.MatchParsedEntry(re, true, host); rel == sni.RelationRegexp {
			return true
		}
	}
	return false
}

func hostService(host string) string {
	geoip, geosite := GetCDNCategories(host)
	switch {
	case len(geosite) > 0:
		return geosite[0]
	case len(geoip) > 0:
		return geoip[0]
	}
	return ""
}

func (ds *DiscoverySuite) addHostsTotal(n int) {
	ds.CheckSuite.mu.Lock()
	ds.HostsTotal += n
	ds.CheckSuite.mu.Unlock()
}

func (ds *DiscoverySuite) hostStepDone() {
	ds.CheckSuite.mu.Lock()
	if ds.HostsDone < ds.HostsTotal {
		ds.HostsDone++
	}
	ds.CheckSuite.mu.Unlock()
}
