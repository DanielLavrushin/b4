package nfq

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/iphealth"
)

func newEscalateWorker() *Worker {
	return &Worker{
		destState: newDestState(),
		goodIPs:   iphealth.NewKnownGood(),
		hostHints: newHostHintCache(),
		ipHealth:  iphealth.NewTracker(nil),
	}
}

func escalatePair(t *testing.T) (*config.Config, *config.SetConfig, *config.SetConfig) {
	t.Helper()
	primary := config.NewSetConfig()
	primary.Id = "primary"
	primary.Name = "primary"
	primary.Enabled = true
	primary.Escalate.To = "backup"

	backup := config.NewSetConfig()
	backup.Id = "backup"
	backup.Name = "backup"
	backup.Enabled = true

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&primary, &backup}
	return &cfg, &primary, &backup
}

func TestTryEscalate_SwitchesToTheBackupSet(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)

	next := w.tryEscalate(cfg, primary, "ntc.party", "", net.ParseIP("1.2.3.4"), escalateReasonDNS)
	if next == nil || next.Id != backup.Id {
		t.Fatalf("expected escalation to %q, got %v", backup.Id, next)
	}
	if got := w.destState.GetEscalationReason("ntc.party"); got != escalateReasonDNS {
		t.Fatalf("reason not recorded, got %q", got)
	}
}

func TestTryEscalate_SkipsDisabledTarget(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)
	backup.Enabled = false

	if next := w.tryEscalate(cfg, primary, "ntc.party", "", nil, escalateReasonDNS); next != nil {
		t.Fatalf("a disabled backup must not be escalated to, got %q", next.Name)
	}
	if _, _, ok := w.destState.GetEscalation("ntc.party"); ok {
		t.Fatal("no escalation state should be stored for a disabled backup")
	}
}

func TestEscalatedSetFor_ClearsWhenTargetDisappears(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, _ := escalatePair(t)
	w.tryEscalate(cfg, primary, "ntc.party", "", nil, escalateReasonDNS)

	cfg.Sets = cfg.Sets[:1]
	if got := w.escalatedSetFor(cfg, "ntc.party", ""); got != nil {
		t.Fatalf("expected no set once the target is gone, got %q", got.Name)
	}
	if _, _, ok := w.destState.GetEscalation("ntc.party"); ok {
		t.Fatal("a dangling escalation must be dropped on lookup")
	}
}

func TestEscalatedSetFor_HonoursSourceDeviceFilter(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)
	backup.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	w.tryEscalate(cfg, primary, "ntc.party", "AA:BB:CC:DD:EE:FF", nil, escalateReasonDNS)

	if got := w.escalatedSetFor(cfg, "ntc.party", "11:22:33:44:55:66"); got != nil {
		t.Fatalf("a device outside the backup's filter must not be escalated, got %q", got.Name)
	}
	if got := w.escalatedSetFor(cfg, "ntc.party", "AA:BB:CC:DD:EE:FF"); got == nil {
		t.Fatal("the device the backup targets must be escalated")
	}
}

func TestTryEscalate_SkipsBackupThatDoesNotTargetTheDevice(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)
	backup.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}

	if next := w.tryEscalate(cfg, primary, "ntc.party", "11:22:33:44:55:66", nil, escalateReasonDNS); next != nil {
		t.Fatalf("a backup that does not target the device must not take over, got %q", next.Name)
	}
	if _, _, ok := w.destState.GetEscalation("ntc.party"); ok {
		t.Fatal("no escalation state should be stored when the backup cannot serve the device")
	}
}

func TestTryEscalate_ExcludeFilterStillCoversUnknownDevices(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)
	backup.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	backup.Targets.SourceDevicesExclude = true

	if next := w.tryEscalate(cfg, primary, "ntc.party", "", nil, escalateReasonDNS); next == nil {
		t.Fatal("a backup that excludes one device must still serve the rest")
	}
}

func TestEscalateOnStall_UsesEscalateThresholdNotIPBlockDetect(t *testing.T) {
	set := config.NewSetConfig()
	set.Escalate.To = "backup"
	set.Escalate.StallThreshold = 5
	set.TCP.IPBlockDetect.RetransmitThreshold = 2

	now := time.Now()
	if escalateOnStall(&set.Escalate, 2, now) {
		t.Fatal("2 retransmits must not trip a stall threshold of 5")
	}
	if !escalateOnStall(&set.Escalate, 5, now) {
		t.Fatal("5 retransmits must trip a stall threshold of 5")
	}
}

func TestEscalateOnStall_TimeoutPathNeedsMoreThanOneHello(t *testing.T) {
	set := config.NewSetConfig()
	set.Escalate.To = "backup"
	set.Escalate.StallTimeoutMs = 10

	stale := time.Now().Add(-time.Second)
	if escalateOnStall(&set.Escalate, 1, stale) {
		t.Fatal("a single ClientHello must never trip the timeout path")
	}
	if !escalateOnStall(&set.Escalate, 2, stale) {
		t.Fatal("a retransmit older than the timeout must trip")
	}
}

func TestEscalateOnStall_InactiveSetNeverTrips(t *testing.T) {
	set := config.NewSetConfig()
	if escalateOnStall(&set.Escalate, 100, time.Now().Add(-time.Hour)) {
		t.Fatal("a set with no escalate target must never trip")
	}
}

func TestHostAddresses_IncludesKnownGoodIPs(t *testing.T) {
	w := newEscalateWorker()
	w.goodIPs.Remember("ntc.party", net.ParseIP("130.255.77.28"))

	ips := w.hostAddresses("ntc.party", net.ParseIP("1.2.3.4"))
	if len(ips) != 2 {
		t.Fatalf("expected the destination plus the known-good address, got %v", ips)
	}
	if !ips[0].Equal(net.ParseIP("1.2.3.4")) {
		t.Fatalf("the destination must come first, got %v", ips[0])
	}
}

func TestHostAddresses_DoesNotDuplicateTheDestination(t *testing.T) {
	w := newEscalateWorker()
	w.goodIPs.Remember("ntc.party", net.ParseIP("1.2.3.4"))

	if ips := w.hostAddresses("ntc.party", net.ParseIP("1.2.3.4")); len(ips) != 1 {
		t.Fatalf("expected one address, got %v", ips)
	}
}

func TestRegisterEscalatedRoute_SkipsDomainOnlySets(t *testing.T) {
	prevSync, prevAsync := RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc
	defer func() {
		RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc = prevSync, prevAsync
	}()

	calls := 0
	RoutingHandleDNSAsyncFunc = func(*config.Config, *config.SetConfig, []net.IP) { calls++ }
	RoutingHandleDNSFunc = nil

	set := config.NewSetConfig()
	set.Enabled = true
	set.Routing.Enabled = true
	set.Targets.DomainOnly = true

	registerEscalatedRoute(&config.Config{}, &set, []net.IP{net.ParseIP("1.2.3.4")})
	if calls != 0 {
		t.Fatalf("a domain-only set carries no ipset to fill, got %d calls", calls)
	}
}

func TestShouldRefreshRoute_ThrottlesRepeatHits(t *testing.T) {
	b := newDestState()
	b.SetEscalation("ntc.party", "backup", escalateReasonDNS, 0)

	if !b.ShouldRefreshRoute("ntc.party", time.Minute) {
		t.Fatal("the first hit must register the route")
	}
	if b.ShouldRefreshRoute("ntc.party", time.Minute) {
		t.Fatal("a second hit inside the interval must not re-register")
	}
	if !b.ShouldRefreshRoute("ntc.party", time.Nanosecond) {
		t.Fatal("a hit past the interval must re-register")
	}
}

func TestShouldRefreshRoute_IgnoresUnknownHost(t *testing.T) {
	b := newDestState()
	if b.ShouldRefreshRoute("ntc.party", time.Minute) {
		t.Fatal("a host with no escalation has no route to refresh")
	}
}

func TestShouldLogHopCap_ThrottlesPerHost(t *testing.T) {
	b := newDestState()
	b.SetEscalation("ntc.party", "backup", escalateReasonStall, 0)

	if !b.ShouldLogHopCap("ntc.party") {
		t.Fatal("the first hop-cap report must be logged")
	}
	if b.ShouldLogHopCap("ntc.party") {
		t.Fatal("repeat reports inside the interval must stay quiet")
	}
}

func TestPruneEscalations_DropsOnlyInvalidTargets(t *testing.T) {
	b := newDestState()
	b.SetEscalation("keep.example", "live", escalateReasonRST, 0)
	b.SetEscalation("drop.example", "gone", escalateReasonRST, 0)

	b.PruneEscalations(func(setId string) bool { return setId == "live" })

	if _, _, ok := b.GetEscalation("keep.example"); !ok {
		t.Fatal("an escalation pointing at a live set must survive a config reload")
	}
	if _, _, ok := b.GetEscalation("drop.example"); ok {
		t.Fatal("an escalation pointing at a vanished set must be pruned")
	}
}

func TestRecordDNSFailure_TripsAtThreshold(t *testing.T) {
	b := newDestState()
	if b.RecordDNSFailure("ntc.party", 2, time.Minute) {
		t.Fatal("the first failure must not trip a threshold of 2")
	}
	if !b.RecordDNSFailure("ntc.party", 2, time.Minute) {
		t.Fatal("the second failure must trip a threshold of 2")
	}
}

func TestRecordDNSFailure_ThresholdOfOneTripsImmediately(t *testing.T) {
	b := newDestState()
	if !b.RecordDNSFailure("ntc.party", 1, time.Minute) {
		t.Fatal("a threshold of 1 must trip on the first failure")
	}
}

func TestRecordDNSFailure_ClearedBySuccess(t *testing.T) {
	b := newDestState()
	b.RecordDNSFailure("ntc.party", 2, time.Minute)
	b.ClearDNSFailures("ntc.party")
	if b.RecordDNSFailure("ntc.party", 2, time.Minute) {
		t.Fatal("a good answer must reset the counter")
	}
}

func TestDNSAnswerFailed(t *testing.T) {
	aQuery := dns.BuildQuery("ntc.party", 1, 1)
	aaaaQuery := dns.BuildQuery("ntc.party", 1, 28)

	cases := []struct {
		name string
		resp []byte
		want bool
	}{
		{"empty response", nil, true},
		{"nxdomain", dns.BuildBlockResponse(aQuery), true},
		{"servfail", dns.BuildServfailResponse(aQuery), true},
		{"answered A", dns.BuildAnswerFromIPs(aQuery, 60, []net.IP{net.ParseIP("1.2.3.4")}), false},
		{"answered AAAA", dns.BuildAnswerFromIPs(aaaaQuery, 60, []net.IP{net.ParseIP("2001:db8::1")}), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dnsAnswerFailed(tc.resp); got != tc.want {
				t.Fatalf("dnsAnswerFailed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDNSAnswerFailed_EmptyNoErrorOnlyCountsForA(t *testing.T) {
	empty := func(qtype uint16) []byte {
		resp := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, qtype))
		resp[3] &= 0xF0
		return resp
	}

	if !dnsAnswerFailed(empty(1)) {
		t.Fatal("an A answer with no addresses is a failure")
	}
	if dnsAnswerFailed(empty(28)) {
		t.Fatal("an empty AAAA answer is normal for IPv4-only hosts and must not count")
	}
}

func TestDNSAnswerFailed_IgnoresOtherQuestionTypes(t *testing.T) {
	https := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, 65))
	if dnsAnswerFailed(https) {
		t.Fatal("escalation must only judge A and AAAA questions")
	}
}

func TestNoteDNSOutcome_EscalatesAfterThreshold(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)
	primary.Escalate.DNSThreshold = 2

	nx := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, 1))

	if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nx); next != nil {
		t.Fatal("the first NXDOMAIN must not escalate at a threshold of 2")
	}
	next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nx)
	if next == nil || next.Id != backup.Id {
		t.Fatalf("the second NXDOMAIN must escalate to the backup, got %v", next)
	}
}

func TestNoteDNSOutcome_GoodAnswerResetsTheCounter(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, _ := escalatePair(t)
	primary.Escalate.DNSThreshold = 2

	query := dns.BuildQuery("ntc.party", 1, 1)
	nx := dns.BuildBlockResponse(query)
	good := dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP("1.2.3.4")})

	w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nx)
	w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, good)
	if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nx); next != nil {
		t.Fatal("a good answer in between must reset the failure run")
	}
}

func TestNoteDNSOutcome_GoodAAAADoesNotClearAFailures(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)
	primary.Escalate.DNSThreshold = 2

	emptyA := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, dnsTypeA))
	emptyA[3] &= 0xF0
	goodAAAA := dns.BuildAnswerFromIPs(dns.BuildQuery("ntc.party", 1, dnsTypeAAAA), 60,
		[]net.IP{net.ParseIP("2a02:e00:ffec:4b8::1")})

	w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, emptyA)
	w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeAAAA, goodAAAA)
	next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, emptyA)

	if next == nil || next.Id != backup.Id {
		t.Fatal("an IPv6-only host answers AAAA fine; that must not wipe the A-record failure run a dual-stack client produces")
	}
}

func TestNoteDNSOutcome_OtherQuestionTypesAreNeutral(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)
	primary.Escalate.DNSThreshold = 2

	emptyA := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, dnsTypeA))
	emptyA[3] &= 0xF0
	https := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, 65))
	https[3] &= 0xF0

	w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, emptyA)
	if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", 65, https); next != nil {
		t.Fatal("an HTTPS/SVCB answer must not count as a failure of its own")
	}
	if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, emptyA); next == nil || next.Id != backup.Id {
		t.Fatal("an HTTPS/SVCB answer in between must not clear the A-record failure run either")
	}
}

func TestNoteDNSOutcome_GoodAnswerClearsItsOwnFamily(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, _ := escalatePair(t)
	primary.Escalate.DNSThreshold = 2

	query := dns.BuildQuery("ntc.party", 1, dnsTypeA)
	nx := dns.BuildBlockResponse(query)
	good := dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP("1.2.3.4")})

	w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nx)
	w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, good)
	if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nx); next != nil {
		t.Fatal("a good A answer must reset the A-record failure run")
	}
}

func TestNoteDNSOutcome_UpstreamErrorOnANonAddressQueryIsIgnored(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, _ := escalatePair(t)
	primary.Escalate.DNSThreshold = 2

	// an upstream outage fails every query type; only address lookups may count
	for i := 0; i < 6; i++ {
		if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", 65, nil); next != nil {
			t.Fatal("a failed HTTPS/SVCB lookup must never escalate on its own")
		}
	}
	if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nil); next != nil {
		t.Fatal("failed HTTPS/SVCB lookups must not have been counted into the A-record run")
	}
}

func TestNoteDNSOutcome_UpstreamErrorOnAnAddressQueryCounts(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, backup := escalatePair(t)
	primary.Escalate.DNSThreshold = 2

	if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nil); next != nil {
		t.Fatal("the first upstream error must not trip a threshold of 2")
	}
	next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nil)
	if next == nil || next.Id != backup.Id {
		t.Fatalf("a second failed A lookup must escalate, got %v", next)
	}
}

func TestNoteDNSOutcome_UpstreamErrorKeepsFamiliesApart(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, _ := escalatePair(t)
	primary.Escalate.DNSThreshold = 2

	w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nil)
	if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeAAAA, nil); next != nil {
		t.Fatal("one failed A and one failed AAAA are one failure each, not two of either")
	}
}

func TestNoteDNSOutcome_ChainTerminatesAtTheHopCap(t *testing.T) {
	w := newEscalateWorker()
	cfg := config.NewConfig()

	// a long chain with a threshold of 1 makes every level trip in one pass,
	// which is the shape that drives the inline resolve into recursion
	const links = MaxEscalationHops + 5
	sets := make([]*config.SetConfig, links)
	for i := range sets {
		s := config.NewSetConfig()
		s.Id = fmt.Sprintf("set-%d", i)
		s.Name = s.Id
		s.Enabled = true
		s.Escalate.DNSThreshold = 1
		if i+1 < links {
			s.Escalate.To = fmt.Sprintf("set-%d", i+1)
		}
		sets[i] = &s
		cfg.Sets = append(cfg.Sets, &s)
	}

	hops := 0
	set := sets[0]
	for i := 0; i < links*2; i++ {
		next := w.noteDNSOutcome(&cfg, set, "ntc.party", "", dnsTypeA, nil)
		if next == nil {
			break
		}
		hops++
		set = next
	}

	if hops == 0 {
		t.Fatal("the chain should have escalated at least once")
	}
	if hops > MaxEscalationHops {
		t.Fatalf("the chain ran %d hops, past the cap of %d; an inline resolve would recurse that deep", hops, MaxEscalationHops)
	}
}

func TestDNSQueryType_FallsBackToTheResponse(t *testing.T) {
	resp := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, dnsTypeAAAA))
	if got := dnsQueryType(nil, resp); got != dnsTypeAAAA {
		t.Fatalf("expected the question type to be read off the response, got %d", got)
	}
	query := dns.BuildQuery("ntc.party", 1, dnsTypeA)
	if got := dnsQueryType(query, resp); got != dnsTypeA {
		t.Fatalf("the query must win when it is readable, got %d", got)
	}
	if got := dnsQueryType(nil, nil); got != 0 {
		t.Fatalf("expected 0 when nothing is readable, got %d", got)
	}
}

func TestClassifyDNSAnswer(t *testing.T) {
	aQuery := dns.BuildQuery("ntc.party", 1, dnsTypeA)
	emptyAAAA := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, dnsTypeAAAA))
	emptyAAAA[3] &= 0xF0

	cases := []struct {
		name string
		resp []byte
		want dnsVerdict
	}{
		{"no response at all", nil, dnsVerdictFailed},
		{"nxdomain", dns.BuildBlockResponse(aQuery), dnsVerdictFailed},
		{"servfail", dns.BuildServfailResponse(aQuery), dnsVerdictFailed},
		{"answered A", dns.BuildAnswerFromIPs(aQuery, 60, []net.IP{net.ParseIP("1.2.3.4")}), dnsVerdictGood},
		{"empty AAAA", emptyAAAA, dnsVerdictGood},
		{"HTTPS question", dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, 65)), dnsVerdictIgnore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyDNSAnswer(tc.resp); got != tc.want {
				t.Fatalf("classifyDNSAnswer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNoteDNSOutcome_IgnoresSetsWithoutATarget(t *testing.T) {
	w := newEscalateWorker()
	cfg, primary, _ := escalatePair(t)
	primary.Escalate.To = ""

	nx := dns.BuildBlockResponse(dns.BuildQuery("ntc.party", 1, 1))
	for i := 0; i < 5; i++ {
		if next := w.noteDNSOutcome(cfg, primary, "ntc.party", "", dnsTypeA, nx); next != nil {
			t.Fatal("a set with no escalate target must never escalate")
		}
	}
}

func TestSynTrackingEnabled(t *testing.T) {
	plain := config.NewSetConfig()
	if synTrackingEnabled(&plain) {
		t.Fatal("a plain set needs no SYN health tracking")
	}

	escalating := config.NewSetConfig()
	escalating.Escalate.To = "backup"
	if !synTrackingEnabled(&escalating) {
		t.Fatal("an escalating set must track SYN health even with IP block detect off")
	}

	detecting := config.NewSetConfig()
	detecting.TCP.IPBlockDetect.Enabled = true
	detecting.TCP.IPBlockDetect.SynDetect = true
	if !synTrackingEnabled(&detecting) {
		t.Fatal("IP block detect with SYN detect must still track")
	}
}

func TestRouteRefreshInterval_HalvesTheIPSetTTL(t *testing.T) {
	set := config.NewSetConfig()
	set.Routing.IPTTLSeconds = 600
	if got := routeRefreshInterval(&set); got != 300*time.Second {
		t.Fatalf("expected half the ipset TTL, got %s", got)
	}

	set.Routing.IPTTLSeconds = 10
	if got := routeRefreshInterval(&set); got != 30*time.Second {
		t.Fatalf("a short TTL must still be floored at 30s, got %s", got)
	}
}
