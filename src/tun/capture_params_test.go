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
