package nfq

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

const (
	inflightHost   = "www.example.com"
	inflightClient = "192.168.1.100"
)

type inflightRig struct {
	w         *Worker
	cfg       *config.Config
	meanwhile func()
}

func newInflightRig(t *testing.T, sets ...*config.SetConfig) *inflightRig {
	t.Helper()
	stubRouteHooks(t)
	stubDSCPHooks(t)
	w, cfg := dscpHookWorker(t, sets...)
	dscpDoHServer(t, w, "192.0.2.254")
	return &inflightRig{w: w, cfg: cfg, meanwhile: func() {}}
}

func (r *inflightRig) resolver(t *testing.T, answer string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		query, _ := io.ReadAll(req.Body)
		r.meanwhile()
		if answer == "" {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", dns.DoHContentType)
		_, _ = rw.Write(dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP(answer)}))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/dns-query"
}

func (r *inflightRig) reload(t *testing.T, sets ...*config.SetConfig) {
	t.Helper()
	pool := &Pool{Workers: []*Worker{r.w}, state: &runtimeState{hostHints: r.w.hostHints, destState: r.w.destState}}
	next := r.cfg.Clone()
	next.Queue.IsDiscovery = true
	next.Sets = sets
	if err := pool.UpdateConfig(next); err != nil {
		t.Error(err)
	}
}

func (r *inflightRig) ask(t *testing.T, overTCP bool, txid uint16) {
	t.Helper()
	query := dns.BuildQuery(inflightHost, txid, dnsTypeA)
	if overTCP {
		dscpWithin(t, dscpTCPAsk(t, r.w, query), "the TCP answer never came")
		return
	}
	watchReplies(t)
	r.w.processDnsPacket(&verdictCtx{id: uint32(txid), q: newFakeQueue()}, queryPacket(), 40000, 53, query)
	r.w.wg.Wait()
}

func inflightSet(id, doh string) *config.SetConfig {
	set := dscpHookSet(id, "example.com", false, false)
	if doh != "" {
		set.DNS.Enabled, set.DNS.DoHURL, set.DNS.Strict = true, doh, true
	}
	return set
}

func TestAnAnswerUnderWayDuringAReloadIsHintedByTheNewSets(t *testing.T) {
	const answer = "203.0.113.90"
	off := func(set *config.SetConfig) *config.SetConfig { set.Enabled = false; return set }
	for _, tc := range []struct {
		name string
		sets func(doh string) []*config.SetConfig
		want string
	}{
		{"nothing changes", nil, "first"},
		{"only the settings change", func(doh string) []*config.SetConfig {
			return []*config.SetConfig{inflightSet("first", doh), inflightSet("second", "")}
		}, "first"},
		{"an unrelated set changes", func(doh string) []*config.SetConfig {
			return []*config.SetConfig{inflightSet("first", doh), inflightSet("second", ""), dscpHookSet("third", "other.example", false, false)}
		}, "first"},
		{"the other set is moved above", func(doh string) []*config.SetConfig {
			return []*config.SetConfig{inflightSet("second", ""), inflightSet("first", doh)}
		}, "second"},
		{"its set is turned off", func(doh string) []*config.SetConfig {
			return []*config.SetConfig{off(inflightSet("first", doh)), inflightSet("second", "")}
		}, "second"},
		{"both sets are deleted", func(doh string) []*config.SetConfig {
			return []*config.SetConfig{dscpHookSet("third", "other.example", false, false)}
		}, ""},
	} {
		for _, overTCP := range []bool{false, true} {
			name := tc.name + ", UDP"
			if overTCP {
				name = tc.name + ", TCP"
			}
			t.Run(name, func(t *testing.T) {
				first, second := inflightSet("first", ""), inflightSet("second", "")
				r := newInflightRig(t, first, second)
				doh := r.resolver(t, answer)
				first.DNS.Enabled, first.DNS.DoHURL, first.DNS.Strict = true, doh, true
				if tc.sets != nil {
					r.meanwhile = func() { r.reload(t, tc.sets(doh)...) }
				}
				r.w.storeHostHint(inflightClient, "203.0.113.91", second, "elsewhere.other.org", "dns", 0, 0)
				r.ask(t, overTCP, 91)
				if got := storedHint(r.w, inflightClient, answer); got != tc.want {
					t.Errorf("the hint names %q, want %q", got, tc.want)
				}
			})
		}
	}
}

func TestAnEscalatedAnswerUnderWayDuringAReloadIsHintedByTheNewSets(t *testing.T) {
	const answer = "203.0.113.92"
	for _, tc := range []struct {
		name            string
		pinned, overTCP bool
	}{
		{"pinned, UDP", true, false},
		{"pinned, TCP", true, true},
		{"resolved, UDP", false, false},
		{"resolved, TCP", false, true},
	} {
		for _, reload := range []bool{false, true} {
			name, want := tc.name+", nothing changes", "backup"
			if reload {
				name, want = tc.name+", the backup is turned off", "primary"
			}
			t.Run(name, func(t *testing.T) {
				primary, backup := inflightSet("primary", ""), dscpHookSet("backup", "backup.example", false, false)
				r := newInflightRig(t, primary, backup)
				primary.DNS.Enabled, primary.DNS.DoHURL, primary.DNS.Strict = true, r.resolver(t, ""), true
				primary.Escalate.To, primary.Escalate.DNSThreshold = "backup", 1
				if tc.pinned {
					backup.DNS.Pins = map[string][]string{inflightHost: {answer}}
				} else {
					backup.DNS.Enabled, backup.DNS.DoHURL = true, (&inflightRig{meanwhile: func() {}}).resolver(t, answer)
				}
				if reload {
					r.meanwhile = func() {
						now, off := inflightSet("primary", primary.DNS.DoHURL), dscpHookSet("backup", "backup.example", false, false)
						now.Escalate.To, off.Enabled = "backup", false
						r.reload(t, now, off)
					}
				}
				r.ask(t, tc.overTCP, 92)
				if got := storedHint(r.w, inflightClient, answer); got != want {
					t.Errorf("the hint names %q, want %q", got, want)
				}
			})
		}
	}
}

func TestAHealedAnswerUnderWayDuringAReloadIsHintedByTheNewSets(t *testing.T) {
	for _, tc := range []struct {
		name             string
		overTCP, relayed bool
	}{
		{"redirected, UDP", false, false},
		{"redirected, TCP", true, false},
		{"relayed, TCP", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, second := inflightSet("first", ""), inflightSet("second", "")
			r := newInflightRig(t, first, second)
			first.TCP.IPBlockDetect.Enabled, first.TCP.IPBlockDetect.HealDNS = true, true
			r.w.goodIPs.Remember(inflightHost, net.ParseIP(healGoodIP))
			if tc.relayed {
				r.w.bans = newTestIPBans(t, nil)
				banAddress(t, r.w.bans, "203.0.113.80", 443)
				tcpDNSUpstream(t)
				dial := dialDNSTCPUpstream
				dialDNSTCPUpstream = func(ctx context.Context, cfg *config.Config, addr string) (net.Conn, error) {
					r.meanwhile()
					return dial(ctx, cfg, addr)
				}
			} else {
				markDead(t, r.w, healDeadIP)
				first.DNS.Enabled, first.DNS.DoHURL = true, r.resolver(t, healDeadIP)
			}
			r.meanwhile = func() { r.reload(t, inflightSet("second", ""), inflightSet("first", "")) }
			r.ask(t, tc.overTCP, 93)
			if got := storedHint(r.w, inflightClient, healGoodIP); got != "second" {
				t.Errorf("the hint of the address heal put in names %q, want second", got)
			}
		})
	}
}

func TestSettlingOneAnswerLeavesTheOtherHintsAlone(t *testing.T) {
	first := newSharedNameSet("first", "shared.example")
	w, reload := hintReloadPool(t, &first)
	asked := w.getMatcher()
	again, second := newSharedNameSet("first", "shared.example"), newSharedNameSet("second", "www.shared.example", "mail.shared.example")
	reload(&again, &second)
	w.storeHostHint("10.0.0.1", "1.2.3.4", &first, "www.shared.example", "dns", 0, 0)
	w.storeHostHint("10.0.0.2", "1.2.3.4", &first, "www.shared.example", "dns", 0, 0)
	w.storeHostHint("10.0.0.1", "1.2.3.5", &first, "mail.shared.example", "dns", 0, 0)
	w.settleHostHints(asked, net.ParseIP("10.0.0.1"), []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("1.2.3.6")})
	for _, tc := range []struct{ client, dest, want string }{
		{"10.0.0.1", "1.2.3.4", "second"},
		{"10.0.0.2", "1.2.3.4", "first"},
		{"10.0.0.1", "1.2.3.5", "first"},
	} {
		if got := storedHint(w, tc.client, tc.dest); got != tc.want {
			t.Errorf("%s -> %s: the hint names %q, want %q", tc.client, tc.dest, got, tc.want)
		}
	}
	w.settleHostHints(w.getMatcher(), net.ParseIP("10.0.0.2"), []net.IP{net.ParseIP("1.2.3.4")})
	if got := storedHint(w, "10.0.0.2", "1.2.3.4"); got != "first" {
		t.Errorf("the sets did not change since the query, the hint names %q", got)
	}
	w.settleHostHints(asked, net.ParseIP("10.0.0.2"), nil)
	if got := storedHint(w, "10.0.0.2", "1.2.3.4"); got != "first" {
		t.Errorf("the answer has no address, the hint of another one names %q", got)
	}
}

func TestSettlingGoesOnWhileTheSetsChange(t *testing.T) {
	first := newSharedNameSet("first", "shared.example")
	w, reload := hintReloadPool(t, &first)
	asked := w.getMatcher()
	second := newSharedNameSet("second", "shared.example")
	reload(&second)
	w.storeHostHint("10.0.0.1", "1.2.3.4", &first, "www.shared.example", "dns", 0, 0)
	w.destState.mu.Lock()
	settled := make(chan struct{})
	go func() {
		defer close(settled)
		w.settleHostHints(asked, net.ParseIP("10.0.0.1"), []net.IP{net.ParseIP("1.2.3.4")})
	}()
	time.Sleep(20 * time.Millisecond)
	again := newSharedNameSet("first", "shared.example")
	next := w.getConfig().Clone()
	next.Sets = []*config.SetConfig{&again}
	w.cfg.Store(next)
	w.matcher.Store(buildMatcher(next))
	w.destState.mu.Unlock()
	<-settled
	if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "first" {
		t.Errorf("the sets changed while the answer was settled, the hint names %q, want first", got)
	}
}

func TestAPlainAnswerUnderWayDuringAReloadIsHintedByTheNewSets(t *testing.T) {
	const answer = "203.0.113.96"
	routed := func(domain string, enabled bool) *config.SetConfig {
		set := dscpHookSet("routed", domain, false, true)
		set.Enabled = enabled
		return set
	}
	for _, tc := range []struct {
		name string
		sets []*config.SetConfig
		want string
	}{
		{"nothing changes", nil, "routed"},
		{"another set takes the name", []*config.SetConfig{routed("else.example", true), inflightSet("second", "")}, "second"},
		{"another set is put above", []*config.SetConfig{inflightSet("second", ""), routed("example.com", true)}, "second"},
		{"the set is turned off", []*config.SetConfig{routed("example.com", false), dscpHookSet("third", "other.example", false, false)}, ""},
		{"the set loses the name", []*config.SetConfig{routed("else.example", true), dscpHookSet("third", "other.example", false, false)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(stopDNSRouteCleanup)
			r := newInflightRig(t, routed("example.com", true), dscpHookSet("third", "other.example", false, false))
			r.w.processDnsPacket(&verdictCtx{id: 1, q: newFakeQueue()}, queryPacket(), 40000, 53, dns.BuildQuery(inflightHost, 94, dnsTypeA))
			if tc.sets != nil {
				r.reload(t, tc.sets...)
			}
			r.w.processDnsPacket(&verdictCtx{id: 2, q: newFakeQueue()}, answerPacket(), 53, 40000, buildDNSResponse(94, inflightHost, []net.IP{net.ParseIP(answer)}))
			r.w.wg.Wait()
			if got := storedHint(r.w, inflightClient, answer); got != tc.want {
				t.Errorf("the hint names %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAPlainAnswerEscalatedTwiceDuringAReloadIsHintedByTheNewSets(t *testing.T) {
	const answer = "203.0.113.95"
	for _, reload := range []bool{false, true} {
		name, want := "nothing changes", "last"
		if reload {
			name, want = "the last set is turned off", "primary"
		}
		t.Run(name, func(t *testing.T) {
			chain := func(doh string, lastOn bool) []*config.SetConfig {
				primary, mid, last := inflightSet("primary", ""), dscpHookSet("mid", "mid.example", false, false), dscpHookSet("last", "last.example", false, false)
				primary.Escalate.To, primary.Escalate.DNSThreshold = "mid", 1
				mid.Escalate.To, mid.Escalate.DNSThreshold = "last", 1
				mid.DNS.Enabled, mid.DNS.DoHURL, mid.DNS.Strict = true, doh, true
				last.DNS.Pins, last.Enabled = map[string][]string{inflightHost: {answer}}, lastOn
				return []*config.SetConfig{primary, mid, last}
			}
			sets := chain("", true)
			r := newInflightRig(t, sets...)
			doh := r.resolver(t, "")
			sets[1].DNS.DoHURL = doh
			if reload {
				r.meanwhile = func() { r.reload(t, chain(doh, false)...) }
			}
			watchReplies(t)
			nothing := dns.BuildBlockResponse(dns.BuildQuery(inflightHost, 95, dnsTypeA))
			r.w.processDnsPacket(&verdictCtx{id: 1, q: newFakeQueue()}, answerPacket(), 53, 40000, nothing)
			r.w.wg.Wait()
			if got := storedHint(r.w, inflightClient, answer); got != want {
				t.Errorf("the hint names %q, want %q", got, want)
			}
		})
	}
}
