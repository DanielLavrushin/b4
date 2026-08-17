package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model       string
	System      string
	Messages    []Message
	MaxTokens   int
	Temperature float64
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Chunk struct {
	Delta string
	Done  bool
	Usage *Usage
	Err   error
}

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Created     int64  `json:"created,omitempty"`
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
	ListModels(ctx context.Context) ([]Model, error)
}

const (
	ProviderOpenAI           = "openai"
	ProviderAnthropic        = "anthropic"
	ProviderOllama           = "ollama"
	ProviderOpenAICompatible = "openai-compatible"
)

var (
	ErrDisabled        = errors.New("ai: disabled in configuration")
	ErrNoProvider      = errors.New("ai: no provider configured")
	ErrNoModel         = errors.New("ai: no model configured")
	ErrMissingAPIKey   = errors.New("ai: missing API key for provider")
	ErrMissingEndpoint = errors.New("ai: endpoint is required for provider")
	ErrUnknownProvider = errors.New("ai: unknown provider")
)

type Manager struct {
	mu      sync.RWMutex
	cfg     config.AIConfig
	secrets *SecretStore
	httpc   *http.Client
}

func NewManager(cfg config.AIConfig, configPath string) *Manager {
	m := &Manager{
		cfg:     cfg,
		secrets: NewSecretStore(configPath),
	}
	m.httpc = newHTTPClient(cfg.TimeoutSec)
	return m
}

func (m *Manager) Update(cfg config.AIConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg.TimeoutSec != m.cfg.TimeoutSec {
		m.httpc = newHTTPClient(cfg.TimeoutSec)
	}
	m.cfg = cfg
}

func (m *Manager) Config() config.AIConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) Secrets() *SecretStore {
	return m.secrets
}

func (m *Manager) Provider() (Provider, error) {
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()

	if !cfg.Enabled {
		return nil, ErrDisabled
	}
	if cfg.Provider == "" {
		return nil, ErrNoProvider
	}
	if cfg.Model == "" {
		return nil, ErrNoModel
	}
	return m.buildProvider(cfg.Provider, cfg.Endpoint, cfg.APIKeyRef, cfg.Model)
}

func (m *Manager) ProviderFor(provider, endpoint, apiKeyRef string) (Provider, error) {
	return m.buildProvider(provider, endpoint, apiKeyRef, "")
}

func (m *Manager) buildProvider(provider, endpoint, apiKeyRef, model string) (Provider, error) {
	m.mu.RLock()
	httpc := m.httpc
	cfg := m.cfg
	m.mu.RUnlock()

	if provider == "" {
		return nil, ErrNoProvider
	}

	keyRef := apiKeyRef
	if keyRef == "" {
		keyRef = provider
	}
	apiKey := m.secrets.Get(keyRef)

	switch strings.ToLower(provider) {
	case ProviderOpenAI:
		if apiKey == "" {
			return nil, fmt.Errorf("%w: %s", ErrMissingAPIKey, provider)
		}
		return &openAIProvider{
			endpoint: defaultIfEmpty(endpoint, "https://api.openai.com/v1"),
			apiKey:   apiKey,
			model:    model,
			httpc:    httpc,
			req:      cfg,
		}, nil
	case ProviderAnthropic:
		if apiKey == "" {
			return nil, fmt.Errorf("%w: %s", ErrMissingAPIKey, provider)
		}
		return &anthropicProvider{
			endpoint: defaultIfEmpty(endpoint, "https://api.anthropic.com/v1"),
			apiKey:   apiKey,
			model:    model,
			httpc:    httpc,
			req:      cfg,
		}, nil
	case ProviderOllama:
		return &ollamaProvider{
			endpoint: defaultIfEmpty(endpoint, "http://127.0.0.1:11434"),
			model:    model,
			httpc:    httpc,
			req:      cfg,
		}, nil
	case ProviderOpenAICompatible:
		if endpoint == "" {
			return nil, fmt.Errorf("%w: %s", ErrMissingEndpoint, provider)
		}
		return &openAIProvider{
			name:           ProviderOpenAICompatible,
			endpoint:       endpoint,
			apiKey:         apiKey,
			model:          model,
			httpc:          httpc,
			req:            cfg,
			allowAllModels: true,
		}, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, provider)
	}
}

func newHTTPClient(timeoutSec int) *http.Client {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: time.Duration(timeoutSec) * time.Second,
	}
	return &http.Client{Transport: tr}
}

func defaultIfEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func clampMaxTokens(req Request, def int) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return def
}

func clampTemperature(req Request) float64 {
	if req.Temperature < 0 {
		return 0
	}
	if req.Temperature > 2 {
		return 2
	}
	return req.Temperature
}

func temperatureRejected(body string) bool {
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "temperature") {
		return false
	}
	return strings.Contains(lower, "deprecated") ||
		strings.Contains(lower, "unsupported") ||
		strings.Contains(lower, "does not support") ||
		strings.Contains(lower, "not supported")
}

func postWithTemperatureRetry(
	ctx context.Context,
	httpc *http.Client,
	makeReq func(omitTemperature bool) (*http.Request, error),
	errPrefix string,
) (*http.Response, error) {
	_ = ctx
	for attempt := 0; attempt < 2; attempt++ {
		req, err := makeReq(attempt > 0)
		if err != nil {
			return nil, err
		}
		resp, err := httpc.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 == 2 {
			return resp, nil
		}
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		_ = resp.Body.Close()
		msg := strings.TrimSpace(string(bodyBytes))
		if attempt == 0 && temperatureRejected(msg) {
			continue
		}
		if msg == "" {
			return nil, fmt.Errorf("%s: http %d", errPrefix, resp.StatusCode)
		}
		return nil, fmt.Errorf("%s: http %d: %s", errPrefix, resp.StatusCode, msg)
	}
	return nil, fmt.Errorf("%s: unreachable", errPrefix)
}
