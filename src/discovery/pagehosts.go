package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

var hostRunBudget = 2 * time.Minute

const (
	maxPageReads    = 3
	maxHistoryHosts = 32
	pageUserAgent   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
	pageAccept      = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	hostsSkippedByUser = "stopped"
	hostsOverBudget    = "budget"
	pageNotRead        = "page"
	pageNotHTML        = "not_html"
)

type pageTarget struct {
	HostTarget
	pages map[string]*pageRead
	names map[string][]string
}

type pageRead struct {
	final     *url.URL
	cands     []HostCandidate
	trackers  int
	bytes     int64
	overflow  int
	skipped   string
	detail    string
	truncated bool
}

func (ds *DiscoverySuite) checkPageHosts() {
	if ds.Source == SourceWatchdog || ds.cfg == nil || (ds.pool == nil && ds.hostConfigFn == nil) || ds.canceled() {
		return
	}
	targets := ds.pageTargets()
	if len(targets) == 0 {
		return
	}
	stop := ds.beginHostsStep()
	defer ds.endHostsStep()
	watched, cancel := watchedContext(ds.cancel, stop)
	defer cancel()
	ctx, cancelBudget := context.WithTimeout(watched, hostRunBudget)
	defer cancelBudget()

	for _, t := range targets {
		ds.readTargetPages(ctx, t)
	}
	checks := make([]*HostTarget, len(targets))
	for i, t := range targets {
		checks[i] = &t.HostTarget
	}
	run := ds.newHostRun(checks)
	run.withoutPass(ctx)
	for i := range run.targets {
		run.withPass(ctx, i)
	}
	ds.endHostsStep()
	if ds.canceled() {
		return
	}
	ds.storePageHosts(targets, run, ds.hostsCutShort(ctx))
}

func (ds *DiscoverySuite) beginHostsStep() chan struct{} {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()
	stop := make(chan struct{})
	ds.hostsStop = stop
	ds.CurrentPhase = PhaseHosts
	return stop
}

func (ds *DiscoverySuite) endHostsStep() {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()
	if ds.hostsStop != nil {
		select {
		case <-ds.hostsStop:
		default:
			close(ds.hostsStop)
		}
	}
	ds.HostsDone = ds.HostsTotal
}

func (s *CheckSuite) skipHostsLocked() bool {
	if s.CurrentPhase != PhaseHosts || s.hostsStop == nil {
		return false
	}
	select {
	case <-s.hostsStop:
	default:
		close(s.hostsStop)
		s.HostsSkipped = true
	}
	return true
}

func (ds *DiscoverySuite) hostsCutShort(ctx context.Context) string {
	ds.CheckSuite.mu.RLock()
	skipped := ds.HostsSkipped
	ds.CheckSuite.mu.RUnlock()
	switch {
	case skipped:
		log.DiscoveryLogf("Page hosts: skipped by the user")
		return hostsSkippedByUser
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return hostsOverBudget
	}
	return ""
}

func (ds *DiscoverySuite) pageTargets() []*pageTarget {
	type pick struct {
		preset  string
		domains []string
	}
	var picks []pick
	ds.CheckSuite.mu.RLock()
	for _, g := range ds.StrategyGroups {
		if g.Set == nil || len(g.Domains) == 0 {
			continue
		}
		if !ds.confirmedOn(g.WinnerPreset, g.Domains) {
			log.DiscoveryLogf("Page hosts: '%s' is not confirmed for %s, its pages are not read", g.WinnerPreset, strings.Join(g.Domains, ", "))
			continue
		}
		picks = append(picks, pick{preset: g.WinnerPreset, domains: append([]string(nil), g.Domains...)})
	}
	ds.CheckSuite.mu.RUnlock()

	var targets []*pageTarget
	for _, p := range picks {
		stored := ds.storedSetFor(p.preset, p.domains)
		if stored == nil {
			log.DiscoveryLogf("Page hosts: the tested configuration of '%s' was not recorded, its pages are not read", p.preset)
			continue
		}
		targets = append(targets, &pageTarget{
			HostTarget: HostTarget{Preset: p.preset, Set: stored, Domains: p.domains},
			pages:      map[string]*pageRead{},
			names:      map[string][]string{},
		})
	}
	return targets
}

func (ds *DiscoverySuite) readTargetPages(ctx context.Context, t *pageTarget) {
	t.base = ds.deployedConfig(t.Set, t.Domains, nil)
	if t.base == nil {
		log.DiscoveryLogf("Page hosts: could not rebuild '%s' as it would be deployed", t.Preset)
		return
	}
	if ctx.Err() != nil {
		return
	}
	log.DiscoveryLogf("Page hosts: reading %s through '%s'", strings.Join(t.Domains, ", "), t.Preset)
	if err := ds.applyHostConfig(t.base); err != nil {
		log.DiscoveryLogf("Page hosts: could not apply '%s': %v", t.Preset, err)
		return
	}

	inputs := ds.confirmInputs(t.Preset, t.Domains)
	reads := make([]*pageRead, len(inputs))
	slots := make(chan struct{}, maxPageReads)
	var wg sync.WaitGroup
	for i, di := range inputs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			reads[i] = ds.readPage(ctx, di)
		}()
	}
	wg.Wait()

	for i, di := range inputs {
		page := reads[i]
		if page == nil {
			continue
		}
		t.pages[di.Domain] = page
		logPageRead(di, page)
		if page.skipped != "" {
			continue
		}
		for _, c := range page.cands {
			t.addCandidate(c, di.Domain)
		}
		if t.Origin == "" && page.final != nil {
			t.Origin = originOf(page.final)
		}
	}
}

func (t *pageTarget) addCandidate(c HostCandidate, domain string) {
	if !slices.Contains(t.names[c.Host], domain) {
		t.names[c.Host] = append(t.names[c.Host], domain)
	}
	for i := range t.Candidates {
		have := &t.Candidates[i]
		if have.Host != c.Host {
			continue
		}
		have.Blocking = have.Blocking || c.Blocking
		have.Tracker = have.Tracker || c.Tracker
		if hostContextRank(c.Context) < hostContextRank(have.Context) {
			have.Context = c.Context
		}
		for _, u := range c.URLs {
			if len(have.URLs) < maxHostSamples && !slices.Contains(have.URLs, u) {
				have.URLs = append(have.URLs, u)
			}
		}
		return
	}
	t.Candidates = append(t.Candidates, c)
}

func originOf(u *url.URL) string {
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + u.Host + "/"
}

func logPageRead(di DomainInput, page *pageRead) {
	switch {
	case page.skipped == pageNotHTML:
		log.DiscoveryLogf("  [%s] %s is not a web page (%s), no hosts to read", di.Domain, di.CheckURL, page.detail)
	case page.skipped != "":
		log.DiscoveryLogf("  [%s] %s could not be read through the strategy (%s)", di.Domain, di.CheckURL, page.detail)
	default:
		note := ""
		if page.truncated {
			note = ", only the start of the page was read"
		}
		if page.overflow > 0 {
			note += fmt.Sprintf(", more than %d hosts, the rest were ignored", maxPageHosts)
		}
		log.DiscoveryLogf("  [%s] read %d KB of %s, %d hosts load from elsewhere (%d trackers left out)%s", di.Domain, page.bytes/1024, di.CheckURL, len(page.cands), page.trackers, note)
	}
}

func (ds *DiscoverySuite) pageAddresses(domain string) []string {
	ips := ds.collectTargetIPs(domain, 2)
	if geoip, geosite := GetCDNCategories(domain); len(geoip) > 0 || len(geosite) > 0 {
		if len(ips) > 0 {
			return ips[:1]
		}
		return []string{""}
	}
	if len(ips) == 0 {
		return []string{""}
	}
	return ips
}

func (ds *DiscoverySuite) readPage(ctx context.Context, di DomainInput) *pageRead {
	var page *pageRead
	for _, ip := range ds.pageAddresses(di.Domain) {
		page = ds.readPageVia(ctx, di, ip)
		if page.skipped != pageNotRead || ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil && (page.skipped == pageNotRead || page.truncated) {
		return nil
	}
	if page.skipped == "" && page.final != nil {
		if requested, err := url.Parse(di.CheckURL); err == nil && !strings.EqualFold(requested.Hostname(), page.final.Hostname()) {
			if host, sample, ok := absoluteURL(page.final.String()); ok {
				redirect := HostCandidate{Host: host, Source: HostFromPage, Context: HostContextRedirect, Blocking: true}
				if sample != "" {
					redirect.URLs = []string{sample}
				}
				page.cands = append([]HostCandidate{redirect}, page.cands...)
			}
		}
	}
	return page
}

type armingReader struct {
	r   io.Reader
	arm func()
}

func (a *armingReader) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	if n > 0 {
		a.arm()
	}
	return n, err
}

func (ds *DiscoverySuite) readPageVia(ctx context.Context, di DomainInput, ip string) *pageRead {
	timeout := ds.hostTimeout()
	readCtx, cancel := context.WithTimeout(ctx, 3*timeout)
	defer cancel()
	reqCtx, abort := context.WithCancel(readCtx)
	defer abort()

	transport := &http.Transport{
		TLSClientConfig:       ds.tlsConfig(),
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
		ForceAttemptHTTP2:     true,
	}
	defer transport.CloseIdleConnections()

	var armed atomic.Bool
	stall := time.AfterFunc(probeStallTimeout, abort)
	stall.Stop()
	defer func() {
		armed.Store(false)
		stall.Stop()
	}()
	dial := ds.dialContext(timeout, di.Domain, ip)
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &progressConn{Conn: conn, onRead: func() {
			if armed.Load() {
				stall.Reset(probeStallTimeout)
			}
		}}, nil
	}
	if _, ech := ds.echFor(di.Domain); ech && !di.plainTLS {
		transport.DialTLSContext = ds.echDialTLS(transport.DialContext)
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && netprobe.IsBlockPageRedirectFrom(via[len(via)-1].URL, req.URL) {
				return &blockPageRedirect{target: req.URL.String()}
			}
			if len(via) >= maxProbeRedirects {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, di.CheckURL, nil)
	if err != nil {
		return &pageRead{skipped: pageNotRead, detail: err.Error()}
	}
	req.Header.Set("User-Agent", pageUserAgent)
	req.Header.Set("Accept", pageAccept)

	resp, err := client.Do(req)
	if err != nil {
		var blocked *blockPageRedirect
		detail := ""
		if errors.As(err, &blocked) {
			detail = blocked.Error()
		} else {
			_, detail = netprobe.ClassifyTLSError(err)
		}
		return &pageRead{skipped: pageNotRead, detail: detail}
	}
	defer resp.Body.Close()

	page := &pageRead{final: resp.Request.URL}
	if resp.StatusCode >= 400 {
		page.skipped, page.detail = pageNotRead, fmt.Sprintf("HTTP %d", resp.StatusCode)
		return page
	}
	if kind := strings.ToLower(resp.Header.Get("Content-Type")); kind != "" && !strings.Contains(kind, "html") {
		page.skipped, page.detail = pageNotHTML, kind
		return page
	}
	scan := scanPage(&armingReader{r: resp.Body, arm: func() {
		if !armed.Swap(true) {
			stall.Reset(probeStallTimeout)
		}
	}})
	page.bytes, page.overflow = scan.bytes, scan.overflow
	page.truncated = scan.truncated || scan.err != nil
	page.cands, page.trackers = scan.candidates()
	return page
}

func (ds *DiscoverySuite) storePageHosts(targets []*pageTarget, run *hostRun, reason string) {
	var lines []string
	var overBudget []string
	ds.CheckSuite.mu.Lock()
	for i, t := range targets {
		results := run.results(i)
		for _, d := range t.Domains {
			dr := ds.domainResults[d]
			if dr == nil {
				continue
			}
			check := &HostCheck{Preset: t.Preset}
			page := t.pages[d]
			switch {
			case page == nil && reason != "":
				check.Skipped = reason
			case page == nil:
				check.Skipped = pageNotRead
			case page.skipped != "":
				check.Skipped = page.skipped
			default:
				check.Truncated = page.truncated
				for _, r := range results {
					if slices.Contains(t.names[r.Host], d) {
						check.Hosts = append(check.Hosts, r)
					}
				}
				if reason != "" && cutShort(check.Hosts) {
					check.Skipped = reason
				}
			}
			if check.Skipped == hostsOverBudget {
				overBudget = append(overBudget, d)
			}
			if needed := hostsWith(check.Hosts, HostNeeded); len(needed) > 0 {
				lines = append(lines, fmt.Sprintf("  [%s] hosts that need '%s': %s", d, t.Preset, strings.Join(needed, ", ")))
			}
			dr.PageHosts = check
		}
	}
	ds.CheckSuite.mu.Unlock()

	for _, line := range lines {
		log.DiscoveryLogf("%s", line)
	}
	if len(overBudget) > 0 {
		log.DiscoveryLogf("Page hosts: time limit reached, %s not checked to the end", strings.Join(overBudget, ", "))
	}
}

func cutShort(hosts []HostResult) bool {
	for _, h := range hosts {
		if h.Verdict == HostUntested && h.Error == "" {
			return true
		}
	}
	return false
}

func hostsWith(hosts []HostResult, verdict HostVerdict) []string {
	var out []string
	for _, h := range hosts {
		if h.Verdict == verdict {
			out = append(out, h.Host)
		}
	}
	return out
}

func logPageHostsSummary(check *HostCheck) {
	if check == nil {
		return
	}
	switch check.Skipped {
	case hostsSkippedByUser:
		log.DiscoveryLogf("    page hosts: not checked to the end, skipped by the user")
	case hostsOverBudget:
		log.DiscoveryLogf("    page hosts: not checked to the end, time limit reached")
	case pageNotRead:
		log.DiscoveryLogf("    page hosts: not checked, the page could not be read through the strategy")
	case pageNotHTML:
		log.DiscoveryLogf("    page hosts: not checked, the link is not a web page")
	}
	needed, notFixed := hostsWith(check.Hosts, HostNeeded), hostsWith(check.Hosts, HostNotFixed)
	if len(needed) == 0 && len(notFixed) == 0 {
		return
	}
	log.DiscoveryLogf("    page hosts: %d need the strategy%s, %d blocked hosts it does not fix%s", len(needed), listInParens(needed), len(notFixed), listInParens(notFixed))
}

func listInParens(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return " (" + strings.Join(items, ", ") + ")"
}

func historyHosts(check *HostCheck) *HostCheck {
	if check == nil {
		return nil
	}
	out := &HostCheck{Preset: check.Preset, Skipped: check.Skipped, Truncated: check.Truncated}
	for _, h := range check.Hosts {
		if h.Verdict != HostNeeded && h.Verdict != HostNotFixed {
			continue
		}
		if len(out.Hosts) == maxHistoryHosts {
			break
		}
		out.Hosts = append(out.Hosts, HostResult{
			HostCandidate: HostCandidate{
				Host:     h.Host,
				Source:   h.Source,
				Context:  h.Context,
				Blocking: h.Blocking,
				Tracker:  h.Tracker,
			},
			Verdict: h.Verdict,
			Service: h.Service,
		})
	}
	if len(out.Hosts) == 0 && out.Skipped == "" {
		return nil
	}
	return out
}
