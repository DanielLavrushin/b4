package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/sni"
)

func countCollectedTakes(t *testing.T) *int {
	t.Helper()
	calls := 0
	old := collectedTakes
	t.Cleanup(func() { collectedTakes = old })
	collectedTakes = func(cfg *config.Config, matcher *sni.SuffixSet, h *helloWatch) bool {
		calls++
		return old(cfg, matcher, h)
	}
	return &calls
}

func TestABlockWatchAsksNoCollectedConsumerWhenNoSetUsesCollectedIPs(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)
	calls := countCollectedTakes(t)
	sendHello(t, r.w, 12345, engine.VerdictAccept)
	if !r.w.destState.Watching() {
		t.Fatal("setup: the ClientHello to a public address is not watched")
	}
	if *calls != 0 {
		t.Fatalf("with no set using collected IPs, a watched ClientHello still looked for a set using collected IPs that takes it: %d lookups", *calls)
	}
}

func TestABlockWatchStillAsksAnEnabledCollectedConsumer(t *testing.T) {
	set := silenceSet()
	consumer := collectedConsumerSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)
	cfg := *r.w.getConfig()
	cfg.Sets = append(append(cfg.Sets[:0:0], cfg.Sets...), &consumer)
	r.w.cfg.Store(&cfg)
	r.w.matcher.Store(buildMatcher(&cfg))
	calls := countCollectedTakes(t)
	sendHello(t, r.w, 12345, engine.VerdictAccept)
	if !r.w.destState.Watching() {
		t.Fatal("setup: the ClientHello to a public address is not watched")
	}
	if *calls != 1 {
		t.Fatalf("with an enabled set using collected IPs, a watched ClientHello must be checked once against it, checked %d times", *calls)
	}
}
