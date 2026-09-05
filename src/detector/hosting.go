package detector

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	fatIterations     = 10
	fatPadBytes       = 4000
	fatDetectMinKB    = 12
	fatRandomPool     = 100_000
	fatDefaultSNI     = "example.com"
	fatConnectTimeout = 8 * time.Second
	fatReadTimeout    = 12 * time.Second
	fatMinTimeout     = 1500 * time.Millisecond
	fatRequestGap     = 50 * time.Millisecond
	sniBatchSize      = 5
	sniWanted         = 3
)

var randomPool string

func init() {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, fatRandomPool)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := range b {
		b[i] = chars[r.Intn(len(chars))]
	}
	randomPool = string(b)
}

type fatOutcome struct {
	alive    bool
	dropped  bool
	dropAtKB int
	rttMs    float64
	detail   string
}

func fatProbe(ctx context.Context, mark uint, ip string, port int, sni string, rttHint float64) fatOutcome {
	scheme := "https"
	if port == 80 {
		scheme = "http"
	}
	effectiveSNI := sni
	if effectiveSNI == "" && port != 80 {
		effectiveSNI = fatDefaultSNI
	}
	addr := net.JoinHostPort(ip, fmt.Sprint(port))
	var tlsConf *tls.Config
	if port != 80 {
		tlsConf = &tls.Config{InsecureSkipVerify: true, ServerName: effectiveSNI}
	}
	transport := &http.Transport{
		MaxConnsPerHost:     1,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     30 * time.Second,
		TLSClientConfig:     tlsConf,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return netprobe.Dialer(int(mark), fatConnectTimeout, 0).DialContext(ctx, "tcp", addr)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	target := fmt.Sprintf("%s://%s/", scheme, addr)

	timeout := fatReadTimeout
	rtt := rttHint
	if rttHint > 0 {
		timeout = clampFat(time.Duration(rttHint*float64(time.Millisecond)) * 3)
	}
	var samples []time.Duration

	for i := 0; i < fatIterations; i++ {
		if ctx.Err() != nil {
			return fatOutcome{detail: "canceled"}
		}
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, target, nil)
		if err != nil {
			cancel()
			return fatOutcome{detail: err.Error()}
		}
		req.Header.Set("User-Agent", fetchUserAgent)
		req.Header.Set("Connection", "keep-alive")
		if effectiveSNI != "" {
			req.Host = effectiveSNI
		}
		if i > 0 {
			start := rand.Intn(len(randomPool) - fatPadBytes - 1)
			req.Header.Set("X-Pad", randomPool[start:start+fatPadBytes])
		}
		start := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(start)
		cancel()
		if err != nil {
			detail := classifyFatError(err)
			if i == 0 {
				return fatOutcome{detail: detail}
			}
			kb := (i * fatPadBytes) / 1024
			return fatOutcome{alive: true, dropped: kb >= fatDetectMinKB, dropAtKB: kb, rttMs: rtt, detail: fmt.Sprintf("%s at %d KB", detail, kb)}
		}
		resp.Body.Close()
		if rttHint <= 0 && i < 2 {
			samples = append(samples, elapsed)
			if len(samples) == 2 {
				worst := samples[0]
				if samples[1] > worst {
					worst = samples[1]
				}
				rtt = float64(worst) / float64(time.Millisecond)
				timeout = clampFat(worst * 3)
			}
		}
		time.Sleep(fatRequestGap)
	}
	return fatOutcome{alive: true, rttMs: rtt}
}

func clampFat(d time.Duration) time.Duration {
	if d < fatMinTimeout {
		return fatMinTimeout
	}
	if d > fatReadTimeout {
		return fatReadTimeout
	}
	return d
}

func classifyFatError(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "refused"):
		return "refused"
	case strings.Contains(msg, "reset"):
		return "TCP RST"
	case strings.Contains(msg, "eof") || strings.Contains(msg, "closed"):
		return "connection closed"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "no answer"
	case strings.Contains(msg, "broken pipe"):
		return "broken pipe"
	default:
		return "error"
	}
}

func targetStatus(o fatOutcome) HostingStatus {
	switch {
	case o.dropped:
		return HostingDropped
	case o.alive:
		if o.dropAtKB > 0 {
			return HostingTimeout
		}
		return HostingOk
	case o.detail == "no answer":
		return HostingTimeout
	default:
		return HostingError
	}
}

func (s *Suite) runHosting() {
	lists := Lists()
	if len(lists.TCPTargets) == 0 {
		return
	}
	s.setProgress(ScopeHosting, "")

	groups := groupTargets(lists.TCPTargets)
	result := &HostingResult{Groups: groups}
	s.mu.Lock()
	s.Hosting = result
	s.mu.Unlock()
	log.DiscoveryLogf("[Detector] Hosting: probing %d targets in %d networks", len(lists.TCPTargets), len(groups))

	type slot struct{ g, t int }
	var slots []slot
	for gi := range groups {
		for ti := range groups[gi].Targets {
			slots = append(slots, slot{gi, ti})
		}
	}
	parallel := s.Options.Parallel * 2
	if parallel > 15 {
		parallel = 15
	}
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for _, sl := range slots {
		if s.canceled() {
			break
		}
		wg.Add(1)
		go func(sl slot) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tgt := groups[sl.g].Targets[sl.t].Target
			s.setProgress(ScopeHosting, tgt.Provider+" "+tgt.IP)
			o := fatProbe(s.ctx, s.directMark, tgt.IP, tgt.Port, tgt.SNI, 0)
			s.mu.Lock()
			tr := &groups[sl.g].Targets[sl.t]
			tr.Status = targetStatus(o)
			tr.DropAtKB = o.dropAtKB
			tr.RTTMs = round1(o.rttMs)
			tr.Detail = o.detail
			tr.Done = true
			summarizeGroup(&groups[sl.g])
			summarizeHosting(result)
			s.mu.Unlock()
			s.step(1)
		}(sl)
	}
	wg.Wait()

	if s.Options.SNISearch && !s.canceled() {
		s.searchSNIs(result)
	}
	s.mu.Lock()
	summarizeHosting(result)
	s.mu.Unlock()
	log.DiscoveryLogf("[Detector] Hosting: %d/%d targets dropped, %d of %d networks affected",
		result.Dropped, result.Total, result.DroppedGroups, len(result.Groups))
}

func groupTargets(targets []TCPTarget) []HostingGroup {
	byASN := make(map[string]*HostingGroup)
	var order []string
	for _, t := range targets {
		g, ok := byASN[t.ASN]
		if !ok {
			g = &HostingGroup{ASN: t.ASN, Provider: providerBase(t.Provider), Reference: t.Reference}
			byASN[t.ASN] = g
			order = append(order, t.ASN)
		}
		if t.Reference {
			g.Reference = true
		}
		g.Targets = append(g.Targets, TargetResult{Target: t})
	}
	out := make([]HostingGroup, 0, len(order))
	for _, asn := range order {
		g := byASN[asn]
		g.Total = len(g.Targets)
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Reference != out[j].Reference {
			return out[i].Reference
		}
		return false
	})
	return out
}

func providerBase(name string) string {
	name = strings.TrimSpace(name)
	for _, suffix := range []string{" HTTP"} {
		name = strings.TrimSuffix(name, suffix)
	}
	fields := strings.Fields(name)
	if len(fields) > 1 {
		last := fields[len(fields)-1]
		if strings.HasPrefix(last, "#") || isDigits(last) {
			fields = fields[:len(fields)-1]
		}
	}
	return strings.Join(fields, " ")
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func summarizeGroup(g *HostingGroup) {
	g.Dropped, g.Ok, g.Timeouts, g.DropMinKB, g.DropMaxKB = 0, 0, 0, 0, 0
	done := 0
	for _, t := range g.Targets {
		if !t.Done {
			continue
		}
		done++
		switch t.Status {
		case HostingDropped:
			g.Dropped++
			if g.DropMinKB == 0 || t.DropAtKB < g.DropMinKB {
				g.DropMinKB = t.DropAtKB
			}
			if t.DropAtKB > g.DropMaxKB {
				g.DropMaxKB = t.DropAtKB
			}
		case HostingOk:
			g.Ok++
		default:
			g.Timeouts++
		}
	}
	switch {
	case done == 0:
		g.Status = ""
	case g.Dropped > 0 && g.Ok == 0:
		g.Status = HostingDropped
	case g.Dropped > 0:
		g.Status = HostingMixed
	case g.Ok > 0:
		g.Status = HostingOk
	case g.Timeouts == done:
		g.Status = HostingTimeout
	default:
		g.Status = HostingError
	}
}

func summarizeHosting(r *HostingResult) {
	r.Total, r.Dropped, r.Ok, r.DroppedGroups, r.OkGroups = 0, 0, 0, 0, 0
	for _, g := range r.Groups {
		r.Total += g.Total
		r.Dropped += g.Dropped
		r.Ok += g.Ok
		switch g.Status {
		case HostingDropped, HostingMixed:
			r.DroppedGroups++
		case HostingOk:
			r.OkGroups++
		}
	}
}

func (s *Suite) searchSNIs(result *HostingResult) {
	candidates := append([]string{""}, Lists().WhitelistSNI...)
	for gi := range result.Groups {
		if s.canceled() {
			return
		}
		g := &result.Groups[gi]
		if g.Status != HostingDropped && g.Status != HostingMixed {
			continue
		}
		var best *TargetResult
		for ti := range g.Targets {
			t := &g.Targets[ti]
			if t.Status != HostingDropped || t.Target.Port == 80 {
				continue
			}
			if best == nil || (t.RTTMs > 0 && (best.RTTMs <= 0 || t.RTTMs < best.RTTMs)) {
				best = t
			}
		}
		if best == nil {
			continue
		}
		s.setProgress(ScopeHosting, "SNI for "+g.Provider)
		found := s.searchSNIFor(best.Target.IP, best.RTTMs, candidates)
		s.mu.Lock()
		g.SNISearched = true
		g.WorkingSNIs = found
		s.mu.Unlock()
		log.DiscoveryLogf("[Detector] Hosting: working SNI for %s (AS%s): %v", g.Provider, g.ASN, found)
	}
}

func (s *Suite) searchSNIFor(ip string, rttHint float64, candidates []string) []string {
	var found []string
	for start := 0; start < len(candidates) && len(found) < sniWanted; start += sniBatchSize {
		if s.canceled() {
			break
		}
		end := start + sniBatchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		batch := candidates[start:end]
		results := make([]bool, len(batch))
		var wg sync.WaitGroup
		for i, sni := range batch {
			wg.Add(1)
			go func(i int, sni string) {
				defer wg.Done()
				o := fatProbe(s.ctx, s.directMark, ip, 443, sni, rttHint)
				results[i] = o.alive && !o.dropped && o.dropAtKB == 0
			}(i, sni)
		}
		wg.Wait()
		for i, ok := range results {
			if ok && len(found) < sniWanted {
				label := batch[i]
				if label == "" {
					label = "(no SNI)"
				}
				found = append(found, label)
			}
		}
	}
	return found
}
