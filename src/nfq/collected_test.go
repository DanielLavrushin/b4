package nfq

import (
	"context"
	"errors"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/sni"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func collectedRig(t *testing.T, install func(context.Context, *config.Config) error) (*Worker, *Pool, func() [][]byte) {
	t.Helper()
	w, p, sent := unseededCollectedRig(t, install)
	seedEarlierSilentConnection(w, net.ParseIP("1.2.3.4"), 443)
	return w, p, sent
}

func seedEarlierSilentConnection(w *Worker, dst net.IP, port uint16) {
	w.destState.watchMu.Lock()
	defer w.destState.watchMu.Unlock()
	if w.destState.collectRuns == nil {
		w.destState.collectRuns = make(map[string]silentRun)
	}
	w.destState.collectRuns[iphealth.BanKey(dst, int(port))] = silentRun{count: 1, until: time.Now().Add(w.getConfig().System.IPHealth.RetestInterval())}
}

func unseededCollectedRig(t *testing.T, install func(context.Context, *config.Config) error, extra ...*config.SetConfig) (*Worker, *Pool, func() [][]byte) {
	t.Helper()
	producer := silenceSet()
	producer.TCP.IPBlockDetect.Action = "collect"
	consumer := newPassiveSet()
	consumer.Id = "arbitrary-consumer"
	consumer.Name = "Any name"
	consumer.Targets.DomainOnly = false
	consumer.Targets.DomainsToMatch = nil
	consumer.Targets.UseCollectedIPs = true
	consumer.Targets.IPPriority = true
	w := newInjectingWorker(t, append([]*config.SetConfig{&consumer, &producer}, extra...)...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &Pool{Workers: []*Worker{w}, state: &runtimeState{tlsCache: w.tlsCache}, collectedBase: w.getConfig(), collectedCtx: ctx, collectedCancel: cancel, collectedInstall: oneStepInstaller(install)}
	w.pool = p
	old := lookupFlow
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
		f := silentFlow().flow
		f.ID = 71
		return f, true, nil
	}
	t.Cleanup(func() { lookupFlow = old })
	return w, p, captureInjected(t, w)
}

func TestCollectSaysOnceThatClosingFakesHideTheServer(t *testing.T) {
	r := newCollectCountRig(t)
	producer := r.w.getConfig().Sets[1]
	producer.TCP.Desync.Mode = "rst"
	logs := captureLog(t)
	closedAtTheDeadline := func(sport uint16, after time.Duration) {
		t.Helper()
		r.w.ProcessPacket(silentHelloFrom(sport))
		if !r.watched(sport) {
			t.Fatalf("the ClientHello from port %d is not watched, so this test proves nothing", sport)
		}
		r.serverSends(t, sport, 0x10, nil)
		r.flow(t, sport).TCPState = 7
		r.sweep(after)
	}
	notes := func() int {
		n := 0
		for _, line := range strings.Split(logs.String(), "\n") {
			if strings.Contains(line, producer.Name) && strings.Contains(line, "close their conntrack entry") {
				n++
			}
		}
		return n
	}

	closedAtTheDeadline(42000, 4*time.Second)
	closedAtTheDeadline(42001, 5*time.Second)
	if got := notes(); got != 1 {
		t.Fatalf("two connections whose entry the set's fakes closed must be reported once at Info, got %d lines in %q", got, logs.String())
	}
	if r.collected() != 0 || r.installs != 0 {
		t.Fatalf("a closed entry is no evidence: candidates=%d installs=%d", r.collected(), r.installs)
	}
	for _, port := range resetsTo(r.sent()) {
		if port == 42000 || port == 42001 {
			t.Fatalf("a client whose entry was closed must not be reset, got resets to %v", resetsTo(r.sent()))
		}
	}

	producer.TCP.Desync.Mode = "fin"
	closedAtTheDeadline(42002, 6*time.Second)
	if got := notes(); got != 2 {
		t.Fatalf("a change of the desync mode must report the set again, got %d lines in %q", got, logs.String())
	}
}

func TestCollectedHelloPublishesBeforeResetAndNextSYNMatches(t *testing.T) {
	installed := false
	w, p, sent := collectedRig(t, func(ctx context.Context, cfg *config.Config) error {
		if len(cfg.Sets[0].Targets.CollectedIPs) != 1 {
			t.Fatal("installer did not receive candidate")
		}
		installed = true
		return nil
	})
	if got := w.ProcessPacket(silentHelloFrom(42000)); got != engine.VerdictAccept {
		t.Fatalf("Collect changed producer verdict: %v", got)
	}
	if !w.destState.Watching() {
		t.Fatal("missing TLS watch")
	}
	sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
	if !installed || len(p.collected) != 1 || len(resetsTo(sent())) != 1 {
		t.Fatalf("publication/reset missing: install=%v candidates=%d reset=%v", installed, len(p.collected), resetsTo(sent()))
	}
	yes, set := w.getMatcher().MatchIPWithSource(net.ParseIP("1.2.3.4"), "")
	if !yes || set.Id != "arbitrary-consumer" {
		t.Fatalf("next SYN cannot select consumer: %v", set)
	}
	if w.destState.IsBlocked("1.2.3.4:443") || banEnforced(w.getConfig().Sets[1]) {
		t.Fatal("Collect inherited Block enforcement")
	}
}

func TestCollectedFailureProgressReplacementAndNoConsumerNeverReset(t *testing.T) {
	for _, name := range []string{"installation-failure", "progress", "replacement", "no-consumer", "server-data", "cancel"} {
		t.Run(name, func(t *testing.T) {
			var w *Worker
			w, p, sent := collectedRig(t, func(ctx context.Context, cfg *config.Config) error {
				switch name {
				case "installation-failure":
					return errors.New("kernel failure")
				case "progress":
					w.destState.NoteClientPacket("10.0.0.1:42000->1.2.3.4:443", silentServerAck+300, true, false, time.Now(), nil)
				case "replacement":
					w.destState.ForgetConnection("10.0.0.1:42000->1.2.3.4:443")
				}
				return nil
			})
			if name == "no-consumer" {
				p.collectedBase.Sets[0].Enabled = false
			}
			w.ProcessPacket(silentHelloFrom(42000))
			w.destState.watchMu.RLock()
			var key string
			for k := range w.destState.watches {
				key = k
			}
			w.destState.watchMu.RUnlock()
			if key == "" {
				t.Fatal("watch not created")
			}
			if name == "progress" || name == "replacement" {
				p.collectedInstall = oneStepInstaller(func(context.Context, *config.Config) error {
					if name == "progress" {
						w.destState.NoteClientPacket(key, silentServerAck+300, true, false, time.Now(), nil)
					} else {
						w.destState.ForgetConnection(key)
					}
					return nil
				})
			}
			if name == "server-data" {
				lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
					f := silentFlow().flow
					f.ID = 71
					f.ReplyBytes = 9000
					return f, true, nil
				}
			}
			if name == "cancel" {
				p.collectedCancel()
			}

			sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
			if len(resetsTo(sent())) != 0 {
				t.Fatalf("unsafe reset after %s", name)
			}
			if len(p.collected) != 0 || len(p.collectedPublished) != 0 {
				t.Fatalf("unconfirmed candidate retained after %s", name)
			}
			if name == "installation-failure" {
				if yes, _ := w.getMatcher().MatchIP(net.ParseIP("1.2.3.4")); yes {
					t.Fatal("failed installation published early membership")
				}
			}
		})
	}
}

func TestCollectedBoundsAndAddressEligibility(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "::1", "224.0.0.1", "0.0.0.0"} {
		if _, ok := eligibleCollectedIP(net.ParseIP(raw)); ok {
			t.Fatalf("ineligible candidate %s", raw)
		}
	}
	installs := 0
	w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { installs++; return nil })
	p.collected = make(map[netip.Addr]collectedCandidate)
	for i := 0; i < maxCollectedIPs; i++ {
		p.collected[netip.AddrFrom4([4]byte{8, 1, byte(i >> 8), byte(i)})] = collectedCandidate{}
	}
	w.ProcessPacket(silentHelloFrom(42000))
	if !w.destState.Watching() {
		t.Fatal("missing TLS watch, so this test proves nothing")
	}
	sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
	if _, added := p.collected[netip.MustParseAddr("1.2.3.4")]; added || len(p.collected) != maxCollectedIPs || installs != 0 || len(resetsTo(sent())) != 0 {
		t.Fatalf("pool bound exceeded: added=%v candidates=%d installs=%d resets=%v", added, len(p.collected), installs, resetsTo(sent()))
	}
}

func TestCollectedMatcherPublicationKeepsExistingConnectionPolicy(t *testing.T) {
	old := sni.NewSuffixSet(nil)
	next := sni.NewSuffixSet(nil)
	if next.SameConnectionPolicy(old) {
		t.Fatal("ordinary config change kept old policy")
	}
	next.KeepConnectionPolicy(old)
	if !next.SameConnectionPolicy(old) {
		t.Fatal("membership addition changed established policy")
	}
}

func TestCollectedExistingFlowKeepsProducer(t *testing.T) {
	w, _, _ := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	w.ProcessPacket(silentHelloFrom(41000))
	key := ""
	w.destState.watchMu.RLock()
	for k, h := range w.destState.watches {
		if h.cport == 41000 {
			key = k
		}
	}
	w.destState.watchMu.RUnlock()
	if key == "" {
		t.Fatal("missing active connection")
	}
	w.destState.NoteClientPacket(key, silentServerAck+1000, true, false, time.Now(), nil)
	seedEarlierSilentConnection(w, net.ParseIP("1.2.3.4"), 443)
	w.ProcessPacket(silentHelloFrom(42000))
	sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
	decision, ok := w.tlsCache.Decision(key)
	if !ok || !decision.collectedPinned || decision.set == nil || decision.set.Id != "yt-images" {
		t.Fatalf("active connection ownership lost: %+v", decision)
	}
	w.ProcessPacket(clientPacketAcking(41000, afterHello(), silentServerAck+1000, 0x18, tlsAppData(80)))
	decision, _ = w.tlsCache.Decision(key)
	if decision.set == nil || decision.set.Id != "yt-images" {
		t.Fatal("pool publication hijacked an active TLS connection")
	}
}

func TestCollectedServerPacketUsesWireTupleAndCleansAlias(t *testing.T) {
	tracker := &destStateTracker{}
	h := &helloWatch{collect: true, key: "lan", replyKey: "wire"}
	tracker.WatchHello(h)
	tcp := make([]byte, TCPHeaderMinLen)
	tcp[12] = 0x50
	tcp[13] = 0x10
	tracker.NoteCollectedServerPacket("wire", tcp, 0, 52)
	if !tracker.Watching() {
		t.Fatal("an ACK alone ended the silent-server watch")
	}
	tracker.NoteCollectedServerPacket("wire", tcp, 1, 53)
	if tracker.Watching() || len(tracker.replyWatches) != 0 {
		t.Fatal("server progress left a watch or translated alias behind")
	}
}

func TestCollectReadsConntrackOutsideTheWatchLock(t *testing.T) {
	w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	lookups, locked := 0, []int(nil)
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
		lookups++
		if w.destState.watchMu.TryLock() {
			w.destState.watchMu.Unlock()
		} else {
			locked = append(locked, lookups)
		}
		f := silentFlow().flow
		f.ID = 71
		return f, true, nil
	}
	w.ProcessPacket(silentHelloFrom(42000))
	if !w.destState.Watching() {
		t.Fatal("missing TLS watch, so this test proves nothing")
	}
	sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
	if len(locked) != 0 {
		t.Fatalf("conntrack lookups %v of %d ran while the watch lock was held, which stalls every queue worker behind netlink", locked, lookups)
	}
	if len(p.collected) != 1 || len(resetsTo(sent())) != 1 {
		t.Fatalf("the silent connection must still be collected and reset: candidates=%d resets=%v", len(p.collected), resetsTo(sent()))
	}
}

func TestCollectedRequiresEstablishedTCP(t *testing.T) {
	w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
		flow := silentFlow().flow
		flow.ID = 71
		flow.TCPState = 1
		return flow, true, nil
	}
	w.ProcessPacket(silentHelloFrom(42000))
	sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
	if len(p.collected) != 0 || len(resetsTo(sent())) != 0 {
		t.Fatal("TCP unreachability became a TLS-stall candidate")
	}
}
