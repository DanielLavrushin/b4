package tables

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func ctmarkDump(restored, claimed uint64) string {
	return fmt.Sprintf(`Chain b4r_test_pre (1 references)
    pkts      bytes target     prot opt in     out     source               destination
       4      212 RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            mark match 0x8000/0x8000
      16    18670 RETURN     all  --  xray0  *       0.0.0.0/0            0.0.0.0/0
%[1]d     4000 CONNMARK   all  --  br0    *       0.0.0.0/0            0.0.0.0/0            connmark match  0x40000000/0x40000000 ctdir ORIGINAL ! ctstate NEW CONNMARK restore mask 0x27fff
%[2]d    19890 MARK       all  --  br0    *       0.0.0.0/0            0.0.0.0/0            ctstate NEW match-set b4r_test_v4 dst MARK xset 0x5179/0x27fff
%[2]d    19890 CONNMARK   all  --  br0    *       0.0.0.0/0            0.0.0.0/0            ctstate NEW match-set b4r_test_v4 dst CONNMARK or 0x40000000
%[2]d    19890 CONNMARK   all  --  br0    *       0.0.0.0/0            0.0.0.0/0            ctstate NEW match-set b4r_test_v4 dst CONNMARK save mask 0x27fff
`, restored, claimed)
}

func TestASilentConnectionMarkIsNoticed(t *testing.T) {
	claimed, ok := routeCountChainHits(ctmarkDump(0, 80), func(l string) bool {
		return strings.Contains(l, "CONNMARK") && strings.Contains(l, "ctstate NEW")
	})
	if !ok {
		t.Fatal("the claim rules must be found in real iptables output")
	}
	if claimed != 160 {
		t.Fatalf("two claim rules of 80 packets each add up to 160, got %d", claimed)
	}

	restored, ok := routeCountChainHits(ctmarkDump(0, 80), func(l string) bool {
		return strings.Contains(l, "CONNMARK") && strings.Contains(l, "restore")
	})
	if !ok || restored != 0 {
		t.Fatalf("the restore rule must be found and read as zero, got %d ok=%v", restored, ok)
	}
}

func TestAWorkingConnectionMarkIsLeftAlone(t *testing.T) {
	restored, ok := routeCountChainHits(ctmarkDump(1, 80), func(l string) bool {
		return strings.Contains(l, "CONNMARK") && strings.Contains(l, "restore")
	})
	if !ok || restored == 0 {
		t.Fatalf("a router that keeps the connection mark shows restores, got %d ok=%v", restored, ok)
	}
}

func TestTheFallbackIsOnlyEmittedWhenTheMarkIsNotHeld(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)

	be := &mockRouteBackend{}
	routeAddMarkFallbackRules(be, "b4r_test_pre", false, "b4r_test_v4", 0x5179, []string{"br0"})
	if len(be.chainOps["b4r_test_pre"]) != 0 {
		t.Fatalf("while the connection mark is trusted the routing decision is carried on the connection, so "+
			"marking every packet again would route a flow that was deliberately left alone: %v",
			be.chainOps["b4r_test_pre"])
	}

	routeCTMarkHeld.Store(false)
	be = &mockRouteBackend{}
	routeAddMarkFallbackRules(be, "b4r_test_pre", false, "b4r_test_v4", 0x5179, []string{"br0", "br1"})
	if got := len(be.chainOps["b4r_test_pre"]); got != 2 {
		t.Fatalf("want one fallback per source interface, got %d in %v", got, be.chainOps["b4r_test_pre"])
	}
}

func TestTheFallbackNeverOverwritesAMarkAlreadySet(t *testing.T) {
	cmds := stickyIptRules(t, func(be *routeIptBackend) {
		be.addMarkFallbackRule("b4r_test_pre", false, "b4r_test_v4", 0x5179, "br0")
	})
	c := cmds[0]
	if !strings.Contains(c, "-m mark --mark 0x0/0x27fff") {
		t.Fatalf("the fallback must only touch a packet the restore left unmarked, or it undoes the sticky "+
			"decision on every router where the connection mark does work: %q", c)
	}
	if !strings.Contains(c, "--match-set b4r_test_v4 dst") {
		t.Fatalf("the fallback must still be scoped to the set's destinations: %q", c)
	}
	if !strings.Contains(c, "-i br0") {
		t.Fatalf("the fallback must keep the set's source scope: %q", c)
	}
}

func TestOneQuietTickIsNotEnoughToGiveUpOnTheConnectionMark(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)

	if routeCTMarkSilent.Add(1) >= routeCTMarkConfirmations {
		t.Fatal("one look is not evidence: right after a restart every connection the set has claimed is still " +
			"in its handshake, so no packet has come back to be restored yet and a healthy router would be " +
			"mistaken for one that eats the mark")
	}
	if !routeCTMarkIsHeld() {
		t.Fatal("the mark must still be trusted after a single quiet look")
	}
}

func TestTheVerdictSurvivesARestart(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)

	dir := t.TempDir()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(dir, "config.json")

	routeLoadCTMarkVerdict(&cfg)
	if !routeCTMarkIsHeld() {
		t.Fatal("with no state on disk the connection mark starts out trusted")
	}

	routeCTMarkHeld.Store(false)
	routeSaveCTMarkVerdict()

	routeForgetCTMarkVerdict()
	routeLoadCTMarkVerdict(&cfg)
	if routeCTMarkIsHeld() {
		t.Fatal("whether the router keeps a connection mark is a property of its firmware, not of one b4 " +
			"process. Re-learning it after every restart costs every connection the set matches until the " +
			"check has enough packets to go on, which is the window the user hits on every deploy")
	}
}

func TestNoConfigPathMeansNoStateFile(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)

	cfg := config.NewConfig()
	cfg.ConfigPath = ""
	if p := routeCTMarkStatePath(&cfg); p != "" {
		t.Fatalf("without a config path there is nowhere safe to write, got %q", p)
	}
	routeLoadCTMarkVerdict(&cfg)
	routeSaveCTMarkVerdict()
	if !routeCTMarkIsHeld() {
		t.Fatal("a missing config path must leave the verdict alone rather than assume the worst")
	}
}

func TestTheFallbackReachesTheRoutersOwnTrafficToo(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)
	routeCTMarkHeld.Store(false)

	be := &mockRouteBackend{}
	cfg := familyTestConfig(true, false)
	set := familyTestSet()
	set.Routing.SourceInterfaces = nil
	set.Routing.RouterTraffic = config.RouterTrafficInclude

	st := buildRouteState(cfg, set)
	if !st.routerOut || st.srcScoped {
		t.Fatalf("this set must carry the router's own traffic for the case to exist: routerOut=%v srcScoped=%v",
			st.routerOut, st.srcScoped)
	}
	routeAddOutChainRules(be, cfg, st, routeSetDeviceGate(cfg, set))

	fallbacks := 0
	for _, op := range be.chainOps[st.chainOut] {
		if op == "fallback" {
			fallbacks++
		}
	}
	if fallbacks == 0 {
		t.Fatalf("on a router that does not keep the connection mark, the set marks the first packet the "+
			"router sends and restores nothing after it, so every packet but the first leaves by the ordinary "+
			"uplink while the connection was made through the set's interface, and the far end drops it. That "+
			"is the same split path the fallback exists to close on the forwarding side: %v",
			be.chainOps[st.chainOut])
	}
}

const conntrackWithTag = `ipv4     2 tcp      6 299 ESTABLISHED src=192.168.1.100 dst=1.2.3.4 sport=1 dport=443 packets=9 bytes=1 src=1.2.3.4 dst=192.168.1.100 sport=443 dport=1 packets=1 bytes=1 [ASSURED] mark=1073762169 use=2
ipv4     2 udp      17 27 src=192.168.1.100 dst=8.8.8.8 sport=2 dport=53 packets=1 bytes=1 mark=0 use=2
`

const conntrackWithoutTag = `ipv4     2 tcp      6 299 ESTABLISHED src=192.168.1.100 dst=1.2.3.4 sport=1 dport=443 packets=9 bytes=1 src=1.2.3.4 dst=192.168.1.100 sport=443 dport=1 packets=1 bytes=1 [ASSURED] mark=7545 use=2
ipv4     2 udp      17 27 src=192.168.1.100 dst=8.8.8.8 sport=2 dport=53 packets=1 bytes=1 mark=6248 use=2
`

func writeConntrack(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nf_conntrack")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := conntrackPath
	conntrackPath = p
	t.Cleanup(func() { conntrackPath = prev })
}

func TestAQuietRestoreRuleAloneIsNotEvidence(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)
	writeConntrack(t, conntrackWithTag)

	quiet := ctmarkDump(0, 80)
	routeCheckCTMarkIn(quiet)
	routeCheckCTMarkIn(quiet)
	routeCheckCTMarkIn(quiet)

	if !routeCTMarkIsHeld() {
		t.Fatal("a set whose destinations never answer, or that only carries one-packet flows like DNS, claims " +
			"connection after connection without a single later packet in the original direction. The restore " +
			"rule is quiet for a reason that has nothing to do with the router keeping the mark, and the " +
			"connection table still shows b4's claim, so switching to fallback here would put every " +
			"established flow back at risk of being re-evaluated mid-connection")
	}
}

func TestTheVerdictFlipsWhenTheClaimIsNotOnTheConnection(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)
	writeConntrack(t, conntrackWithoutTag)

	quiet := ctmarkDump(0, 80)
	routeCheckCTMarkIn(quiet)
	if !routeCTMarkIsHeld() {
		t.Fatal("one look is not enough")
	}
	routeCheckCTMarkIn(quiet)
	if routeCTMarkIsHeld() {
		t.Fatal("b4 claimed 80 connections and the connection table carries its tag on none of them, which is " +
			"positive evidence that something else owns the mark rather than an inference from silence")
	}
}

func TestAnUnreadableConnectionTableDecidesNothing(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)
	prev := conntrackPath
	conntrackPath = filepath.Join(t.TempDir(), "absent")
	t.Cleanup(func() { conntrackPath = prev })

	quiet := ctmarkDump(0, 80)
	for i := 0; i < 5; i++ {
		routeCheckCTMarkIn(quiet)
	}
	if !routeCTMarkIsHeld() {
		t.Fatal("without the connection table b4 cannot tell the two cases apart, and guessing wrong costs " +
			"every established flow, so it must leave the verdict alone")
	}
}

func TestTwoQuietSetsAreOneObservationNotTwo(t *testing.T) {
	routeForgetCTMarkVerdict()
	t.Cleanup(routeForgetCTMarkVerdict)
	writeConntrack(t, conntrackWithoutTag)

	quiet := parseIptDump(ctmarkDump(0, 80))
	two := map[string]iptChainInfo{
		"b4r_a_pre": quiet["b4r_test_pre"],
		"b4r_b_pre": quiet["b4r_test_pre"],
	}

	routeCheckCTMarkFromDump(two)
	if !routeCTMarkIsHeld() {
		t.Fatal("two routed sets read in the same monitor pass are one look at the router, not two. Counting " +
			"them separately lets a second set stand in for the second tick and settles the question inside a " +
			"single pass, right after a restart when every claimed connection is still in its handshake")
	}

	routeCheckCTMarkFromDump(two)
	if routeCTMarkIsHeld() {
		t.Fatal("a second pass is a genuine second look and must be allowed to settle it")
	}
}
