package nfq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

const (
	lifetimeOwnedMac = "aa:aa:aa:aa:aa:01"
	lifetimeOtherMac = "aa:aa:aa:aa:aa:02"
)

type lifetimeRig struct {
	*collectCountRig
	views  []*config.Config
	failed bool
	resets func() []uint16
}

func newLifetimeRig(t *testing.T, consumer *config.SetConfig) *lifetimeRig {
	t.Helper()
	if consumer == nil {
		consumer = readyConsumer(true)
	}
	consumer.Targets.SourceDevices = []string{lifetimeOwnedMac}
	r := &lifetimeRig{collectCountRig: newCollectReadyRig(t, consumer, readyProducer())}
	r.resets = r.resetLog()
	r.p.collectedInstall = oneStepInstaller(func(_ context.Context, view *config.Config) error {
		r.installs++
		r.views = append(r.views, view)
		if r.failed {
			return errors.New("kernel refused the update")
		}
		return nil
	})
	r.device(lifetimeOwnedMac)
	return r
}

func (r *lifetimeRig) device(mac string) {
	r.w.ipToMac.Store(map[string]string{"10.0.0.1": mac})
}

func (r *lifetimeRig) collect(t *testing.T) {
	t.Helper()
	installs := r.installs
	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	r.silent(t, 42001)
	r.sweep(5 * time.Second)
	r.p.configMu.Lock()
	_, pooled := r.p.collected[netip.MustParseAddr("1.2.3.4")]
	r.p.configMu.Unlock()
	if !pooled || r.installs != installs+1 || !slices.Equal(r.resets(), []uint16{42000, 42001}) {
		t.Fatalf("setup: two silent connections must collect 1.2.3.4: pooled=%v installs=%d resets=%v", pooled, r.installs, r.resets())
	}
}

func (r *lifetimeRig) expireAt(after time.Duration) {
	r.p.configMu.Lock()
	defer r.p.configMu.Unlock()
	for addr, candidate := range r.p.collected {
		candidate.at = r.start.Add(after).Add(-config.CollectedIPTTL)
		r.p.collected[addr] = candidate
	}
}

func (r *lifetimeRig) lastView(t *testing.T) []string {
	t.Helper()
	if len(r.views) == 0 {
		t.Fatal("no install ran")
	}
	return r.views[len(r.views)-1].Sets[0].Targets.CollectedIPs
}

func (r *lifetimeRig) consumerMatches(addr string) bool {
	matched, set := r.w.getMatcher().MatchIPWithSource(net.ParseIP(addr), lifetimeOwnedMac)
	return matched && set.Targets.UseCollectedIPs
}

func TestACollectedAddressExpiresAfterTheTTL(t *testing.T) {
	r := newLifetimeRig(t, nil)
	r.collect(t)
	entries := r.p.GetCollectedIPs()
	if len(entries) != 1 || entries[0].Until != entries[0].Since+config.CollectedIPTTL.Milliseconds() {
		t.Fatalf("the entry must carry its expiry, %v after it was collected: %+v", config.CollectedIPTTL, entries)
	}

	r.sweep(5*time.Second + config.CollectedIPTTL - time.Second)
	if r.collected() != 1 || r.installs != 1 {
		t.Fatalf("the address left the pool before its expiry: candidates=%d installs=%d", r.collected(), r.installs)
	}
	r.sweep(5*time.Second + config.CollectedIPTTL)

	if r.collected() != 0 || r.installs != 2 || len(r.lastView(t)) != 0 {
		t.Fatalf("the address must leave the pool through one install once its time is up: candidates=%d installs=%d view=%v", r.collected(), r.installs, r.lastView(t))
	}
	if len(r.p.GetCollectedIPs()) != 0 || r.consumerMatches("1.2.3.4") {
		t.Fatal("the expired address is still listed or still selects the consumer")
	}
}

func TestALateAnswerDropsTheAddress(t *testing.T) {
	for name, answer := range map[string]func(*testing.T, *lifetimeRig){
		"the server sent data the queue saw": func(t *testing.T, r *lifetimeRig) {
			r.hello(t, 42010)
			r.serverSends(t, 42010, 0x18, []byte{0x16, 3, 3, 0, 1, 2})
		},
		"the client acknowledged server bytes": func(t *testing.T, r *lifetimeRig) {
			r.silent(t, 42010)
			r.w.ProcessPacket(clientPacketAcking(42010, afterHello(), silentServerAck+1400, 0x10, nil))
		},
		"conntrack shows server bytes the queue did not see": func(t *testing.T, r *lifetimeRig) {
			r.silent(t, 42010)
			f := r.flow(t, 42010)
			f.ReplyPackets++
			f.ReplyBytes += 1400
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newLifetimeRig(t, nil)
			r.collect(t)
			r.device(lifetimeOtherMac)
			answer(t, r)
			r.sweep(7 * time.Second)

			if r.collected() != 0 || r.installs != 2 || len(r.lastView(t)) != 0 {
				t.Fatalf("a connection of the collecting set that the server answered must drop the address: candidates=%d installs=%d view=%v", r.collected(), r.installs, r.lastView(t))
			}
			if r.consumerMatches("1.2.3.4") || r.watched(42010) {
				t.Fatal("the dropped address still selects the consumer, or its connection stays watched")
			}
		})
	}
}

func TestAnAnswerOnAConsumerConnectionKeepsTheAddress(t *testing.T) {
	consumer := readyConsumer(true)
	consumer.TCP.RSTProtection.Enabled = false
	consumer.TCP.IPBlockDetect.Enabled = true
	consumer.TCP.IPBlockDetect.CacheBlockedIPs = true
	r := newLifetimeRig(t, consumer)
	r.collect(t)
	r.w.ProcessPacket(silentHelloFrom(42010))
	h := r.watchOf(t, 42010)
	if h.setID != consumer.Id || !h.consumer {
		t.Fatalf("setup: the retry must be watched as the consumer's own connection, got set %s consumer=%v", h.setID, h.consumer)
	}
	r.w.ProcessPacket(clientPacketAcking(42010, afterHello(), silentServerAck+1400, 0x10, nil))
	if r.watched(42010) {
		t.Fatal("setup: the answered consumer connection must end its watch")
	}
	r.sweep(7 * time.Second)

	if r.collected() != 1 || r.installs != 1 || !r.consumerMatches("1.2.3.4") {
		t.Fatalf("an answer the consumer got says nothing about the collecting set's path and must keep the address: candidates=%d installs=%d", r.collected(), r.installs)
	}
}

func TestExpiryAndAnswersShareOnePublish(t *testing.T) {
	r := newLifetimeRig(t, nil)
	r.p.configMu.Lock()
	r.p.collected = map[netip.Addr]collectedCandidate{}
	for _, raw := range []string{"5.6.7.8", "5.6.7.9"} {
		r.p.collected[netip.MustParseAddr(raw)] = collectedCandidate{setID: "yt-images", host: "seeded.example", port: 443, at: r.start.Add(10*time.Second - config.CollectedIPTTL)}
	}
	r.p.configMu.Unlock()
	if result := r.p.publishCollected(false); result.outcome != collectedApplied {
		t.Fatalf("setup: publishing the seeded addresses failed with outcome %d", result.outcome)
	}
	r.collect(t)
	if got := r.lastView(t); !slices.Equal(got, []string{"1.2.3.4", "5.6.7.8", "5.6.7.9"}) {
		t.Fatalf("setup: the collected address must join the seeded ones, got %v", got)
	}
	r.device(lifetimeOtherMac)
	r.hello(t, 42010)
	r.serverSends(t, 42010, 0x18, []byte{0x16, 3, 3, 0, 1, 2})
	r.sweep(10 * time.Second)

	if r.collected() != 0 || r.installs != 3 || len(r.lastView(t)) != 0 {
		t.Fatalf("two expired addresses and an answered one must leave in one install: candidates=%d installs=%d view=%v", r.collected(), r.installs, r.lastView(t))
	}
}

func TestARecollectionAfterExpiryNeedsTwoNewSilentConnections(t *testing.T) {
	r := newLifetimeRig(t, nil)
	r.collect(t)
	r.expireAt(10 * time.Second)
	r.sweep(10 * time.Second)
	if r.collected() != 0 || r.installs != 2 {
		t.Fatalf("setup: the address must expire: candidates=%d installs=%d", r.collected(), r.installs)
	}

	r.silent(t, 42002)
	r.sweep(11 * time.Second)
	if r.collected() != 0 || r.installs != 2 {
		t.Fatalf("one silent connection after the expiry must not collect the address again: candidates=%d installs=%d", r.collected(), r.installs)
	}
	r.silent(t, 42003)
	r.sweep(12 * time.Second)
	if r.collected() != 1 || r.installs != 3 || !slices.Equal(r.resets(), []uint16{42000, 42001, 42002, 42003}) {
		t.Fatalf("two new silent connections must collect it again and be reset: candidates=%d installs=%d resets=%v", r.collected(), r.installs, r.resets())
	}
}

func TestACountThatCompletesInTheExpiryPassDoesNotKeepTheAddress(t *testing.T) {
	consumer := readyConsumer(true)
	consumer.Escalate.To = readyProducer().Id
	r := newLifetimeRig(t, consumer)
	r.w.destState.SetEscalation("i.ytimg.com", consumer.Escalate.To, escalateReasonStall, time.Hour)
	r.collect(t)
	r.expireAt(10 * time.Second)
	r.silent(t, 42002)
	r.sweep(10 * time.Second)

	if r.collected() != 0 || r.installs != 2 || len(r.lastView(t)) != 0 || r.consumerMatches("1.2.3.4") {
		t.Fatalf("an address whose time is up must leave the firewall even when a count for it completes in the same pass: candidates=%d installs=%d view=%v", r.collected(), r.installs, r.lastView(t))
	}
	if r.watched(42002) || slices.Contains(r.resets(), 42002) {
		t.Fatal("the connection that completed a count for the expiring address must end without a reset")
	}
	r.silent(t, 42003)
	r.sweep(11 * time.Second)
	if r.collected() != 0 {
		t.Fatal("one silent connection after the expiry must not collect the address again")
	}
	r.silent(t, 42004)
	r.sweep(12 * time.Second)
	entries := r.p.GetCollectedIPs()
	if len(entries) != 1 || r.installs != 3 || entries[0].Since != r.start.Add(12*time.Second).UnixMilli() {
		t.Fatalf("two new silent connections must collect it again, with a new collection time: entries=%+v installs=%d", entries, r.installs)
	}
}

func TestAFailedRemovalPublishIsRetried(t *testing.T) {
	for name, drop := range map[string]func(*testing.T, *lifetimeRig){
		"expired": func(t *testing.T, r *lifetimeRig) { r.expireAt(10 * time.Second) },
		"answered": func(t *testing.T, r *lifetimeRig) {
			r.device(lifetimeOtherMac)
			r.hello(t, 42010)
			r.serverSends(t, 42010, 0x18, []byte{0x16, 3, 3, 0, 1, 2})
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newLifetimeRig(t, nil)
			r.collect(t)
			drop(t, r)
			r.failed = true
			r.sweep(10 * time.Second)
			failedAt := r.installs
			if failedAt < 2 || len(r.views[1].Sets[0].Targets.CollectedIPs) != 0 || r.collected() != 1 || !r.consumerMatches("1.2.3.4") {
				t.Fatalf("setup: the removal must be tried and fail, keeping the address installed: installs=%d candidates=%d", r.installs, r.collected())
			}
			r.failed = false
			r.sweep(10*time.Second + collectedRetryFirst/2)
			if r.installs != failedAt {
				t.Fatalf("a failed removal must wait for the retry delay, installs=%d after %d", r.installs, failedAt)
			}
			r.sweep(10*time.Second + collectedRetryFirst)

			if r.collected() != 0 || r.installs != failedAt+1 || len(r.lastView(t)) != 0 || r.consumerMatches("1.2.3.4") {
				t.Fatalf("the retry must still remove the address: candidates=%d installs=%d view=%v", r.collected(), r.installs, r.lastView(t))
			}
		})
	}
}

func TestAConnectionAnInterfaceConsumerTunnelsIsNotWatched(t *testing.T) {
	consumer := readyConsumer(false)
	consumer.Routing.Enabled, consumer.Routing.Mode, consumer.Routing.EgressInterface = true, config.RoutingModeInterface, "wg-collected"
	r := newLifetimeRig(t, consumer)
	r.collect(t)
	vc := &verdictCtx{verdict: engine.VerdictAccept, tunnel: "wg-collected"}
	r.w.dispatch(vc, silentHelloFrom(42010))
	if decision, ok := r.w.tlsCache.Decision(fmt.Sprintf(connKeyFormat, "10.0.0.1", 42010, "1.2.3.4", 443)); !ok || decision.set == nil || decision.set.Id != "yt-images" {
		t.Fatalf("setup: the collecting set names the server, so it decides the routed retry, got %+v", decision)
	}
	if r.watched(42010) {
		t.Fatal("a connection the interface consumer routes away must not be watched by the collecting set, or its answer would drop the address it was collected for")
	}
}

func TestAnInterfaceConsumersRoutedConnectionIsNotWatched(t *testing.T) {
	consumer := readyConsumer(false)
	consumer.Routing.Enabled, consumer.Routing.Mode, consumer.Routing.EgressInterface = true, config.RoutingModeInterface, "eth-collected"
	r := newLifetimeRig(t, consumer)
	r.collect(t)
	r.w.ProcessPacket(silentHelloFrom(42010))
	if decision, ok := r.w.tlsCache.Decision(fmt.Sprintf(connKeyFormat, "10.0.0.1", 42010, "1.2.3.4", 443)); !ok || decision.set == nil || decision.set.Id != "yt-images" {
		t.Fatalf("setup: the collecting set names the server, so it decides the routed retry, got %+v", decision)
	}
	if r.watched(42010) {
		t.Fatal("a connection the interface consumer routes out of a plain interface must not be watched by the collecting set, or its answer would drop the address it was collected for")
	}
	r.serverSends(t, 42010, 0x18, []byte{0x16, 3, 3, 0, 1, 2})
	r.sweep(10 * time.Second)
	if r.collected() != 1 || !r.consumerMatches("1.2.3.4") {
		t.Fatalf("an answer on a connection the consumer routes must keep the address: candidates=%d", r.collected())
	}

	r.device(lifetimeOtherMac)
	r.hello(t, 42011)
}

func TestCollectedPoolStringsStateTheExpiry(t *testing.T) {
	hours := int(config.CollectedIPTTL / time.Hour)
	if time.Duration(hours)*time.Hour != config.CollectedIPTTL {
		t.Fatalf("CollectedIPTTL is %v, not whole hours: the strings below state hours", config.CollectedIPTTL)
	}
	for locale, phrase := range map[string]string{"en": fmt.Sprintf("%d hours", hours), "ru": fmt.Sprintf("%d час", hours)} {
		raw, err := os.ReadFile("../http/ui/src/i18n/" + locale + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"sets.targets.useCollectedIpsDesc", "dashboard.collected.subtitle", "dashboard.collected.confirmBody"} {
			var node any = doc
			for _, part := range strings.Split(path, ".") {
				node, _ = node.(map[string]any)[part]
			}
			text, _ := node.(string)
			if !strings.Contains(text, phrase) {
				t.Errorf("%s %s must state the expiry %q from config.CollectedIPTTL, got %q", locale, path, phrase, text)
			}
		}
	}
}
