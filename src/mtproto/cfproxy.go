package mtproto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	cfProxySuffix      = ".co.uk"
	DefaultCFProxyURL  = "https://raw.githubusercontent.com/Flowseal/tg-ws-proxy/main/.github/cfproxy-domains.txt"
	cfProxyRefreshInt  = time.Hour
	cfProxyMinValid    = 3
	cfProxyFetchMaxLen = 65536
	// cfProxyDomainCooldown is how long a CF-proxy domain is skipped after it
	// returns 429/503. Shared public domains get rate-limited in bursts; without
	// this, every dial re-hammers all 11 and DC1/3/5 (which have no Telegram WS
	// edge) stall entirely. Matches the observed recovery window in the field.
	cfProxyDomainCooldown = 60 * time.Second
)

// defaultCFProxyEncoded mirrors tg-ws-proxy/proxy/config.py:_CFPROXY_ENC.
// Caesar-shifted by alpha-char-count, .com suffix swapped for .co.uk at decode.
var defaultCFProxyEncoded = []string{
	"virkgj.com",
	"vmmzovy.com",
	"mkuosckvso.com",
	"zaewayzmplad.com",
	"twdmbzcm.com",
	"awzwsldi.com",
	"clngqrflngqin.com",
	"tjacxbqtj.com",
	"bxaxtxmrw.com",
	"dmohrsgmohcrwb.com",
}

// decodeCFDomain reverses the Flowseal/tg-ws-proxy obfuscation.
// Lines that don't end in .com pass through unchanged.
func decodeCFDomain(s string) string {
	if !strings.HasSuffix(s, ".com") {
		return s
	}
	p := s[:len(s)-4]
	n := 0
	for _, c := range p {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			n++
		}
	}
	var b strings.Builder
	b.Grow(len(p) + len(cfProxySuffix))
	for _, c := range p {
		switch {
		case c >= 'a' && c <= 'z':
			shifted := ((int(c)-'a'-n)%26 + 26) % 26
			b.WriteByte(byte(shifted) + 'a')
		case c >= 'A' && c <= 'Z':
			shifted := ((int(c)-'A'-n)%26 + 26) % 26
			b.WriteByte(byte(shifted) + 'A')
		default:
			b.WriteRune(c)
		}
	}
	b.WriteString(cfProxySuffix)
	return b.String()
}

func defaultCFProxyDomains() []string {
	out := make([]string, 0, len(defaultCFProxyEncoded))
	for _, e := range defaultCFProxyEncoded {
		out = append(out, decodeCFDomain(e))
	}
	return out
}

// isValidCFDomain mirrors tg-ws-proxy/proxy/config.py:_is_valid_domain.
func isValidCFDomain(domain string) bool {
	if domain == "" || len(domain) > 253 {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '-'
			if !ok {
				return false
			}
		}
	}
	tld := labels[len(labels)-1]
	if len(tld) < 2 {
		return false
	}
	hasAlpha := false
	for _, c := range tld {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasAlpha = true
			break
		}
	}
	return hasAlpha
}

// normalizeCFDomains lowercases, trims, validates, and dedupes (preserves order).
func normalizeCFDomains(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(d))
		if !isValidCFDomain(d) || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// cfBalancer is the Go equivalent of tg-ws-proxy/proxy/balancer.py:_Balancer.
// Per-DC sticky domain selection over a rotating pool; the active domain is
// tried first, the remaining pool is shuffled.
type cfBalancer struct {
	mu       sync.Mutex
	domains  []string
	perDC    map[int]string
	cooldown map[string]time.Time
}

func newCFBalancer() *cfBalancer {
	b := &cfBalancer{
		domains:  defaultCFProxyDomains(),
		perDC:    map[int]string{},
		cooldown: map[string]time.Time{},
	}
	b.seedPerDC()
	return b
}

// seedPerDC picks a random domain from the current pool for each known DC.
// Caller must hold mu.
func (b *cfBalancer) seedPerDC() {
	if len(b.domains) == 0 {
		b.perDC = map[int]string{}
		return
	}
	dcs := []int{1, -1, 2, -2, 3, -3, 4, -4, 5, -5, 203}
	b.perDC = make(map[int]string, len(dcs))
	for _, dc := range dcs {
		b.perDC[dc] = b.domains[cfproxyRand(len(b.domains))]
	}
}

func (b *cfBalancer) updateDomainsList(list []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(list) == 0 || sameStringSet(b.domains, list) {
		return
	}
	b.domains = append([]string(nil), list...)
	b.seedPerDC()
}

// domainsForDC returns the trial order for `dc`: current pinned first, then a
// shuffle of the rest. Domains in 429/503 cooldown are dropped, unless that
// would leave nothing (DC1/3/5 have no other WS path), in which case cooldown
// is ignored so we still try. Empty only if the pool itself is empty.
func (b *cfBalancer) domainsForDC(dc int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.domains) == 0 {
		return nil
	}
	now := time.Now()
	cooled := func(d string) bool {
		t, ok := b.cooldown[d]
		return ok && now.Before(t)
	}
	current := b.perDC[dc]
	others := make([]string, 0, len(b.domains))
	for _, d := range b.domains {
		if d != current {
			others = append(others, d)
		}
	}
	cfproxyShuffle(others)

	out := make([]string, 0, len(b.domains))
	if current != "" && !cooled(current) {
		out = append(out, current)
	}
	for _, d := range others {
		if !cooled(d) {
			out = append(out, d)
		}
	}
	if len(out) > 0 {
		return out
	}
	// everything is cooled down: fall back to trying all (still better than no path)
	if current != "" {
		out = append(out, current)
	}
	return append(out, others...)
}

// penalize puts a CF-proxy domain into cooldown after a 429/503 so subsequent
// dials skip it until it recovers.
func (b *cfBalancer) penalize(domain string, d time.Duration) {
	if domain == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cooldown[domain] = time.Now().Add(d)
}

// pin records that `domain` worked for `dc`. Returns true if the pin changed.
func (b *cfBalancer) pin(dc int, domain string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.perDC[dc] == domain {
		return false
	}
	b.perDC[dc] = domain
	return true
}

func (b *cfBalancer) size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.domains)
}

// refreshFromURL fetches the encoded domain list from the given URL,
// decodes + validates, and replaces the pool if the new list is sane.
func (b *cfBalancer) refreshFromURL(url string) error {
	if url == "" {
		url = DefaultCFProxyURL
	}
	// match tg-ws-proxy: random querystring suffix to defeat caches
	req, err := http.NewRequest("GET", url+"?cb="+cfproxyCacheBust(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "b4-mtproto")
	cli := &http.Client{Timeout: 10 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cfProxyFetchMaxLen))
	if err != nil {
		return err
	}
	var decoded []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		decoded = append(decoded, decodeCFDomain(line))
	}
	pool := normalizeCFDomains(decoded)
	if len(pool) < cfProxyMinValid {
		return fmt.Errorf("only %d valid domains, keeping current pool", len(pool))
	}
	b.updateDomainsList(pool)
	return nil
}

// cfBalancerInst is the package-level singleton. Initialized with the bundled
// defaults so b4 has a working CF pool from process start even before the
// first GitHub refresh succeeds.
var cfBalancerInst = newCFBalancer()

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
	}
	for _, v := range counts {
		if v != 0 {
			return false
		}
	}
	return true
}

// cfproxyRand returns an unbiased-ish int in [0,n) using crypto/rand.
// Non-crypto usage; modulo bias on a 64-bit value is negligible for our pool sizes.
func cfproxyRand(n int) int {
	if n <= 0 {
		return 0
	}
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	v := uint64(buf[0]) | uint64(buf[1])<<8 | uint64(buf[2])<<16 | uint64(buf[3])<<24 |
		uint64(buf[4])<<32 | uint64(buf[5])<<40 | uint64(buf[6])<<48 | uint64(buf[7])<<56
	return int(v % uint64(n))
}

func cfproxyShuffle(s []string) {
	for i := len(s) - 1; i > 0; i-- {
		j := cfproxyRand(i + 1)
		s[i], s[j] = s[j], s[i]
	}
}

func cfproxyCacheBust() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var (
	cfRefreshOnce sync.Once
	cfRefreshURL  atomic.Pointer[string]
)

func StartCFProxyRefresh(ctx interface{ Done() <-chan struct{} }, url string) {
	cfRefreshURL.Store(&url)
	cfRefreshOnce.Do(func() {
		go runCFProxyRefreshLoop(ctx)
	})
}

func runCFProxyRefreshLoop(ctx interface{ Done() <-chan struct{} }) {
	currentURL := func() string {
		if p := cfRefreshURL.Load(); p != nil {
			return *p
		}
		return ""
	}
	if err := cfBalancerInst.refreshFromURL(currentURL()); err != nil {
		log.Warnf("CF proxy refresh failed at startup: %v", err)
	} else {
		log.Infof("CF proxy pool refreshed (%d domains)", cfBalancerInst.size())
	}
	ticker := time.NewTicker(cfProxyRefreshInt)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := cfBalancerInst.refreshFromURL(currentURL()); err != nil {
				log.Debugf("CF proxy refresh failed: %v", err)
			} else {
				log.Debugf("CF proxy pool refreshed (%d domains)", cfBalancerInst.size())
			}
		}
	}
}
