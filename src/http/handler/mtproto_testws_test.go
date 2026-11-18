package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/mtproto"
)

func closedLocalRelay(t *testing.T) (relay, route string) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return fmt.Sprintf("127.0.0.1:%d", port-1), fmt.Sprintf("tcp://127.0.0.1:%d", port)
}

func runTestWS(t *testing.T, accept, body string) *httptest.ResponseRecorder {
	t.Helper()
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	req := httptest.NewRequest(http.MethodPost, "/api/mtproto/test-ws", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	api.handleMTProtoTestWS(rec, req)
	return rec
}

type sseEvent struct {
	name string
	data string
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			default:
				t.Fatalf("unexpected line %q in block %q", line, block)
			}
		}
		events = append(events, ev)
	}
	return events
}

func TestTestWSStreamsEachRouteWhenAskedForEvents(t *testing.T) {
	relay, route := closedLocalRelay(t)
	rec := runTestWS(t, "text/event-stream", fmt.Sprintf(`{"upstream_mode":"tcp","dc_relay":%q,"dc":2}`, relay))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q, want text/event-stream", ct)
	}
	events := parseSSE(t, rec.Body.String())
	var names []string
	for _, ev := range events {
		names = append(names, ev.name)
	}
	if got := strings.Join(names, ","); got != "plan,start,result,done" {
		t.Fatalf("events %s, want plan,start,result,done", got)
	}

	var plan struct {
		DC     int      `json:"dc"`
		Routes []string `json:"routes"`
	}
	if err := json.Unmarshal([]byte(events[0].data), &plan); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.DC != 2 || len(plan.Routes) != 1 || plan.Routes[0] != route {
		t.Errorf("plan %+v, want DC 2 with the one route %s", plan, route)
	}

	var start struct {
		Index *int `json:"index"`
	}
	if err := json.Unmarshal([]byte(events[1].data), &start); err != nil || start.Index == nil || *start.Index != 0 {
		t.Errorf("start %s, want index 0", events[1].data)
	}

	var result struct {
		Index int `json:"index"`
		mtproto.TransportProbeResult
	}
	if err := json.Unmarshal([]byte(events[2].data), &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if result.Index != 0 || result.Transport != route || result.OK || result.Stage != "dial" || result.Error == "" {
		t.Errorf("result %s, want a dial failure for %s at index 0", events[2].data, route)
	}
}

func TestTestWSAnswersWithOneJSONByDefault(t *testing.T) {
	relay, route := closedLocalRelay(t)
	rec := runTestWS(t, "", fmt.Sprintf(`{"upstream_mode":"tcp","dc_relay":%q,"dc":2}`, relay))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type %q, want application/json", ct)
	}
	var body struct {
		Success bool                           `json:"success"`
		DC      int                            `json:"dc"`
		Results []mtproto.TransportProbeResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Success || body.DC != 2 || len(body.Results) != 1 {
		t.Fatalf("body %s, want success for DC 2 with one result", rec.Body.String())
	}
	if r := body.Results[0]; r.Transport != route || r.OK || r.Stage != "dial" {
		t.Errorf("result %+v, want a dial failure for %s", r, route)
	}
}

func TestTestWSRejectsABadRequestBeforeStreaming(t *testing.T) {
	rec := runTestWS(t, "text/event-stream", `{"upstream_mode":"udp"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type %q, want a JSON error", ct)
	}
	if !strings.Contains(rec.Body.String(), "upstream_mode") {
		t.Errorf("body %s, want the reason", rec.Body.String())
	}
}
