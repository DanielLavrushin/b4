package metrics

import "testing"

func TestSnapshotCarriesACopyOfTheEngineFailure(t *testing.T) {
	m := &MetricsCollector{}
	if m.GetSnapshot().EngineFailure != nil {
		t.Fatal("a running engine must not report a failure")
	}

	m.SetEngineFailure(&EngineFailure{Mode: "tun", Error: "no usable IPv4 default route", RetriesLeft: 1})
	snap := m.GetSnapshot()
	if snap.EngineFailure == nil || snap.EngineFailure.Error != "no usable IPv4 default route" || snap.EngineFailure.RetriesLeft != 1 {
		t.Fatalf("the snapshot must carry the engine failure, got %+v", snap.EngineFailure)
	}

	snap.EngineFailure.Error = "changed"
	if got := m.GetEngineFailure().Error; got != "no usable IPv4 default route" {
		t.Fatalf("the snapshot must not share the collector's record, got %q", got)
	}
}
