package metrics

import "testing"

func TestFramesCarryACopyOfTheEngineFailure(t *testing.T) {
	m := &MetricsCollector{nowFn: newFakeClock(rigStart).now, sampler: &fakeSampler{}}
	if m.Hello().EngineFailure != nil {
		t.Fatal("a running engine must not report a failure")
	}

	m.SetEngineFailure(&EngineFailure{Mode: "tun", Error: "no usable IPv4 default route", RetriesLeft: 1})
	fr := m.Hello()
	if fr.EngineFailure == nil || fr.EngineFailure.Error != "no usable IPv4 default route" || fr.EngineFailure.RetriesLeft != 1 {
		t.Fatalf("the frame must carry the engine failure, got %+v", fr.EngineFailure)
	}

	fr.EngineFailure.Error = "changed"
	if got := m.GetEngineFailure().Error; got != "no usable IPv4 default route" {
		t.Fatalf("the frame must not share the collector's record, got %q", got)
	}
}
