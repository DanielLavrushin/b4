package tun

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestUpdateCaptureMarksTheChainDirtyOnlyWhenSomethingChanged(t *testing.T) {
	cfg := config.NewConfig()
	r := &routeManager{}
	r.setCaptureParams(captureParamsFrom(&cfg))

	if r.updateCapture(captureParamsFrom(&cfg)) || r.captureDirty {
		t.Fatalf("the same settings marked the capture chain for a rebuild")
	}

	changed := cfg.Clone()
	changed.Queue.UDPConnBytesLimit = 12
	if !r.updateCapture(captureParamsFrom(changed)) || !r.captureDirty {
		t.Fatalf("a new UDP packet limit did not mark the capture chain for a rebuild")
	}
	if r.udpLimit != 12 {
		t.Fatalf("udpLimit = %d, want 12", r.udpLimit)
	}
}

func TestReplyPortsFollowTheCaptureSettings(t *testing.T) {
	r := &routeManager{}
	if r.replyPorts() != nil {
		t.Fatalf("expected no ports before any settings")
	}
	r.setCaptureParams(captureParams{tcpPorts: []string{"443", "8443"}})
	if got := r.replyPorts(); len(got) != 2 || got[1] != "8443" {
		t.Fatalf("replyPorts = %v", got)
	}
}

func TestDeviceFilterChangeTouchesOnlyTheGate(t *testing.T) {
	cfg := config.NewConfig()
	r := &routeManager{}
	r.setCaptureParams(captureParamsFrom(&cfg))

	changed := cfg.Clone()
	changed.Queue.Devices.Enabled = !cfg.Queue.Devices.Enabled
	if !r.updateCapture(captureParamsFrom(changed)) {
		t.Fatalf("a device filter change was ignored")
	}
	if r.captureDirty || !r.gateDirty {
		t.Fatalf("device filter change: captureDirty=%v gateDirty=%v, want false/true", r.captureDirty, r.gateDirty)
	}
}

func TestSNATShadowedOnlyByMasqueradeThatCanMatchTheTunnel(t *testing.T) {
	own := "-A POSTROUTING -o b4tun0 -j SNAT --to-source 62.78.52.126\n"
	cases := []struct {
		name string
		dump string
		want bool
	}{
		{"own rule first", "-P POSTROUTING ACCEPT\n" + own + "-A POSTROUTING -j MASQUERADE\n", false},
		{"catch-all masquerade first", "-A POSTROUTING -s 192.168.1.0/24 -j MASQUERADE\n" + own, true},
		{"docker style negated interface first", "-A POSTROUTING -s 172.17.0.0/16 ! -o docker0 -j MASQUERADE\n" + own, true},
		{"masquerade pinned to the WAN first", "-A POSTROUTING -o eth0 -j MASQUERADE\n" + own, false},
		{"b4 masquerade chain jump first", "-A POSTROUTING -j B4_MASQ\n" + own, false},
		{"client mark return first", "-A POSTROUTING -m mark --mark 0x20000000/0x20000000 -j RETURN\n" + own, false},
		{"interface wildcard covering the tunnel first", "-A POSTROUTING -o b4+ -j MASQUERADE\n" + own, true},
		{"interface wildcard for another device first", "-A POSTROUTING -o ppp+ -j MASQUERADE\n" + own, false},
		{"foreign masquerade on the tunnel itself first", "-A POSTROUTING -o b4tun0 -j MASQUERADE\n" + own, true},
		{"negated tunnel interface first", "-A POSTROUTING ! -o b4tun0 -j MASQUERADE\n" + own, false},
	}
	for _, c := range cases {
		if got := snatShadowed(c.dump, "b4tun0"); got != c.want {
			t.Errorf("%s: snatShadowed = %v, want %v", c.name, got, c.want)
		}
	}
}
