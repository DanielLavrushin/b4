package tables

import (
	"fmt"
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
