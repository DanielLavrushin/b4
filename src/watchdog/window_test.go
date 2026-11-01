package watchdog

import (
	"sync"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

type registryDriver struct {
	*fakeDriver
	runID    string
	verdict  *discovery.SetVerdict
	inWindow func()

	windowMu sync.Mutex
	started  bool
	fired    bool
}

func (d *registryDriver) Start(cfg *config.Config, urls []string, opts discovery.StartSuiteOptions) (string, error) {
	if _, err := d.fakeDriver.Start(cfg, urls, opts); err != nil {
		return "", err
	}
	discovery.RegisterSuite(&discovery.CheckSuite{
		Id: d.runID, Status: discovery.CheckStatusComplete, Source: discovery.SourceWatchdog,
		SetId: opts.SetId, SetVerdict: d.verdict,
	})
	d.windowMu.Lock()
	d.started = true
	d.windowMu.Unlock()
	return d.runID, nil
}

func (d *registryDriver) IsActive() bool {
	d.windowMu.Lock()
	fire := d.started && !d.fired
	d.fired = d.fired || fire
	d.windowMu.Unlock()
	if fire {
		d.inWindow()
	}
	return false
}

func (d *registryDriver) Snapshot(id string) (*discovery.CheckSuite, bool) {
	return discovery.SnapshotCheckSuite(id)
}

func TestAWebRunStartedInTheIdleWaitLeavesTheHealsRunReadable(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, okCheck)
	driver := &registryDriver{
		fakeDriver: h.driver,
		runID:      "watchdog-heal-in-window",
		verdict:    coveredVerdict("disorder"),
		inWindow: func() {
			discovery.RegisterSuite(&discovery.CheckSuite{Id: "web-run-in-window", Status: discovery.CheckStatusRunning, Source: discovery.SourceWeb})
		},
	}
	t.Cleanup(func() { _ = discovery.CancelCheckSuite("web-run-in-window") })
	h.w.disc = driver

	h.healNow("yt")

	if !driver.fired {
		t.Fatal("precondition: the web run must start while the watchdog waits for the runtime to go idle")
	}
	if set := h.set("yt"); set.Fragmentation.Strategy != "disorder" {
		t.Fatalf("the heal still reads its own finished run and adopts its verdict, got strategy %q", set.Fragmentation.Strategy)
	}
	if st := h.state("yt"); st.Status != SetStatusHealthy {
		t.Errorf("a verified heal leaves the set healthy: %+v", st)
	}
}
