package nfq

import (
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/warmtuple"
)

const bystanderName = "Bystander"

func newBystanderSet(mutate func(set *config.SetConfig)) config.SetConfig {
	set := newPassiveSet()
	set.Id, set.Name = "bystander", bystanderName
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = nil
	set.TCP.RSTProtection.Enabled = false
	if mutate != nil {
		mutate(&set)
	}
	return set
}

func listsTheAddress(set *config.SetConfig) {
	set.Targets.IpsToMatch = []string{"1.2.3.0/24", "2001:db8::/64"}
}

func mustWait(t *testing.T, q *verdictLog) {
	t.Helper()
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the name", got)
	}
}

type bystanderCase struct {
	name   string
	mutate func(set *config.SetConfig)
	teach  func(w *Worker, set *config.SetConfig)
}

func bystanderCases() []bystanderCase {
	named := func(set *config.SetConfig) { set.Targets.DomainsToMatch = []string{"other.example"} }
	return []bystanderCase{
		{name: "a listed address", mutate: listsTheAddress},
		{name: "a listed address with RST protection", mutate: func(set *config.SetConfig) {
			listsTheAddress(set)
			set.TCP.RSTProtection.Enabled = true
		}},
		{name: "every address", mutate: func(set *config.SetConfig) { set.Targets.IpsToMatch = []string{"0.0.0.0/0", "::/0"} }},
		{name: "a listed address and the same name", mutate: func(set *config.SetConfig) {
			listsTheAddress(set)
			set.Targets.DomainsToMatch = []string{"example.com", "ytimg.com"}
		}},
		{name: "a port filter", mutate: func(set *config.SetConfig) {
			set.TCP.DPortFilter = "443"
			set.TCPPortRanges = config.ParsePortRanges("443")
		}},
		{name: "a learned address", mutate: named, teach: func(w *Worker, set *config.SetConfig) {
			w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "www.other.example", set)
		}},
		{name: "a DNS answer", mutate: named, teach: func(w *Worker, set *config.SetConfig) {
			w.storeHostHint("10.0.0.1", "1.2.3.4", set, "www.other.example", "dns", 0, 0)
		}},
	}
}

func bystanderWorker(t *testing.T, bystander config.SetConfig, teach func(w *Worker, set *config.SetConfig), actingFirst bool) (*Worker, func() [][]byte) {
	t.Helper()
	acting := newFragmentingYTImagesSet()
	acting.Targets.DomainOnly = false
	sets := []*config.SetConfig{&bystander, &acting}
	if actingFirst {
		sets = []*config.SetConfig{&acting, &bystander}
	}
	w, sent := heldHelloWorker(t, sets...)
	if teach != nil {
		teach(w, &bystander)
	}
	return w, sent
}

func TestASetThatLeavesTheHelloAloneDoesNotKeepSegmentOneFromTheSetOfTheName(t *testing.T) {
	for _, tc := range bystanderCases() {
		t.Run(tc.name, func(t *testing.T) {
			holdHellosFor(t, time.Minute)
			w, sent := bystanderWorker(t, newBystanderSet(tc.mutate), tc.teach, true)
			ch := connectionLines(t)
			q := &verdictLog{}
			full, seg1, seg2 := splitHello("i.ytimg.com")

			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			mustWait(t, q)
			if row := lastConnectionRow(t, ch); row.sniSet != "" || row.ipSet != "" || row.meta != "" || row.domain != "" {
				t.Errorf("a segment that waits for its name has neither a set nor a name yet, got %+v", row)
			}
			queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
			wantVerdicts(t, q.list(), dropOf(2), dropOf(1))

			segs := sentData(sent())
			coversOnce(t, segs, splitSeq, full)
			pieces := 0
			for _, s := range segs {
				if inSegmentOne(s, splitSeq) {
					pieces++
				}
			}
			if pieces < 2 {
				t.Errorf("segment 1 left in %d piece(s), the set of the name must split it", pieces)
			}
			if row := lastConnectionRow(t, ch); row.sniSet != "YT images" || row.ipSet != "" || row.domain != "i.ytimg.com" {
				t.Errorf("segment 2 must show the set of the name alone, got %+v", row)
			}
			if n := w.pendingHello.holding.Load(); n != 0 {
				t.Errorf("%d holds left, want none", n)
			}
		})
	}
}

func TestListOrderDoesNotChangeWhoGetsSegmentOne(t *testing.T) {
	holdHellosFor(t, time.Minute)
	w, sent := bystanderWorker(t, newBystanderSet(listsTheAddress), nil, false)
	q := &verdictLog{}
	full, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	mustWait(t, q)
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), dropOf(2), dropOf(1))
	coversOnce(t, sentData(sent()), splitSeq, full)
}

func TestSegmentOneOfABystandersAddressIsHeldOverIPv6(t *testing.T) {
	holdHellosFor(t, time.Minute)
	w, sent := bystanderWorker(t, newBystanderSet(listsTheAddress), nil, true)
	q := &verdictLog{}
	full, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV6TCPPacket(seg1, splitSeq))
	mustWait(t, q)
	queueTo(w, q, 2, makeV6TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), dropOf(2), dropOf(1))
	coversOnce(t, sentData(sent()), splitSeq, full)
}

func TestAHelloTheBystanderKeepsLeavesUnchangedInOrder(t *testing.T) {
	for _, tc := range bystanderCases() {
		t.Run(tc.name, func(t *testing.T) {
			holdHellosFor(t, time.Minute)
			w, sent := bystanderWorker(t, newBystanderSet(tc.mutate), tc.teach, true)
			q := &verdictLog{}
			_, seg1, seg2 := splitHello("www.example.org")

			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			mustWait(t, q)
			queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
			wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
			if segs := sentData(sent()); len(segs) != 0 {
				t.Fatalf("%d segments injected for a hello no acting set takes", len(segs))
			}
		})
	}
}

func TestAHeldSegmentOneOfABystanderIsReleasedUnchanged(t *testing.T) {
	_, seg1, _ := splitHello("i.ytimg.com")
	for _, tc := range []struct {
		name string
		next []byte
	}{
		{"a retransmitted segment 1", makeV4TCPPacket(seg1, splitSeq)},
		{"a FIN", makeV4TCPPacketFlags(nil, splitSeq+splitCut, 0x11)},
		{"a reset", makeV4TCPPacketFlags(nil, splitSeq+splitCut, 0x14)},
		{"a bare ACK", makeV4TCPPacketFlags(nil, splitSeq+splitCut, 0x10)},
		{"a new connection on the tuple", makeV4TCPPacketFlags(nil, 90000, 0x02)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			holdHellosFor(t, time.Minute)
			w, sent := bystanderWorker(t, newBystanderSet(listsTheAddress), nil, true)
			q := &verdictLog{}
			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			mustWait(t, q)
			queueTo(w, q, 2, tc.next)
			wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
			if segs := sentData(sent()); len(segs) != 0 {
				t.Fatalf("%d segments injected before any name was seen", len(segs))
			}
			if n := w.pendingHello.holding.Load(); n != 0 {
				t.Fatalf("%d holds left, want none", n)
			}
		})
	}

	for what, isn := range map[string]uint32{"a SYN resent": splitSeq - 1, "a SYN of a new connection": 90000} {
		t.Run(what+" to a set that fakes the SYN", func(t *testing.T) {
			holdHellosFor(t, time.Minute)
			w, _ := bystanderWorker(t, newBystanderSet(func(set *config.SetConfig) {
				listsTheAddress(set)
				set.TCP.SynFake = true
			}), nil, true)
			q := &verdictLog{}
			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			mustWait(t, q)
			queueTo(w, q, 2, makeV4TCPPacketFlags(nil, isn, 0x02))
			wantVerdicts(t, q.list(), acceptOf(1), dropOf(2))
			if n := w.pendingHello.holding.Load(); n != 0 {
				t.Fatalf("%d holds left, want none", n)
			}
		})
	}

	t.Run("segment 2 never arrives", func(t *testing.T) {
		holdHellosFor(t, 100*time.Millisecond)
		w, sent := bystanderWorker(t, newBystanderSet(listsTheAddress), nil, true)
		q := &verdictLog{}
		queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
		mustWait(t, q)
		wantVerdicts(t, q.waitFor(1, 5*time.Second), acceptOf(1))
		time.Sleep(60 * time.Millisecond)
		wantVerdicts(t, q.list(), acceptOf(1))
		if segs := sentData(sent()); len(segs) != 0 {
			t.Fatalf("%d segments injected for a hello whose name never came", len(segs))
		}
	})

	t.Run("segment 2 arrives after the limit", func(t *testing.T) {
		holdHellosFor(t, 100*time.Millisecond)
		w, sent := bystanderWorker(t, newBystanderSet(listsTheAddress), nil, true)
		_, _, seg2 := splitHello("i.ytimg.com")
		q := &verdictLog{}
		queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
		mustWait(t, q)
		wantVerdicts(t, q.waitFor(1, 5*time.Second), acceptOf(1))
		queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
		wantVerdicts(t, q.list(), acceptOf(1), dropOf(2))
		coversOnce(t, sentData(sent()), splitSeq+splitCut, seg2)
	})

	t.Run("b4 stops", func(t *testing.T) {
		holdHellosFor(t, time.Minute)
		acting, bystander := newFragmentingYTImagesSet(), newBystanderSet(listsTheAddress)
		cfg := config.NewConfig()
		cfg.Sets = []*config.SetConfig{&acting, &bystander}
		w := newTestWorker(t, &cfg)
		q := &verdictLog{}
		queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
		mustWait(t, q)
		w.Stop()
		wantVerdicts(t, q.list(), acceptOf(1))
		queueTo(w, q, 2, makeV4TCPPacketFromPort(seg1, 5000, 23456, 0x18))
		wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	})
}

func TestAReloadWhileSegmentOneOfABystanderWaits(t *testing.T) {
	_, seg1, seg2 := splitHello("i.ytimg.com")
	for _, tc := range []struct {
		name string
		sets func() []*config.SetConfig
		want []heldVerdict
	}{
		{"takes the name away", func() []*config.SetConfig {
			acting, bystander := newFragmentingYTImagesSet(), newBystanderSet(listsTheAddress)
			acting.Targets.DomainsToMatch = []string{"else.example"}
			return []*config.SetConfig{&acting, &bystander}
		}, []heldVerdict{acceptOf(1), acceptOf(2)}},
		{"gives the bystander a strategy and the name", func() []*config.SetConfig {
			bystander := newFragmentingYTImagesSet()
			bystander.Id, bystander.Name = "bystander", bystanderName
			listsTheAddress(&bystander)
			return []*config.SetConfig{&bystander}
		}, []heldVerdict{dropOf(2), dropOf(1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			holdHellosFor(t, time.Minute)
			w, _ := bystanderWorker(t, newBystanderSet(listsTheAddress), nil, true)
			q := &verdictLog{}
			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			mustWait(t, q)
			cfg := config.NewConfig()
			cfg.Sets = tc.sets()
			cfg.ConfigPath = w.getConfig().ConfigPath
			matcher := buildMatcher(&cfg)
			w.cfg.Store(&cfg)
			w.matcher.Store(matcher)
			w.tlsCache.ForgetDecisions(matcher)
			queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
			wantVerdicts(t, q.list(), tc.want...)
		})
	}
}

func TestASetThatActsOnSegmentOneKeepsIt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(set *config.SetConfig)
	}{
		{"splits", func(set *config.SetConfig) { set.Fragmentation.Strategy = "tcp" }},
		{"sends fakes", func(set *config.SetConfig) { set.Faking.SNI = true }},
		{"strips SACK", func(set *config.SetConfig) { set.TCP.DropSACK = true }},
		{"duplicates", func(set *config.SetConfig) {
			set.TCP.Duplicate.Enabled = true
			set.TCP.Duplicate.Count = 1
		}},
		{"detects IP blocks", func(set *config.SetConfig) { set.TCP.IPBlockDetect.Enabled = true }},
		{"escalates", func(set *config.SetConfig) { set.Escalate.To = "yt-images" }},
		{"blocks", func(set *config.SetConfig) {
			set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeBlock
		}},
		{"routes through a proxy", func(set *config.SetConfig) {
			set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy
		}},
		{"routes through an interface", func(set *config.SetConfig) {
			set.Routing.Enabled, set.Routing.Mode, set.Routing.EgressInterface = true, config.RoutingModeInterface, "wan9"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			holdHellosFor(t, time.Minute)
			bystander := newBystanderSet(func(set *config.SetConfig) {
				listsTheAddress(set)
				tc.mutate(set)
			})
			w, _ := bystanderWorker(t, bystander, nil, true)
			q := &verdictLog{}
			_, seg1, _ := splitHello("i.ytimg.com")
			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			if got := q.list(); len(got) != 1 || got[0].id != 1 {
				t.Fatalf("verdicts %v, want one for segment 1 while it was handled", got)
			}
			if n := w.pendingHello.onceLen.Load(); n != 0 {
				t.Fatalf("segment 1 was attached as a hold %d times, want never", n)
			}
		})
	}
}

func TestAHelloHeldByIPPriorityStaysWithTheBystander(t *testing.T) {
	holdHellosFor(t, time.Minute)
	bystander := newBystanderSet(func(set *config.SetConfig) {
		listsTheAddress(set)
		set.Targets.IPPriority = true
	})
	w, sent := bystanderWorker(t, bystander, nil, false)
	lines := connectionLines(t)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	mustWait(t, q)
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	if segs := sentData(sent()); len(segs) != 0 {
		t.Fatalf("%d segments injected for a connection the bystander holds", len(segs))
	}
	if seen := drainLines(lines); rowsFor(seen, bystanderName) == 0 || rowsFor(seen, "YT images") != 0 {
		t.Fatalf("the holding set must show on the hello's row, got %v", seen)
	}
}

func TestANameSetThatLeavesTheHelloAloneWinsOverABystandersAddress(t *testing.T) {
	holdHellosFor(t, time.Minute)
	passive := newPassiveSet()
	passive.Targets.DomainOnly = false
	acting := newFragmentingYTImagesSet()
	acting.Id, acting.Name = "acting", "Acting"
	acting.Targets.DomainsToMatch = []string{"acting.example"}
	bystander := newBystanderSet(listsTheAddress)
	w, sent := heldHelloWorker(t, &passive, &acting, &bystander)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	mustWait(t, q)
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	if segs := sentData(sent()); len(segs) != 0 {
		t.Fatalf("%d segments injected for a set that does not inject", len(segs))
	}
}

func TestWithoutAnActingNameSetABystanderHoldsNothing(t *testing.T) {
	bystander := newBystanderSet(listsTheAddress)
	named := newPassiveSet()
	w, _ := heldHelloWorker(t, &named, &bystander)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")
	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	wantVerdicts(t, q.list(), acceptOf(1))
}

func TestB4sOwnTelegramConnectionIsNotHeldForAName(t *testing.T) {
	holdHellosFor(t, time.Minute)
	telegram := newBystanderSet(nil)
	acting := newFragmentingYTImagesSet()
	w, _ := heldHelloWorker(t, &acting, &telegram)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")
	w.dispatch(&verdictCtx{id: 1, q: q, telegramSet: &telegram, selfDial: true}, makeV4TCPPacket(seg1, splitSeq))
	wantVerdicts(t, q.list(), acceptOf(1))
}

func TestATelegramConnectionItsSetsPortsMissIsHeldLikeAnyOther(t *testing.T) {
	holdHellosFor(t, time.Minute)
	telegram := newBystanderSet(func(set *config.SetConfig) {
		set.TCP.DPortFilter = "8443"
		set.TCPPortRanges = config.ParsePortRanges("8443")
	})
	acting := newFragmentingYTImagesSet()
	w, _ := heldHelloWorker(t, &acting, &telegram)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")
	w.dispatch(&verdictCtx{id: 1, q: q, telegramSet: &telegram, selfDial: true}, makeV4TCPPacket(seg1, splitSeq))
	mustWait(t, q)
}

func TestNothingIsHeldOnAConnectionThatLeavesThroughATunnel(t *testing.T) {
	for name, mutate := range map[string]func(set *config.SetConfig){
		"a set that leaves the hello alone": func(set *config.SetConfig) {},
		"a set that duplicates": func(set *config.SetConfig) {
			set.TCP.Duplicate.Enabled, set.TCP.Duplicate.Count = true, 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			holdHellosFor(t, time.Minute)
			w, _ := bystanderWorker(t, newBystanderSet(func(set *config.SetConfig) {
				listsTheAddress(set)
				mutate(set)
			}), nil, true)
			ch := subscribeRows(t)
			q := &verdictLog{}
			_, seg1, _ := splitHello("i.ytimg.com")
			w.dispatch(&verdictCtx{id: 1, q: q, tunnel: "wg0"}, makeV4TCPPacket(seg1, splitSeq))
			wantVerdicts(t, q.list(), acceptOf(1))
			if row := lastConnectionRow(t, ch); row.ipSet != bystanderName || row.meta != "tunnel->wg0" {
				t.Errorf("the row names the set and the tunnel, got %+v", row)
			}
		})
	}
}

func TestB4sOwnReuseConnectionIsNotHeldForItsOwnName(t *testing.T) {
	holdHellosFor(t, time.Minute)
	holdLocalHellosFor(t, time.Minute)
	reused, acting := newBystanderSet(nil), newFragmentingYTImagesSet()
	reused.Targets.DomainsToMatch = []string{"ytimg.com"}
	acting.Targets.DomainsToMatch = []string{"acting.example"}
	w, _ := heldHelloWorker(t, &reused, &acting)
	w.warmFlows = warmtuple.NewFlows()
	release, err := w.warmFlows.Register(netip.MustParseAddrPort("10.0.0.1:12345"), netip.MustParseAddrPort("1.2.3.4:443"), w.getConfig(), &reused, "i.ytimg.com")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")
	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	wantVerdicts(t, q.list(), acceptOf(1))
}

func TestWhileSegmentOneWaitsTheConnectionIsCountedInNoSet(t *testing.T) {
	holdHellosFor(t, time.Minute)
	w, _ := bystanderWorker(t, newBystanderSet(listsTheAddress), nil, true)
	before := metrics.GetMetricsCollector().Totals()
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")
	queueTo(w, q, 1, tcpFlowPacket(seg1, net.IPv4(1, 2, 3, 77), freshPorts(1)))
	mustWait(t, q)
	if d := totalsDelta(before, metrics.GetMetricsCollector().Totals()); d.Conns != 1 || d.ConnsInSets != 0 {
		t.Errorf("a connection whose name is awaited belongs to no set yet, got %+v", d)
	}
}

func TestWhileSegmentOneWaitsTheBystanderWatchesTheReplies(t *testing.T) {
	holdHellosFor(t, time.Minute)
	w, _ := bystanderWorker(t, newBystanderSet(func(set *config.SetConfig) {
		listsTheAddress(set)
		set.TCP.RSTProtection.Enabled = true
	}), nil, true)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")
	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	mustWait(t, q)
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != "bystander" {
		t.Errorf("want the replies watched by the bystander, got %v", bound)
	}
}

func TestAnEscalationThatLandsAroundTheHoldSettlesSegmentOneOnce(t *testing.T) {
	_, seg1, _ := splitHello("i.ytimg.com")
	for name, mutate := range map[string]func(set *config.SetConfig){
		"splits":      func(set *config.SetConfig) { set.Fragmentation.Strategy = "tcp" },
		"leaves":      func(set *config.SetConfig) {},
		"duplicates":  func(set *config.SetConfig) { set.TCP.Duplicate.Enabled, set.TCP.Duplicate.Count = true, 1 },
		"blocks":      func(set *config.SetConfig) { set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeBlock },
		"is a proxy":  func(set *config.SetConfig) { set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy },
		"fakes a SYN": func(set *config.SetConfig) { set.TCP.SynFake = true },
	} {
		t.Run("the backup "+name, func(t *testing.T) {
			holdHellosFor(t, time.Minute)
			bystander := newBystanderSet(func(set *config.SetConfig) { set.Targets.DomainsToMatch = []string{"other.example"} })
			bystander.Escalate.To = ""
			backup := newBystanderSet(func(set *config.SetConfig) {
				set.Id, set.Name = "backup", "Backup"
				set.Targets.DomainsToMatch = []string{"backup.example"}
				mutate(set)
			})
			acting := newFragmentingYTImagesSet()
			w, _ := heldHelloWorker(t, &acting, &bystander, &backup)
			w.storeHostHint("10.0.0.1", "1.2.3.4", &bystander, "www.other.example", "dns", 0, 0)
			stop := make(chan struct{})
			flipped := make(chan struct{})
			go func() {
				defer close(flipped)
				for {
					select {
					case <-stop:
						return
					default:
						w.destState.SetEscalation("www.other.example", "backup", escalateReasonStall, time.Minute)
						w.destState.ClearEscalation("www.other.example")
					}
				}
			}()
			const flows = 200
			q := &verdictLog{}
			for i := uint32(0); i < flows; i++ {
				queueTo(w, q, i+1, makeV4TCPPacketFromPort(seg1, splitSeq, uint16(20000+i), 0x18))
			}
			close(stop)
			<-flipped
			w.pendingHello.ReleaseHolds()
			w.wg.Wait()
			ids := make([]uint32, 0, flows)
			for _, v := range q.list() {
				ids = append(ids, v.id)
			}
			slices.Sort(ids)
			if len(ids) != flows || len(slices.Compact(ids)) != flows {
				t.Errorf("%d verdicts for %d segments, want one each", len(ids), flows)
			}
		})
	}
}

func TestTUNNeverHoldsASegmentOneOfABystander(t *testing.T) {
	w, _ := bystanderWorker(t, newBystanderSet(listsTheAddress), nil, true)
	_, seg1, _ := splitHello("i.ytimg.com")
	w.ProcessPacket(makeV4TCPPacket(seg1, splitSeq))
	if n := w.pendingHello.onceLen.Load(); n != 0 {
		t.Fatalf("%d holds attached without a queue, want none", n)
	}
}
