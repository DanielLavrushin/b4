package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/ai"
	"github.com/daniellavrushin/b4/config"
)

func newAITestAPI(t *testing.T, mgr *ai.Manager) *API {
	t.Helper()
	prev := globalAIManager
	globalAIManager = mgr
	t.Cleanup(func() { globalAIManager = prev })

	mux := http.NewServeMux()
	api := &API{mux: mux}
	api.RegisterAIApi()
	return api
}

func openAIFakeServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
}

func parseSSEEvents(raw string) []map[string]string {
	var events []map[string]string
	for _, block := range strings.Split(raw, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		ev := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "event: ") {
				ev["event"] = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				ev["data"] = strings.TrimPrefix(line, "data: ")
			}
		}
		events = append(events, ev)
	}
	return events
}

func TestAIStatusUninitialized(t *testing.T) {
	api := newAITestAPI(t, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ai/status", nil)
	api.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp aiStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Ready {
		t.Fatal("expected not ready")
	}
	if resp.NotReadyReason == "" {
		t.Fatal("expected reason")
	}
	want := []string{ai.ProviderOpenAI, ai.ProviderAnthropic, ai.ProviderOllama, ai.ProviderOpenAICompatible}
	if len(resp.AvailableProviders) != len(want) {
		t.Fatalf("providers = %v", resp.AvailableProviders)
	}
	for i, p := range want {
		if resp.AvailableProviders[i] != p {
			t.Fatalf("providers[%d] = %q, want %q", i, resp.AvailableProviders[i], p)
		}
	}
}

func TestAIStatusReady(t *testing.T) {
	dir := t.TempDir()
	srv := openAIFakeServer(t, "")
	defer srv.Close()

	mgr := ai.NewManager(config.AIConfig{
		Enabled:  true,
		Provider: ai.ProviderOpenAI,
		Model:    "gpt-4o-mini",
		Endpoint: srv.URL,
	}, filepath.Join(dir, "config.json"))
	if err := mgr.Secrets().Set("openai", "sk-x"); err != nil {
		t.Fatalf("seed secret: %v", err)
	}

	api := newAITestAPI(t, mgr)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ai/status", nil))

	var resp aiStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Ready {
		t.Fatalf("expected ready, reason=%s", resp.NotReadyReason)
	}
	if !resp.HasKey {
		t.Fatal("expected has_key true")
	}
	if resp.Provider != ai.ProviderOpenAI {
		t.Fatalf("provider = %s", resp.Provider)
	}
}

func TestAISecretsCRUD(t *testing.T) {
	dir := t.TempDir()
	mgr := ai.NewManager(config.AIConfig{}, filepath.Join(dir, "config.json"))
	api := newAITestAPI(t, mgr)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ai/secrets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}

	put := httptest.NewRequest(http.MethodPut, "/api/ai/secrets", strings.NewReader(`{"ref":"openai","key":"sk-1"}`))
	rec = httptest.NewRecorder()
	api.mux.ServeHTTP(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !mgr.Secrets().Has("openai") {
		t.Fatal("secret not stored")
	}

	put = httptest.NewRequest(http.MethodPut, "/api/ai/secrets", strings.NewReader(`{"ref":"openai","key":""}`))
	rec = httptest.NewRecorder()
	api.mux.ServeHTTP(rec, put)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty key should 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/ai/secrets?ref=openai", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if mgr.Secrets().Has("openai") {
		t.Fatal("secret should be gone")
	}
}

func TestAIExplainStreamsSSE(t *testing.T) {
	dir := t.TempDir()
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Frag"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"mentation"}}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`,
		``,
		`data: [DONE]`,
		``,
		``,
	}, "\n")
	srv := openAIFakeServer(t, stream)
	defer srv.Close()

	mgr := ai.NewManager(config.AIConfig{
		Enabled: true, Provider: ai.ProviderOpenAI, Model: "gpt-4o-mini", Endpoint: srv.URL,
	}, filepath.Join(dir, "config.json"))
	mgr.Secrets().Set("openai", "sk-x")

	api := newAITestAPI(t, mgr)

	body := `{"topic":"fragmentation.strategy","value":"tcp_split","question":"what does this do?"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/explain", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %s", ct)
	}

	events := parseSSEEvents(rec.Body.String())
	var text strings.Builder
	var sawDone bool
	for _, ev := range events {
		switch ev["event"] {
		case "delta":
			var d struct{ Text string }
			if err := json.Unmarshal([]byte(ev["data"]), &d); err != nil {
				t.Fatalf("decode delta: %v", err)
			}
			text.WriteString(d.Text)
		case "done":
			sawDone = true
		case "error":
			t.Fatalf("unexpected error event: %s", ev["data"])
		}
	}
	if got := text.String(); got != "Fragmentation" {
		t.Fatalf("text = %q", got)
	}
	if !sawDone {
		t.Fatal("expected done event")
	}
}

func TestAIExplainGroundsOnFieldDoc(t *testing.T) {
	dir := t.TempDir()
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
		``,
	}, "\n")
	var captured struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	}))
	defer srv.Close()

	mgr := ai.NewManager(config.AIConfig{
		Enabled: true, Provider: ai.ProviderOpenAI, Model: "gpt-4o-mini", Endpoint: srv.URL,
	}, filepath.Join(dir, "config.json"))
	mgr.Secrets().Set("openai", "sk-x")
	api := newAITestAPI(t, mgr)

	body := `{"topic":"tcp.conn_bytes_limit","field_label":"Connection Packets Limit","field_doc":"Max TCP packets per connection to inspect (default 19).","value":"19"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/explain", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var system, user string
	for _, m := range captured.Messages {
		switch m.Role {
		case "system":
			system = m.Content
		case "user":
			user = m.Content
		}
	}

	if !strings.Contains(system, "Authoritative description") {
		t.Errorf("system prompt missing grounding instruction: %s", system)
	}
	if !strings.Contains(system, "may be misleading") {
		t.Errorf("system prompt missing JSON-name warning: %s", system)
	}
	if !strings.Contains(user, "Authoritative description: Max TCP packets per connection to inspect (default 19).") {
		t.Errorf("user prompt missing field doc: %s", user)
	}
	if !strings.Contains(user, "UI label: Connection Packets Limit") {
		t.Errorf("user prompt missing field label: %s", user)
	}
}

func TestAIExplainLanguageInstruction(t *testing.T) {
	dir := t.TempDir()
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		``,
		`data: [DONE]`,
		``,
		``,
	}, "\n")
	var captured struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	}))
	defer srv.Close()

	mgr := ai.NewManager(config.AIConfig{
		Enabled: true, Provider: ai.ProviderOpenAI, Model: "gpt-4o-mini", Endpoint: srv.URL,
	}, filepath.Join(dir, "config.json"))
	mgr.Secrets().Set("openai", "sk-x")
	api := newAITestAPI(t, mgr)

	cases := []struct {
		lang    string
		want    string
		notWant string
	}{
		{lang: "ru", want: "Reply in Russian"},
		{lang: "ru-RU", want: "Reply in Russian"},
		{lang: "en", want: "Reply in English"},
		{lang: "", want: "", notWant: "Reply in"},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			captured.Messages = nil
			body := `{"topic":"tcp.conn_bytes_limit","language":"` + tc.lang + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/ai/explain", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			api.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			var system string
			for _, m := range captured.Messages {
				if m.Role == "system" {
					system = m.Content
				}
			}
			if tc.want != "" && !strings.Contains(system, tc.want) {
				t.Errorf("expected %q in system prompt: %s", tc.want, system)
			}
			if tc.notWant != "" && strings.Contains(system, tc.notWant) {
				t.Errorf("did not expect %q in system prompt: %s", tc.notWant, system)
			}
		})
	}
}

func TestAIExplainNoFieldDocWarnsModel(t *testing.T) {
	dir := t.TempDir()
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		``,
		`data: [DONE]`,
		``,
		``,
	}, "\n")
	var captured struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	}))
	defer srv.Close()

	mgr := ai.NewManager(config.AIConfig{
		Enabled: true, Provider: ai.ProviderOpenAI, Model: "gpt-4o-mini", Endpoint: srv.URL,
	}, filepath.Join(dir, "config.json"))
	mgr.Secrets().Set("openai", "sk-x")
	api := newAITestAPI(t, mgr)

	body := `{"topic":"undocumented.test_topic","value":"42"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/explain", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var system string
	for _, m := range captured.Messages {
		if m.Role == "system" {
			system = m.Content
		}
	}
	if !strings.Contains(system, "NOT given authoritative documentation") {
		t.Errorf("expected uncertainty warning, got: %s", system)
	}
	if strings.Contains(system, "Authoritative description") {
		t.Errorf("should not mention authoritative description when none provided: %s", system)
	}
}

func TestAIExplainRequiresTopic(t *testing.T) {
	dir := t.TempDir()
	mgr := ai.NewManager(config.AIConfig{Enabled: true, Provider: ai.ProviderOllama, Model: "llama3"}, filepath.Join(dir, "config.json"))
	api := newAITestAPI(t, mgr)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/ai/explain", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAIChatRejectsEmptyMessages(t *testing.T) {
	dir := t.TempDir()
	mgr := ai.NewManager(config.AIConfig{Enabled: true, Provider: ai.ProviderOllama, Model: "llama3"}, filepath.Join(dir, "config.json"))
	api := newAITestAPI(t, mgr)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/ai/chat", strings.NewReader(`{"messages":[]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAIModelsEndpoint(t *testing.T) {
	dir := t.TempDir()
	body := `{"data":[{"id":"gpt-4o-mini","created":1700000000},{"id":"text-embedding-3-large","created":1700000001}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("upstream path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	mgr := ai.NewManager(config.AIConfig{
		Enabled: true, Provider: ai.ProviderOpenAI, Model: "gpt-4o-mini", Endpoint: srv.URL,
	}, filepath.Join(dir, "config.json"))
	mgr.Secrets().Set("openai", "sk-x")

	api := newAITestAPI(t, mgr)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ai/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Provider string     `json:"provider"`
		Models   []ai.Model `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Provider != ai.ProviderOpenAI {
		t.Fatalf("provider = %s", resp.Provider)
	}
	if len(resp.Models) != 1 || resp.Models[0].ID != "gpt-4o-mini" {
		t.Fatalf("models = %+v", resp.Models)
	}
}

func TestAIModelsOverrideUnsavedProvider(t *testing.T) {
	dir := t.TempDir()
	body := `{"data":[{"id":"claude-haiku-4-5","display_name":"Claude Haiku 4.5","created_at":"2026-02-04T00:00:00Z"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	mgr := ai.NewManager(config.AIConfig{
		Enabled: true, Provider: ai.ProviderOpenAI, Model: "gpt-4o-mini", Endpoint: "https://saved-openai",
	}, filepath.Join(dir, "config.json"))
	mgr.Secrets().Set("anthropic", "ant-x")

	api := newAITestAPI(t, mgr)

	url := "/api/ai/models?provider=anthropic&endpoint=" + srv.URL
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAIModelsMissingKey(t *testing.T) {
	dir := t.TempDir()
	mgr := ai.NewManager(config.AIConfig{
		Enabled: true, Provider: ai.ProviderOpenAI, Endpoint: "http://nope",
	}, filepath.Join(dir, "config.json"))
	api := newAITestAPI(t, mgr)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ai/models", nil))
	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAIChatHotReload(t *testing.T) {
	dir := t.TempDir()
	mgr := ai.NewManager(config.AIConfig{}, filepath.Join(dir, "config.json"))
	api := newAITestAPI(t, mgr)

	body := `{"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/ai/chat", strings.NewReader(body)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled status = %d", rec.Code)
	}

	stream := strings.Join([]string{
		`{"message":{"role":"assistant","content":"ok"},"done":false}`,
		`{"message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":3,"eval_count":1}`,
		``,
	}, "\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, stream)
	}))
	defer srv.Close()

	mgr.Update(config.AIConfig{
		Enabled: true, Provider: ai.ProviderOllama, Model: "llama3", Endpoint: srv.URL,
	})

	rec = httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/ai/chat", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("after Update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("event: delta")) {
		t.Fatalf("expected delta event, got %s", rec.Body.String())
	}
}
