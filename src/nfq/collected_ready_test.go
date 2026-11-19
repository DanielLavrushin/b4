package nfq

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
)

func readyConsumer(priority bool) *config.SetConfig {
	consumer := newPassiveSet()
	consumer.Id, consumer.Name = "arbitrary-consumer", "Any name"
	consumer.Targets.DomainOnly = false
	consumer.Targets.DomainsToMatch = nil
	consumer.Targets.UseCollectedIPs = true
	consumer.Targets.IPPriority = priority
	return &consumer
}

func readyProducer() *config.SetConfig {
	producer := silenceSet()
	producer.TCP.IPBlockDetect.Action = "collect"
	return &producer
}

func newCollectReadyRig(t *testing.T, sets ...*config.SetConfig) *collectCountRig {
	t.Helper()
	r := &collectCountRig{flows: make(map[ctnl.Tuple]*ctnl.Flow), start: time.Now()}
	r.w = newInjectingWorker(t, sets...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.p = &Pool{Workers: []*Worker{r.w}, state: &runtimeState{tlsCache: r.w.tlsCache}, collectedBase: r.w.getConfig(), collectedCtx: ctx, collectedCancel: cancel, collectedInstall: oneStepInstaller(func(context.Context, *config.Config) error {
		r.installs++
		return nil
	})}
	r.w.pool = r.p
	r.w.bans = newTestIPBans(t, nil)
	r.sent = captureInjected(t, r.w)
	old := lookupFlow
	lookupFlow = func(tuple ctnl.Tuple, _ ctnl.Direction) (ctnl.Flow, bool, error) {
		return *r.flowOf(tuple), true, nil
	}
	t.Cleanup(func() { lookupFlow = old })
	return r
}

func (r *collectCountRig) watchOf(t *testing.T, sport uint16) helloWatch {
	t.Helper()
	r.w.destState.watchMu.RLock()
	defer r.w.destState.watchMu.RUnlock()
	for _, h := range r.w.destState.watches {
		if h.cport == sport {
			return *h
		}
	}
	t.Fatalf("the ClientHello from port %d is not watched", sport)
	return helloWatch{}
}

func (r *collectCountRig) decidedBy(t *testing.T, sport uint16) string {
	t.Helper()
	r.w.ProcessPacket(silentHelloFrom(sport))
	decision, ok := r.w.tlsCache.Decision(fmt.Sprintf(connKeyFormat, "10.0.0.1", sport, "1.2.3.4", 443))
	if !ok || decision.set == nil {
		t.Fatalf("the ClientHello from port %d was decided by no set", sport)
	}
	return decision.set.Id
}

func (r *collectCountRig) resetLog() func() []uint16 {
	var sent [][]byte
	return func() []uint16 {
		sent = append(sent, r.sent()...)
		return resetsTo(sent)
	}
}

func matcherBuilds(logs string) int {
	return strings.Count(logs, "Built matcher with")
}

func TestCollectResetsOnlyWhenTheConsumerTakesTheRetry(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sets     func() []*config.SetConfig
		owned    bool
		installs int
		resets   []uint16
	}{
		{
			name:     "consumer first with IP priority",
			sets:     func() []*config.SetConfig { return []*config.SetConfig{readyConsumer(true), readyProducer()} },
			owned:    true,
			installs: 1,
			resets:   []uint16{42000, 42001},
		},
		{
			name:  "consumer first without IP priority",
			sets:  func() []*config.SetConfig { return []*config.SetConfig{readyConsumer(false), readyProducer()} },
			owned: false,
		},
		{
			name:  "producer first, consumer with IP priority",
			sets:  func() []*config.SetConfig { return []*config.SetConfig{readyProducer(), readyConsumer(true)} },
			owned: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCollectReadyRig(t, tc.sets()...)
			resets := r.resetLog()
			logs := captureLog(t)
			r.silent(t, 42000)
			r.sweep(4 * time.Second)
			r.silent(t, 42001)
			second := r.watchOf(t, 42001)
			r.sweep(5 * time.Second)

			if r.installs != tc.installs || !slices.Equal(resets(), tc.resets) {
				t.Fatalf("installs=%d resets=%v, want installs=%d resets=%v: a silent client is reset only when a set that uses collected IPs takes its retry", r.installs, resets(), tc.installs, tc.resets)
			}
			if got := matcherBuilds(logs.String()); got != 1 {
				t.Fatalf("the pass built %d matchers, want the one candidate matcher, reused by the publish when the address is collected", got)
			}
			if tc.owned {
				if r.watched(42000) || r.watched(42001) {
					t.Fatal("reset connections must not stay watched")
				}
			} else {
				if r.watched(42000) || r.watched(42001) || r.collected() != 0 {
					t.Fatalf("an address nobody takes must end its connections and stay out of the pool, candidates=%d", r.collected())
				}
				if n := strings.Count(logs.String(), "not collected: no set with Use collected IPs takes its next connection"); n != 1 {
					t.Fatalf("the refused address must be reported once, got %d lines", n)
				}
				r.silent(t, 42002)
				r.sweep(6 * time.Second)
				if r.installs != 0 || len(resets()) != 0 || matcherBuilds(logs.String()) != 1 {
					t.Fatalf("a further silent connection in the same run must end without a matcher build: installs=%d resets=%v builds=%d", r.installs, resets(), matcherBuilds(logs.String()))
				}
				if n := strings.Count(logs.String(), "not collected: no set with Use collected IPs takes its next connection"); n != 1 {
					t.Fatalf("the refused address must be reported once per run, got %d lines", n)
				}
			}

			view := r.p.collectedBase.WithCollectedIPs([]string{"1.2.3.4"})
			matcher := buildMatcher(view)
			_, owned := collectedOwner(view, matcher, second)
			if owned != tc.owned {
				t.Fatalf("collectedOwner says owned=%v, want %v", owned, tc.owned)
			}
			r.w.cfg.Store(view)
			r.w.matcher.Store(matcher)
			r.w.tlsCache.ForgetDecisions(matcher)
			decided := r.decidedBy(t, 42010)
			if (decided == "arbitrary-consumer") != owned {
				t.Fatalf("collectedOwner says owned=%v, but with the address published the retry is decided by %q: the check drifted from the ClientHello decision", owned, decided)
			}
		})
	}
}

func TestARoutingConsumerOwnsTheRetryWhateverTheName(t *testing.T) {
	proxy := readyConsumer(false)
	proxy.Id = "proxy-consumer"
	proxy.Routing.Enabled, proxy.Routing.Mode = true, config.RoutingModeProxy
	reuse := readyConsumer(false)
	reuse.Id = "reuse-consumer"
	reuse.TCP.WSSizeReuse.Enabled = true
	queue := readyConsumer(false)
	queue.Id = "queue-consumer"
	for _, tc := range []struct {
		name     string
		consumer *config.SetConfig
		port     uint16
		owned    bool
	}{
		{name: "proxy below the producer", consumer: proxy, port: 443, owned: true},
		{name: "reuse below the producer on 443", consumer: reuse, port: 443, owned: true},
		{name: "reuse below the producer on another port", consumer: reuse, port: 8443, owned: false},
		{name: "queue consumer below the producer", consumer: queue, port: 443, owned: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Sets = []*config.SetConfig{readyProducer(), tc.consumer}
			view := cfg.WithCollectedIPs([]string{"1.2.3.4"})
			matcher := buildMatcher(view)
			h := helloWatch{dst: net.ParseIP("1.2.3.4").To4(), port: tc.port, host: "i.ytimg.com", tls: 0x0303, ver: IPv4}
			consumer, owned := collectedOwner(view, matcher, h)
			if owned != tc.owned {
				t.Fatalf("owned=%v, want %v", owned, tc.owned)
			}
			if consumer == nil || consumer.Id != tc.consumer.Id {
				t.Fatalf("the consumer found is %v, want %s", consumer, tc.consumer.Id)
			}
		})
	}
}

func TestAnAddressAlreadyCollectedIsNotInstalledAgain(t *testing.T) {
	const ownedMac, otherMac = "aa:aa:aa:aa:aa:01", "aa:aa:aa:aa:aa:02"
	consumer := readyConsumer(true)
	consumer.Targets.SourceDevices = []string{ownedMac}
	producer := readyProducer()
	r := newCollectReadyRig(t, consumer, producer)
	resets := r.resetLog()
	device := func(mac string) { r.w.ipToMac.Store(map[string]string{"10.0.0.1": mac}) }

	producer.TCP.IPBlockDetect.TimeoutMs = 30000
	device(ownedMac)
	r.silent(t, 42002)
	device(otherMac)
	r.silent(t, 42003)
	producer.TCP.IPBlockDetect.TimeoutMs = 3000
	device(ownedMac)
	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	r.silent(t, 42001)
	r.sweep(5 * time.Second)
	if r.installs != 1 || !slices.Equal(resets(), []uint16{42000, 42001}) {
		t.Fatalf("setup: the first pair must collect the address: installs=%d resets=%v", r.installs, resets())
	}
	matcher := r.w.getMatcher()

	r.sweep(31 * time.Second)

	if r.installs != 1 {
		t.Fatalf("a pair that completes for an address already collected ran %d more installs, want none", r.installs-1)
	}
	if r.w.getMatcher() != matcher {
		t.Fatal("a pair that completes for an address already collected rebuilt the matcher")
	}
	if ports := resets(); !slices.Equal(ports, []uint16{42000, 42001, 42002}) {
		t.Fatalf("got resets to %v, want the connection the consumer takes reset (42002) and the one from another device (42003) left alone", ports)
	}
	if r.watched(42002) || r.watched(42003) {
		t.Fatal("both connections that completed for the collected address must stop being watched")
	}
}

func TestAMispredictedOwnerResetsOnlyOnce(t *testing.T) {
	consumer := readyConsumer(true)
	producer := readyProducer()
	consumer.Escalate.To = producer.Id
	r := newCollectReadyRig(t, consumer, producer)
	resets := r.resetLog()
	r.w.destState.SetEscalation("i.ytimg.com", producer.Id, escalateReasonStall, time.Hour)
	logs := captureLog(t)

	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	r.silent(t, 42001)
	r.sweep(5 * time.Second)
	if r.installs != 1 || !slices.Equal(resets(), []uint16{42000, 42001}) {
		t.Fatalf("setup: the first pair must collect the address: installs=%d resets=%v", r.installs, resets())
	}

	r.silent(t, 42002)
	retry := r.watchOf(t, 42002)
	if retry.setID != producer.Id {
		t.Fatalf("setup: the retry must be taken by the escalation target %s, got %s, so this test proves nothing", producer.Id, retry.setID)
	}
	if _, owned := collectedOwner(r.w.getConfig(), r.w.getMatcher(), retry); !owned {
		t.Fatal("setup: collectedOwner must mispredict the escalated retry as owned, or this test proves nothing")
	}
	r.sweep(6 * time.Second)
	r.silent(t, 42003)
	r.sweep(7 * time.Second)

	if ports := resets(); !slices.Equal(ports, []uint16{42000, 42001}) {
		t.Fatalf("got resets to %v: a retry that came after the publication and still reached the producer must end without a reset, or the client loops", ports)
	}
	if r.installs != 1 {
		t.Fatalf("installs=%d, want the one publication", r.installs)
	}
	if r.watched(42002) || r.watched(42003) {
		t.Fatal("the retries must stop being watched")
	}
	if n := strings.Count(logs.String(), "is collected, but set"); n != 1 {
		t.Fatalf("the misprediction must be reported once per address, got %d lines in %q", n, logs.String())
	}
}

func TestAnAddressCollectedAgainBeforeItsRemovalIsPublishedIsNotInstalledAgain(t *testing.T) {
	consumer := readyConsumer(true)
	producer := readyProducer()
	consumer.Escalate.To = producer.Id
	r := newCollectReadyRig(t, consumer, producer)
	resets := r.resetLog()
	r.w.destState.SetEscalation("i.ytimg.com", producer.Id, escalateReasonStall, time.Hour)
	logs := captureLog(t)
	addr := netip.MustParseAddr("1.2.3.4")

	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	r.silent(t, 42001)
	r.sweep(5 * time.Second)
	if r.installs != 1 || !slices.Equal(resets(), []uint16{42000, 42001}) {
		t.Fatalf("setup: the first pair must collect the address: installs=%d resets=%v", r.installs, resets())
	}

	install, save := r.p.collectedInstall, true
	r.p.collectedInstall = func(ctx context.Context, view *config.Config, current func() bool) error {
		if save {
			save = false
			r.p.configMu.Lock()
			n := *r.p.collectedBase
			r.p.configMu.Unlock()
			if err := r.p.UpdateConfig(&n); err != nil {
				t.Fatal(err)
			}
		}
		return install(ctx, view, current)
	}
	r.p.configMu.Lock()
	expiring := r.p.collected[addr]
	expiring.at = r.start.Add(10 * time.Second).Add(-config.CollectedIPTTL)
	r.p.collected[addr] = expiring
	r.p.configMu.Unlock()
	r.sweep(10 * time.Second)
	r.p.configMu.Lock()
	_, pooled := r.p.collected[addr]
	r.p.configMu.Unlock()
	if pooled || !slices.Equal(r.p.publishedCollected(), []string{"1.2.3.4"}) {
		t.Fatalf("setup: a save during the expiry's install must leave the address out of the pool and still published: pooled=%v published=%v", pooled, r.p.publishedCollected())
	}

	r.silent(t, 42002)
	r.silent(t, 42003)
	r.sweep(14 * time.Second)
	r.p.configMu.Lock()
	again, pooled := r.p.collected[addr]
	r.p.configMu.Unlock()
	if !pooled || !slices.Equal(r.p.publishedCollected(), []string{"1.2.3.4"}) {
		t.Fatalf("setup: the next pair must collect the address again before its removal reached the firewall: pooled=%v published=%v", pooled, r.p.publishedCollected())
	}
	if again.published == 0 {
		t.Fatal("an address the commit leaves published must carry its publication mark, even when the published list did not change")
	}

	installs, builds := r.installs, matcherBuilds(logs.String())
	r.silent(t, 42004)
	r.sweep(20 * time.Second)
	if r.installs != installs || matcherBuilds(logs.String()) != builds {
		t.Fatalf("a later connection to the published address was installed again: installs %d->%d, matcher builds %d->%d", installs, r.installs, builds, matcherBuilds(logs.String()))
	}
	if ports := resets(); slices.Contains(ports, 42004) {
		t.Fatalf("a connection after the publication that still reached the producer must end without a reset, got resets to %v", ports)
	}
	if r.watched(42004) {
		t.Fatal("the later connection must stop being watched")
	}
}
