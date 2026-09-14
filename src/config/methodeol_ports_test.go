package config

import (
	"slices"
	"testing"
)

func TestCollectTCPPortsAddsPort80ForHTTPMethodEOL(t *testing.T) {
	cfg := &Config{Sets: []*SetConfig{
		{Name: "m", Enabled: true, TCP: TCPConfig{HTTPMethodEOL: true}},
	}}
	ports := cfg.CollectTCPPorts()
	if !slices.Contains(ports, "80") {
		t.Fatalf("http_methodeol set must pull port 80 into the capture list, got %v", ports)
	}
}

func TestCollectTCPPortsSkipsPort80WhenMethodEOLOff(t *testing.T) {
	cfg := &Config{Sets: []*SetConfig{
		{Name: "m", Enabled: true, TCP: TCPConfig{}},
	}}
	if slices.Contains(cfg.CollectTCPPorts(), "80") {
		t.Fatal("port 80 must not be captured unless a set asks for it")
	}
}

func TestCollectTCPPortsIgnoresMethodEOLOnDisabledSet(t *testing.T) {
	cfg := &Config{Sets: []*SetConfig{
		{Name: "m", Enabled: false, TCP: TCPConfig{HTTPMethodEOL: true}},
	}}
	if slices.Contains(cfg.CollectTCPPorts(), "80") {
		t.Fatal("a disabled set must not pull in port 80")
	}
}
