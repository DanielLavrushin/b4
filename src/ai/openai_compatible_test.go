package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func newCompatManager(t *testing.T, endpoint string) *Manager {
	t.Helper()
	dir := t.TempDir()
	return NewManager(config.AIConfig{
		Enabled:  true,
		Provider: ProviderOpenAICompatible,
		Model:    "qwen3-8b-instruct",
		Endpoint: endpoint,
	}, filepath.Join(dir, "config.json"))
}

func TestOpenAICompatibleRequiresEndpoint(t *testing.T) {
	m := newCompatManager(t, "")
	_, err := m.Provider()
	if !errors.Is(err, ErrMissingEndpoint) {
		t.Fatalf("err = %v, want ErrMissingEndpoint", err)
	}
}

func TestOpenAICompatibleNoAPIKeyRequired(t *testing.T) {
	var gotAuth string
	var sawAuthHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, sawAuthHeader = r.Header["Authorization"]
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	m := newCompatManager(t, srv.URL)
	p, err := m.Provider()
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if p.Name() != ProviderOpenAICompatible {
		t.Fatalf("name = %q", p.Name())
	}

	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var text strings.Builder
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("chunk err: %v", c.Err)
		}
		text.WriteString(c.Delta)
	}
	if text.String() != "ok" {
		t.Fatalf("text = %q", text.String())
	}
	if sawAuthHeader {
		t.Fatalf("Authorization header must be absent when no key is set, got %q", gotAuth)
	}
}

func TestOpenAICompatibleSendsKeyWhenSet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	m := newCompatManager(t, srv.URL)
	if err := m.Secrets().Set(ProviderOpenAICompatible, "sk-local"); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	p, err := m.Provider()
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range ch {
	}
	if gotAuth != "Bearer sk-local" {
		t.Fatalf("auth = %q", gotAuth)
	}
}

func TestOpenAICompatibleListModelsUnfiltered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[
			{"id":"qwen3-8b-instruct","created":3},
			{"id":"llama-3.2-3b","created":2},
			{"id":"","created":1},
			{"id":"gpt-4o-mini","created":4}
		]}`)
	}))
	defer srv.Close()

	m := newCompatManager(t, srv.URL)
	p, err := m.Provider()
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	want := []string{"qwen3-8b-instruct", "llama-3.2-3b", "gpt-4o-mini"}
	if len(models) != len(want) {
		t.Fatalf("models = %+v, want %v", models, want)
	}
	for i, id := range want {
		if models[i].ID != id {
			t.Fatalf("models[%d] = %q, want %q", i, models[i].ID, id)
		}
	}
}

func TestOpenAIProviderStillFiltersModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"qwen3-8b-instruct"},{"id":"gpt-4o-mini"}]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager(config.AIConfig{
		Enabled:  true,
		Provider: ProviderOpenAI,
		Model:    "gpt-4o-mini",
		Endpoint: srv.URL,
	}, filepath.Join(dir, "config.json"))
	if err := m.Secrets().Set(ProviderOpenAI, "sk-test"); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	p, err := m.Provider()
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-4o-mini" {
		t.Fatalf("models = %+v, want only gpt-4o-mini", models)
	}
}
