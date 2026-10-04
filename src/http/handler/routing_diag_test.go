package handler

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
)

func TestRoutingDiagnostics(t *testing.T) {
	prev := routingStatus
	t.Cleanup(func() { routingStatus = prev })
	status := tables.RoutingState{}
	routingStatus = func() tables.RoutingState { return status }

	routed := func(enabled bool) *config.Config {
		cfg := config.NewConfig()
		set := config.NewSetConfig()
		set.Id, set.Name = "s1", "Video"
		set.Enabled = true
		set.Routing.Enabled = enabled
		cfg.Sets = []*config.SetConfig{&set}
		return &cfg
	}

	if got := collectRoutingInfo(routed(false)); got != nil {
		t.Fatalf("System Info shows a routing row for a configuration without routing: %+v", got)
	}

	since := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	status = tables.RoutingState{
		Backend:      "nftables",
		MissingTool:  "ipset",
		Error:        "ensure table: exit status 1: Error: Could not process rule: Not supported",
		FailingSince: since,
		LastAttempt:  since.Add(630 * time.Second),
		NextRetry:    since.Add(1230 * time.Second),
	}
	got := collectRoutingInfo(routed(true))
	if got == nil {
		t.Fatalf("a routing set whose base cannot be installed is missing from System Info")
	}
	if got.Sets != 1 || got.Installed != 0 || got.Backend != "nftables" || got.MissingTool != "ipset" || got.Error != status.Error {
		t.Fatalf("the routing row lost a field: %+v", got)
	}
	if got.FailingSince != "2026-10-04T10:00:00Z" || got.LastAttempt != "2026-10-04T10:10:30Z" || got.NextRetry != "2026-10-04T10:20:30Z" {
		t.Fatalf("the routing row carries the wrong times: %+v", got)
	}

	status = tables.RoutingState{Backend: "iptables", SetErrors: []tables.RoutingSetError{{Set: "Video", Error: "ipset create: Kernel error received: Protocol not supported"}}}
	got = collectRoutingInfo(routed(false))
	if got == nil || len(got.SetErrors) != 1 || got.SetErrors[0].Set != "Video" || got.NextRetry != "" {
		t.Fatalf("a set that failed to install is not named in System Info: %+v", got)
	}
}
