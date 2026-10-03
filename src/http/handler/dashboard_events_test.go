package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/metrics"
)

func newestEventID() uint64 {
	if items := GetMetricsCollector().Hello().Events.Items; len(items) > 0 {
		return items[0].ID
	}
	return 0
}

func eventsAfter(id uint64, code string) []metrics.Event {
	var out []metrics.Event
	for _, ev := range GetMetricsCollector().Hello().Events.Items {
		if ev.ID > id && ev.Code == code {
			out = append(out, ev)
		}
	}
	return out
}

func TestMCPWritesAndUndosRaiseAnEventWithThePathOnly(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)
	since := newestEventID()

	if res := callSetValue(t, session, ctx, "sets[video].fragmentation.strategy", "disorder"); res.IsError {
		t.Fatalf("write failed: %+v", res.Content)
	}
	if out := decodeRevert(t, session, ctx); !out.Reverted {
		t.Fatalf("undo failed: %+v", out)
	}

	events := eventsAfter(since, metrics.EventMCPWrite)
	if len(events) != 2 {
		t.Fatalf("a write and its undo are two mcp_write events, got %d", len(events))
	}
	for _, ev := range events {
		if ev.Level != metrics.LevelInfo || ev.Args["path"] != "sets[video].fragmentation.strategy" || len(ev.Args) != 1 {
			t.Fatalf("mcp_write carries the path and nothing else: %+v", ev)
		}
		if strings.Contains(ev.Message, "disorder") {
			t.Fatalf("mcp_write must never carry the value: %q", ev.Message)
		}
	}
}

func TestAnUpdateIsReportedOnceItsBinaryLands(t *testing.T) {
	prev := executableFingerprintAtStart
	t.Cleanup(func() {
		executableFingerprintAtStart = prev
		launchedUpdate.Store(nil)
	})
	if executableFingerprintAtStart == "" {
		t.Skip("cannot read this test binary's own path")
	}
	since := newestEventID()

	noteUpdateLaunched("v1.85.0")
	if BinaryReplaced() {
		t.Fatal("the binary has not changed yet")
	}
	if got := eventsAfter(since, metrics.EventUpdateInstalled); len(got) != 0 {
		t.Fatal("nothing is installed before the binary changes")
	}

	executableFingerprintAtStart = "1:1"
	if !BinaryReplaced() || !BinaryReplaced() {
		t.Fatal("a replaced binary must be reported on every check")
	}
	events := eventsAfter(since, metrics.EventUpdateInstalled)
	if len(events) != 1 {
		t.Fatalf("the update is reported once, got %d events", len(events))
	}
	if ev := events[0]; ev.Args["version"] != "v1.85.0" || ev.Level != metrics.LevelInfo || !strings.Contains(ev.Message, "v1.85.0") {
		t.Fatalf("unexpected update_installed event: %+v", ev)
	}
}

func TestAReplacedBinaryWithoutAnUpdateRaisesNoEvent(t *testing.T) {
	prev := executableFingerprintAtStart
	t.Cleanup(func() { executableFingerprintAtStart = prev })
	launchedUpdate.Store(nil)
	since := newestEventID()

	executableFingerprintAtStart = "1:1"
	if prev != "" && !BinaryReplaced() {
		t.Fatal("a swapped binary still reads as replaced")
	}
	if got := eventsAfter(since, metrics.EventUpdateInstalled); len(got) != 0 {
		t.Fatal("a binary swapped by hand is the binary_replaced attention item, not an installed update")
	}
}

func TestDiagnosticsCarryTheGoRuntimeFigures(t *testing.T) {
	info := collectB4Info("", "standalone")
	if info.HeapSys == 0 || info.HeapInuse == 0 || info.Goroutines < 1 || info.OSThreads < 1 || info.OpenFDs < 1 {
		t.Fatalf("runtime figures missing: %+v", info)
	}
	raw, err := json.Marshal(DiagB4{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"heap_inuse", "heap_sys", "goroutines", "os_threads", "open_fds", "num_gc"} {
		if !strings.Contains(string(raw), `"`+key+`":`) {
			t.Errorf("%s must always be present, even at zero: %s", key, raw)
		}
	}
}
