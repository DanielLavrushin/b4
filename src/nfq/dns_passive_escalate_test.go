package nfq

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/sni"
)

func passiveDNSPair(t *testing.T, threshold int) (*config.Config, *config.SetConfig, *config.SetConfig) {
	t.Helper()

	primary := config.NewSetConfig()
	primary.Id = "primary"
	primary.Name = "primary"
	primary.Enabled = true
	primary.Targets.DomainsToMatch = []string{"youtube.com"}
	primary.Escalate.To = "backup"
	primary.Escalate.DNSThreshold = threshold

	backup := config.NewSetConfig()
	backup.Id = "backup"
	backup.Name = "backup"
	backup.Enabled = true
	backup.Targets.DomainsToMatch = []string{"youtube.com"}

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&primary, &backup}
	return &cfg, &primary, &backup
}

func passiveDNSWorker(t *testing.T, cfg *config.Config) *Worker {
	t.Helper()

	health := iphealth.NewTracker(func(string, uint16, int) bool { return false })
	t.Cleanup(health.Stop)

	w := &Worker{
		destState: newDestState(),
		goodIPs:   iphealth.NewKnownGood(),
		hostHints: newHostHintCache(),
		ipHealth:  health,
	}
	w.cfg.Store(cfg)
	w.matcher.Store(sni.NewSuffixSet(cfg.Sets))
	w.ipToMac.Store(make(map[string]string))
	return w
}

func feedDNSResponse(t *testing.T, w *Worker, payload []byte) *verdictCtx {
	t.Helper()

	server := net.ParseIP("192.168.1.1").To4()
	client := net.ParseIP("192.168.1.100").To4()
	pkt := &pktInfo{
		ver:    IPv4,
		proto:  17,
		src:    server,
		dst:    client,
		srcStr: server.String(),
		dstStr: client.String(),
	}

	vc := &verdictCtx{verdict: engine.VerdictAccept}
	w.processDnsPacket(vc, pkt, 53, 40000, payload)
	return vc
}

func TestPassiveDNSStripKeepsFeedingTheEscalationCounter(t *testing.T) {
	cfg, primary, _ := passiveDNSPair(t, 2)
	w := passiveDNSWorker(t, cfg)

	nx := dns.BuildBlockResponse(dns.BuildQuery("youtube.com", 1, dnsTypeAAAA))
	if next := w.noteDNSOutcome(cfg, primary, "youtube.com", "", dnsTypeAAAA, nx); next != nil {
		t.Fatal("the first failure must not escalate at a threshold of 2")
	}

	answer := buildTestMixedResponse("youtube.com", dnsTypeAAAA, "2a00:1450:4010:c0d::5d")
	vc := feedDNSResponse(t, w, answer)
	if vc.verdict != engine.VerdictDrop {
		t.Fatalf("verdict = %v, want drop: b4 answers the stripped response itself", vc.verdict)
	}

	if next := w.noteDNSOutcome(cfg, primary, "youtube.com", "", dnsTypeAAAA, nx); next != nil {
		t.Fatalf("the answer that got its AAAA records stripped must still reset the failure run, escalated to %s", next.Name)
	}
}

func TestPassiveDNSEscalationRunsBeforeTheAnswerIsRewritten(t *testing.T) {
	cfg, _, backup := passiveDNSPair(t, 1)
	backup.DNS.Pins = map[string][]string{"youtube.com": {"142.250.74.14"}}
	w := passiveDNSWorker(t, cfg)

	nx := dns.BuildBlockResponse(dns.BuildQuery("youtube.com", 0x1234, dnsTypeA))
	vc := feedDNSResponse(t, w, nx)

	if vc.verdict != engine.VerdictDrop {
		t.Fatalf("verdict = %v, want drop: the escalated set answered the client", vc.verdict)
	}
	escId, _, ok := w.destState.GetEscalation("youtube.com")
	if !ok || escId != backup.Id {
		t.Fatalf("escalation = %q ok=%v, want %q", escId, ok, backup.Id)
	}
	if reason := w.destState.GetEscalationReason("youtube.com"); reason != escalateReasonDNS {
		t.Fatalf("reason = %q, want %q", reason, escalateReasonDNS)
	}
}
