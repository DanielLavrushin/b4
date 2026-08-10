package nfq

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func newCatchAllUDPSet(tcpFilter string) config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "catch-all-udp"
	set.Name = "UDP"
	set.Enabled = true
	set.Targets.SNIDomains = []string{"regexp:.*"}
	set.Targets.IPs = []string{"0.0.0.0/0"}
	set.TCP.DPortFilter = tcpFilter
	set.UDP.DPortFilter = "443"
	set.UDP.FilterQUIC = "all"
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	set.Faking.SNI = false
	set.Faking.SNIMutation.Mode = config.ConfigOff
	set.TCP.Desync.Mode = config.ConfigOff
	set.TCP.Win.Mode = config.ConfigOff
	set.TCP.DropSACK = false
	return set
}

func nextConnectionLine(t *testing.T, ch chan string) []string {
	t.Helper()
	select {
	case line := <-ch:
		return strings.Split(line, ",")
	default:
		t.Fatal("expected a connection log line")
		return nil
	}
}

func TestTCPPortFilterExcludesSetFromConnectionLog(t *testing.T) {
	set := newCatchAllUDPSet("1")

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}
	if _, _, err := cfg.GetTargetsForSet(&set); err != nil {
		t.Fatalf("expand targets: %v", err)
	}
	cfg.BuildTCPPortMap()
	cfg.BuildSetPortRanges()

	w := newTestWorker(t, &cfg)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	if v := w.ProcessPacket(makeV4TCPPacket([]byte("not-tls"), 1000)); v != 0 {
		t.Fatalf("a set scoped to tcp port 1 must not process tcp:443, got verdict %v", v)
	}

	f := nextConnectionLine(t, ch)
	if len(f) < 7 {
		t.Fatalf("malformed connection line: %v", f)
	}
	if sniSet, ipSet := f[2], f[5]; sniSet != "" || ipSet != "" {
		t.Errorf("tcp:443 must not be attributed to a set scoped to tcp port 1, got sniSet=%q ipSet=%q", sniSet, ipSet)
	}
}

func TestTCPPortFilterMatchStillAttributesSet(t *testing.T) {
	set := newCatchAllUDPSet("443")

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}
	if _, _, err := cfg.GetTargetsForSet(&set); err != nil {
		t.Fatalf("expand targets: %v", err)
	}
	cfg.BuildTCPPortMap()
	cfg.BuildSetPortRanges()

	w := newTestWorker(t, &cfg)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	w.ProcessPacket(makeV4TCPPacket([]byte("not-tls"), 1000))

	f := nextConnectionLine(t, ch)
	if len(f) < 7 {
		t.Fatalf("malformed connection line: %v", f)
	}
	if f[5] != set.Name {
		t.Errorf("tcp:443 covered by the set port filter should report ipSet=%q, got %q", set.Name, f[5])
	}
}
