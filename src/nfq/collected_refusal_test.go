package nfq

import (
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/iphealth"
)

const collectedRefusalLine = "not collected: no set with Use collected IPs takes its next connection"

func TestARefusalForOneDeviceLeavesAServedDeviceJudged(t *testing.T) {
	for _, poolChange := range []bool{false, true} {
		t.Run(map[bool]string{false: "same pool", true: "after a pool change"}[poolChange], func(t *testing.T) {
			r := newLifetimeRig(t, nil)
			logs := captureLog(t)
			r.device(lifetimeOtherMac)
			r.silent(t, 42000)
			r.sweep(4 * time.Second)
			r.silent(t, 42001)
			r.sweep(5 * time.Second)
			if r.collected() != 0 || r.installs != 0 || strings.Count(logs.String(), collectedRefusalLine) != 1 || matcherBuilds(logs.String()) != 1 {
				t.Fatalf("setup: the device no consumer serves must be refused once with one matcher build: candidates=%d installs=%d refusals=%d builds=%d", r.collected(), r.installs, strings.Count(logs.String(), collectedRefusalLine), matcherBuilds(logs.String()))
			}
			r.silent(t, 42002)
			r.sweep(6 * time.Second)
			if builds, refusals := matcherBuilds(logs.String()), strings.Count(logs.String(), collectedRefusalLine); builds != 1 || refusals != 1 {
				t.Fatalf("a further silent connection from the refused device in the same run built %d matchers and logged %d refusals, want 1 and 1", builds, refusals)
			}
			if poolChange {
				r.p.collectedEpoch.Add(1)
			}
			r.device(lifetimeOwnedMac)
			r.silent(t, 42003)
			r.sweep(7 * time.Second)
			if r.collected() != 1 || r.installs != 1 || !slices.Contains(r.resets(), uint16(42003)) {
				t.Errorf("a silent connection from the served device after another device's refusal: candidates=%d installs=%d resets=%v; the refusal held only for the device it was decided for, so this one is judged and collected at once", r.collected(), r.installs, r.resets())
			}
			if builds := matcherBuilds(logs.String()); builds != 2 {
				t.Errorf("the served device's pass built %d matchers in all, want 2: one per pass that judges", builds)
			}
		})
	}
}

func TestTheRefusalsOfACountStayBoundedAndFallBackToJudging(t *testing.T) {
	tracker := &destStateTracker{}
	h := helloWatch{dst: net.IPv4(1, 2, 3, 4), port: 443, scope: &ingressScope{resolved: true, name: "br0"}}
	tracker.collectRuns = map[string]collectRun{iphealth.BanKey(h.dst, 443): {count: 2, until: time.Now().Add(time.Minute)}}
	device := func(i int) helloWatch {
		d := h
		d.mac = fmt.Sprintf("aa:aa:aa:aa:aa:%02x", i)
		return d
	}
	for i := range maxCollectedRefusals + 1 {
		tracker.markCollectRunUnowned(device(i), collectedRefusalScope(device(i)), 7)
	}
	if n := len(tracker.collectRuns[iphealth.BanKey(h.dst, 443)].refused); n != maxCollectedRefusals {
		t.Fatalf("the count holds %d refused device scopes, want at most %d", n, maxCollectedRefusals)
	}
	if !tracker.collectRunUnowned(device(0), 7) || tracker.collectRunUnowned(device(maxCollectedRefusals), 7) {
		t.Error("with the refusals full, a device refused earlier must stay refused and a further one must be judged again, never refused unjudged")
	}
	if tracker.collectRunUnowned(device(0), 8) {
		t.Error("a refusal from before a pool change still held")
	}
	tracker.markCollectRunUnowned(device(maxCollectedRefusals), collectedRefusalScope(device(maxCollectedRefusals)), 8)
	if !tracker.collectRunUnowned(device(maxCollectedRefusals), 8) {
		t.Error("after a pool change, the refusals decided before it must make room for a new one")
	}
}

func TestARefusalLeavesAServedDevicesParkedConnectionWatched(t *testing.T) {
	r := newLifetimeRig(t, nil)
	r.device(lifetimeOwnedMac)
	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	if !r.watched(42000) {
		t.Fatal("setup: the served device's first silent connection must wait for a second one")
	}
	r.device(lifetimeOtherMac)
	r.silent(t, 42001)
	r.sweep(5 * time.Second)
	if r.collected() != 0 || r.watched(42001) {
		t.Fatalf("setup: the other device's connection must be refused and end: candidates=%d watched=%v", r.collected(), r.watched(42001))
	}
	if !r.watched(42000) {
		t.Fatal("the refusal of the other device ended the served device's waiting connection, which the served device's consumer would have taken")
	}
	r.device(lifetimeOwnedMac)
	r.silent(t, 42002)
	r.sweep(6 * time.Second)
	if got := r.resets(); r.collected() != 1 || !slices.Contains(got, uint16(42000)) || !slices.Contains(got, uint16(42002)) || slices.Contains(got, uint16(42001)) {
		t.Errorf("the served device's next silent connection: candidates=%d resets=%v, want the address collected and both of its silent connections reset, and none of the refused device's", r.collected(), got)
	}
}
