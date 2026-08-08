package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/convert"
)

func convertAPI(t *testing.T) *http.ServeMux {
	t.Helper()
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterConvertApi()
	return mux
}

func postConvert(t *testing.T, mux *http.ServeMux, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandleConvertTools(t *testing.T) {
	mux := convertAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/convert/tools", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var tools []convert.ToolInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &tools); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one supported tool")
	}
}

func TestHandleConvertAnalyze(t *testing.T) {
	mux := convertAPI(t)
	rec := postConvert(t, mux, "/api/convert/analyze",
		`{"text":"-H:youtube.com -s1+s -At -d0+sm"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var res convert.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if res.Tool != "byedpi" {
		t.Fatalf("tool: got %q", res.Tool)
	}
	if len(res.Sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(res.Sets))
	}
	if len(res.Notes) == 0 {
		t.Fatal("expected a per-option report")
	}
}

func TestHandleConvertAnalyze_Errors(t *testing.T) {
	mux := convertAPI(t)

	tests := []struct {
		name string
		body string
		code int
	}{
		{"badJSON", `{`, http.StatusBadRequest},
		{"emptyText", `{"text":"   "}`, http.StatusBadRequest},
		{"unknownTool", `{"text":"-s1","tool":"nope"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postConvert(t, mux, "/api/convert/analyze", tt.body)
			if rec.Code != tt.code {
				t.Fatalf("got %d (%s), want %d", rec.Code, rec.Body.String(), tt.code)
			}
		})
	}
}

func TestHandleConvertAnalyze_MethodNotAllowed(t *testing.T) {
	mux := convertAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/convert/analyze", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want 405", rec.Code)
	}
}

func TestAssignSetIDs_RemapsEscalation(t *testing.T) {
	sets := []config.SetConfig{
		{Id: "p0", Name: "first", Escalate: config.EscalateConfig{To: "p1"}},
		{Id: "p1", Name: "second", Escalate: config.EscalateConfig{To: "p2"}},
		{Id: "p2", Name: "third"},
	}
	out := assignSetIDs(sets)

	if len(out) != 3 {
		t.Fatalf("expected 3 sets, got %d", len(out))
	}
	seen := map[string]bool{}
	for _, s := range out {
		if s.Id == "" || strings.HasPrefix(s.Id, "p") && len(s.Id) == 2 {
			t.Fatalf("placeholder id survived: %q", s.Id)
		}
		if seen[s.Id] {
			t.Fatalf("duplicate id %q", s.Id)
		}
		seen[s.Id] = true
	}
	if out[0].Escalate.To != out[1].Id {
		t.Fatalf("escalate.to not remapped: %q vs %q", out[0].Escalate.To, out[1].Id)
	}
	if out[1].Escalate.To != out[2].Id {
		t.Fatalf("escalate.to not remapped: %q vs %q", out[1].Escalate.To, out[2].Id)
	}
	if out[2].Escalate.To != "" {
		t.Fatalf("last set should not escalate, got %q", out[2].Escalate.To)
	}
}

func TestAssignSetIDs_DropsDanglingEscalation(t *testing.T) {
	out := assignSetIDs([]config.SetConfig{
		{Id: "p0", Escalate: config.EscalateConfig{To: "missing"}},
	})
	if out[0].Escalate.To != "" {
		t.Fatalf("expected dangling escalation to be cleared, got %q", out[0].Escalate.To)
	}
}

func TestNormalizeDomains(t *testing.T) {
	got := normalizeDomains([]string{" YouTube.com ", "https://youtube.com/watch", "", "vk.com:443"})
	want := []string{"youtube.com", "vk.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}
