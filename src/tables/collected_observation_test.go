package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestCollectedReplyObservationUsesIndependentPacketWindow(t *testing.T) {
	for _, name := range []string{"collect", "block", "consumer", "discovery", "tun", "original", "no-packet-counters"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			producer := &config.SetConfig{Enabled: true}
			producer.TCP.IPBlockDetect.Enabled = true
			producer.TCP.IPBlockDetect.Action = "collect"
			cfg.Sets = []*config.SetConfig{producer}
			manager := &NFTablesManager{cfg: cfg, ctPacketsSupported: true}
			direction := "reply"
			switch name {
			case "block":
				producer.TCP.IPBlockDetect.Action = "block"
			case "consumer":
				producer.Targets.UseCollectedIPs = true
			case "discovery":
				cfg.Queue.IsDiscovery = true
			case "tun":
				cfg.Queue.Mode = "tun"
			case "original":
				direction = "original"
			case "no-packet-counters":
				manager.ctPacketsSupported = false
			}
			match := manager.limitedQueueMatch(direction, "20", true, "tcp", "sport", "443")
			all := strings.Join(match, " ")
			wantReply := name == "collect"
			if got := strings.Contains(all, "ct reply packets < 20"); got != wantReply {
				t.Fatalf("reply window=%v want=%v: %s", got, wantReply, all)
			}
			if name == "no-packet-counters" {
				if strings.Contains(all, "packets") {
					t.Fatal("unsupported counter match emitted")
				}
			} else if !wantReply && !strings.Contains(all, "ct original packets < 20") {
				t.Fatal(all)
			}
			if !strings.Contains(all, "ct direction "+direction) {
				t.Fatal("packet-window direction changed flow direction")
			}
		})
	}
}
