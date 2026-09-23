package detector

import (
	"context"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

func stubGateway(t *testing.T, terminated map[string]bool) *[]string {
	t.Helper()
	orig := netprobe.GatewayProbe
	var probed []string
	netprobe.GatewayProbe = func(_ context.Context, ip string, port int, _ int, _ time.Duration) bool {
		probed = append(probed, ip)
		return terminated[ip] && port == 443
	}
	t.Cleanup(func() { netprobe.GatewayProbe = orig })
	return &probed
}

func TestDirectTLSDropOnAGatewayTerminatedAddressBecomesGateway(t *testing.T) {
	probed := stubGateway(t, map[string]bool{"31.13.72.60": true})
	s := &Suite{ctx: context.Background(), directMark: 0x40000, Options: normalizeOptions(Options{})}

	st, detail := s.classifyFailure(context.Background(), context.DeadlineExceeded, netprobe.StageHandshake, "31.13.72.60", "443", s.directMark)
	if st != netprobe.DomainGateway || detail != netprobe.GatewayDetail {
		t.Fatalf("direct handshake drop on a gateway-terminated address = %s (%s), want GATEWAY", st, detail)
	}

	st, _ = s.classifyFailure(context.Background(), context.DeadlineExceeded, netprobe.StageHandshake, "157.240.253.60", "443", s.directMark)
	if st != netprobe.DomainTLSDrop {
		t.Fatalf("an address the gateway does not terminate keeps its own status, got %s", st)
	}

	st, _ = s.classifyFailure(context.Background(), context.DeadlineExceeded, netprobe.StageHandshake, "31.13.72.60", "443", markThroughB4)
	if st != netprobe.DomainTLSDrop {
		t.Fatalf("the through-b4 fetch must not be reclassified, got %s", st)
	}
	if len(*probed) != 2 {
		t.Fatalf("only direct fetches probe the gateway, probed %v", *probed)
	}

	if !isBlockedStatus(netprobe.DomainGateway) {
		t.Fatal("GATEWAY is a blocked status, otherwise the site would count as working")
	}
}

func TestGatewayIsCountedInBlockKinds(t *testing.T) {
	direct := &Fetch{Status: netprobe.DomainGateway, Detail: netprobe.GatewayDetail, IP: "31.13.72.60"}
	through := &Fetch{Status: netprobe.DomainTLSDrop, IP: "31.13.72.60"}
	s := &Suite{
		ctx:        context.Background(),
		directMark: 0x40000,
		Options:    normalizeOptions(Options{Sites: []string{"www.whatsapp.com"}, Scopes: []Scope{ScopeSites}}),
		Sites: &SitesResult{Sites: []SiteResult{{
			Input: "www.whatsapp.com", Domain: "www.whatsapp.com", URL: "https://www.whatsapp.com/",
			Direct: direct, ThroughB4: through, Outcome: outcomeFor(direct, through), Done: true,
		}}},
	}

	s.refreshVerdict()

	if s.Verdict.BlockKinds["GATEWAY"] != 1 {
		t.Fatalf("BlockKinds = %v, want GATEWAY counted once", s.Verdict.BlockKinds)
	}
	if s.Verdict.BlockedByISP != 0 || s.Verdict.StillBlocked != 0 || s.Verdict.Gateway != 1 || len(s.Verdict.StillBlockedAt) != 0 {
		t.Fatalf("a gateway-terminated site is neither blocked by the ISP nor something Discovery from this host can fix, it gets its own count: %+v", s.Verdict)
	}
}
