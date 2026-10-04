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
