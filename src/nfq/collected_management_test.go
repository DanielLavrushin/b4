package nfq

import (
	"context"
	"errors"
	"github.com/daniellavrushin/b4/config"
	"net"
	"net/netip"
	"testing"
	"time"
)

func managementRig(t *testing.T) (*Worker, *Pool) {
	w, p, _ := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	p.state.destState = w.destState
	w.ProcessPacket(silentHelloFrom(42000))
	p.configMu.Lock()
	p.collected = map[netip.Addr]collectedCandidate{
		netip.MustParseAddr("1.2.3.4"): {setID: p.collectedBase.Sets[1].Id, host: "first.example.org", port: 443, at: time.Now()},
		netip.MustParseAddr("8.8.8.8"): {setID: p.collectedBase.Sets[1].Id, host: "second.example.org", port: 443, at: time.Now().Add(-time.Second)},
	}
	p.configMu.Unlock()
	if p.publishCollected(true).outcome != collectedApplied {
		t.Fatal("seed publication failed")
	}
	return w, p
}

func TestCollectedManagementRemovesOneAndForgetsOldWatch(t *testing.T) {
	w, p := managementRig(t)
	items := p.GetCollectedIPs()
	if len(items) != 2 || items[0].IP != "1.2.3.4" || items[0].Host != "first.example.org" {
		t.Fatal("bad snapshot", items)
	}
	items[0].Host = "changed"
	if p.GetCollectedIPs()[0].Host == "changed" {
		t.Fatal("snapshot aliases live metadata")
	}
	n, err := p.ClearCollectedIPs("1.2.3.4")
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if len(p.GetCollectedIPs()) != 1 || p.GetCollectedIPs()[0].IP != "8.8.8.8" {
		t.Fatal("wrong remaining member")
	}
	if yes, set := w.getMatcher().MatchIP(net.ParseIP("1.2.3.4")); yes && set.Targets.UseCollectedIPs {
		t.Fatal("removed IP still selects collected consumer")
	}
	if yes, set := w.getMatcher().MatchIP(net.ParseIP("8.8.8.8")); !yes || !set.Targets.UseCollectedIPs {
		t.Fatal("unrelated member lost")
	}
	if w.destState.Watching() {
		t.Fatal("removed IP retained an old collection watch")
	}
	n, err = p.ClearCollectedIPs("1.2.3.4")
	if n != 0 || err != nil {
		t.Fatal("repeat deletion is not idempotent", n, err)
	}
}

func TestCollectedManagementFailureRestoresMembership(t *testing.T) {
	w, p := managementRig(t)
	p.collectedInstall = oneStepInstaller(func(_ context.Context, cfg *config.Config) error {
		if len(cfg.Sets[0].Targets.CollectedIPs) == 1 {
			return errors.New("kernel rejected update")
		}
		return nil
	})
	n, err := p.ClearCollectedIPs("1.2.3.4")
	if err == nil || n != 0 || len(p.GetCollectedIPs()) != 2 {
		t.Fatal("failed removal reported success or lost state", n, err)
	}
	if yes, set := w.getMatcher().MatchIP(net.ParseIP("1.2.3.4")); !yes || !set.Targets.UseCollectedIPs {
		t.Fatal("failed removal changed matcher")
	}
	if !w.destState.Watching() {
		t.Fatal("failed removal cancelled an old watch")
	}
}

func TestCollectedManagementClearAllWithDisabledConsumer(t *testing.T) {
	_, p := managementRig(t)
	p.collectedBase.Sets[0].Enabled = false
	n, err := p.ClearCollectedIPs("")
	if err != nil || n != 2 || len(p.GetCollectedIPs()) != 0 || len(p.CollectedConfig(p.collectedBase).Sets[0].Targets.CollectedIPs) != 0 {
		t.Fatal(n, err)
	}
}

func TestCollectedManagementAddressValidationAndIPv6(t *testing.T) {
	_, p := managementRig(t)
	for _, bad := range []string{"bad", "1.2.3.4:443", "1.2.3.0/24", "fe80::1%eth0"} {
		if _, err := p.ClearCollectedIPs(bad); err == nil {
			t.Fatal("invalid address accepted", bad)
		}
	}
	address := netip.MustParseAddr("2001:4860:4860::8888")
	p.configMu.Lock()
	p.collected[address] = collectedCandidate{at: time.Now()}
	p.configMu.Unlock()
	if n, err := p.ClearCollectedIPs("2001:4860:4860:0:0:0:0:8888"); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := p.ClearCollectedIPs("::ffff:1.2.3.4"); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	var empty *Pool
	if items := empty.GetCollectedIPs(); items == nil || len(items) != 0 {
		t.Fatal("nil pool returned null")
	}
}

func TestCollectedManagementRemovalRejectsAnAlreadyQueuedWatch(t *testing.T) {
	_, p := managementRig(t)
	tracker := p.state.destState
	tracker.watchMu.RLock()
	var stale helloWatch
	for _, h := range tracker.watches {
		stale = *h
	}
	tracker.watchMu.RUnlock()
	if stale.gen == 0 {
		t.Fatal("missing watch generation")
	}
	if _, err := p.ClearCollectedIPs("1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	p.collectedInstall = oneStepInstaller(func(context.Context, *config.Config) error { calls++; return nil })
	p.applyCollected(p.Workers[0], []helloWatch{stale}, time.Now())
	if _, back := p.collected[netip.MustParseAddr("1.2.3.4")]; back || calls != 0 {
		t.Fatal("queued old watch republished a removed IP", calls)
	}
}
