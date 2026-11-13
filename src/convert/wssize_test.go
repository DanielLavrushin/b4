package convert

import (
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
