package convert

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const wssizeProfile = "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit "

func TestZapret_WSSizeBecomesTheRealTCPWindow(t *testing.T) {
	tests := []struct {
		name   string
		token  string
		window int
		scale  int
		status Status
		reason string
	}{
		{"windowAndScale", "--wssize=1:6", 1, 6, StatusMapped, "wsSizeMapped"},
		{"windowOnly", "--wssize=64", 64, 0, StatusApproximated, "wsSizeApproximated"},
		{"scaleZero", "--wssize=64:0", 64, 0, StatusApproximated, "wsSizeApproximated"},
		{"scaleAboveTheLimit", "--wssize=1:20", 1, config.MaxWSSizeScale, StatusMapped, "wsSizeMapped"},
		{"windowAboveTheLimit", "--wssize=70000:6", config.MaxWSSizeWindow, 6, StatusMapped, "wsSizeMapped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, wssizeProfile+tt.token)
			tcp := res.Sets[0].TCP
			if !tcp.WSSize.Enabled || tcp.WSSize.Window != tt.window || tcp.WSSize.Scale != tt.scale || tcp.WSSize.Cutoff != "" {
				t.Fatalf("tcp.wssize=%+v, want it on with window %d, scale %d and the cutoff after the ClientHello, where zapret stops too",
					tcp.WSSize, tt.window, tt.scale)
			}
			if tcp.Win.Mode != config.DefaultSetConfig.TCP.Win.Mode {
				t.Fatalf("tcp.win.mode=%q: fake ACKs with a zero window stood in for the option before b4 had it", tcp.Win.Mode)
			}
			n := noteFor(t, res, tt.token)
			if n.Status != tt.status || n.Reason != tt.reason || !hasField(n, "tcp.wssize.enabled") || !hasField(n, "tcp.wssize.window") {
				t.Fatalf("got %+v", n)
			}
			if got := hasField(n, "tcp.wssize.scale"); got != (tt.scale > 0) {
				t.Fatalf("the note names the scale only when b4 lowers it: %+v", n)
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}
}

func TestZapret_WSSizeStaysOffNextToPacketDuplication(t *testing.T) {
	for _, options := range []string{"--wssize=1:6 --dup=2", "--dup=2 --wssize=1:6"} {
		t.Run(options, func(t *testing.T) {
			res := analyze(t, wssizeProfile+options)
			tcp := res.Sets[0].TCP
			if tcp.WSSize.Enabled || !tcp.Duplicate.Enabled {
				t.Fatalf("b4 refuses to save an enabled set with both, so the import keeps the duplication: wssize=%+v duplicate=%+v",
					tcp.WSSize, tcp.Duplicate)
			}
			if n := noteFor(t, res, "--wssize=1:6"); n.Status != StatusUnsupported || n.Reason != "wsSizeWithDuplicate" {
				t.Fatalf("got %+v", n)
			}
			if !res.Sets[0].Enabled {
				t.Fatal("the set must be enabled for the save check below to mean anything")
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}
}

func TestZapret_WSSizeOffAndItsCutoffOptions(t *testing.T) {
	tests := []struct {
		name    string
		options string
		token   string
		enabled bool
		status  Status
		reason  string
	}{
		{"windowZero", "--wssize=0", "--wssize=0", false, StatusMapped, "wsSizeOff"},
		{"windowZeroWithAScale", "--wssize=0:6", "--wssize=0:6", false, StatusMapped, "wsSizeOff"},
		{"cutoffByPacketCount", "--wssize=1:6 --wssize-cutoff=n3", "--wssize-cutoff=n3", true, StatusUnsupported, "wsSizeCutoffUnsupported"},
		{"forcedCutoffOn", "--wssize=1:6 --wssize-forced-cutoff=1", "--wssize-forced-cutoff=1", true, StatusMapped, "wsSizeForcedCutoffOn"},
		{"forcedCutoffOff", "--wssize=1:6 --wssize-forced-cutoff=0", "--wssize-forced-cutoff=0", true, StatusUnsupported, "wsSizeForcedCutoffOff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, wssizeProfile+tt.options)
			if got := res.Sets[0].TCP.WSSize.Enabled; got != tt.enabled {
				t.Fatalf("tcp.wssize.enabled=%v, want %v", got, tt.enabled)
			}
			if n := noteFor(t, res, tt.token); n.Status != tt.status || n.Reason != tt.reason {
				t.Fatalf("got %+v", n)
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}
}

func TestZapret_AUDPProfileCarriesNoTCPOptions(t *testing.T) {
	defaults := config.DefaultSetConfig
	profiles := []struct{ name, line string }{
		{"udpPorts", "--filter-udp=443 --hostlist-domains=example.com"},
		{"callsOnly", "--filter-udp=50000-50100 --filter-l7=discord,stun"},
	}
	options := []string{"--wssize=1:6", "--dup=2", "--dpi-desync-split-seqovl=4"}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			res := analyze(t, p.line+" --dpi-desync=fake,rst "+strings.Join(options, " "))
			set := res.Sets[0]
			if !set.Enabled {
				t.Fatal("the set must be enabled, or the options below would do nothing whatever the import wrote")
			}
			if set.TCP.WSSize.Enabled || set.TCP.Duplicate.Enabled || set.TCP.Desync.Mode != defaults.TCP.Desync.Mode ||
				set.Fragmentation.SeqOverlapLength != defaults.Fragmentation.SeqOverlapLength {
				t.Fatalf("nfqws denies TCP to a profile that filters only UDP, and a b4 set is not limited to one protocol: "+
					"wssize=%+v duplicate=%v desync=%q seq_overlap_length=%d", set.TCP.WSSize, set.TCP.Duplicate.Enabled,
					set.TCP.Desync.Mode, set.Fragmentation.SeqOverlapLength)
			}
			for _, token := range options {
				if n := noteFor(t, res, token); n.Status != StatusDegenerate || n.Reason != "tcpOnlyOption" {
					t.Errorf("%s: got %s/%s", token, n.Status, n.Reason)
				}
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}

	res := analyze(t, "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit --new "+
		"--filter-udp=443 --dpi-desync=fake --wssize=1:6")
	if len(res.Sets) != 1 || res.Sets[0].TCP.WSSize.Enabled {
		t.Fatalf("a UDP profile folded into a TCP set brings no TCP window with it: %d sets, wssize=%+v", len(res.Sets), res.Sets[0].TCP.WSSize)
	}
}
