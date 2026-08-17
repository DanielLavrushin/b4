package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/ai"
	"github.com/daniellavrushin/b4/log"
)

func (api *API) RegisterAIApi() {
	api.mux.HandleFunc("/api/ai/status", api.handleAIStatus)
	api.mux.HandleFunc("/api/ai/secrets", api.handleAISecrets)
	api.mux.HandleFunc("/api/ai/models", api.handleAIModels)
	api.mux.HandleFunc("/api/ai/explain", api.handleAIExplain)
	api.mux.HandleFunc("/api/ai/chat", api.handleAIChat)
}

// @Summary List models for an AI provider
// @Tags AI
// @Produce json
// @Param provider query string false "Provider id (openai, anthropic, ollama). Defaults to configured provider."
// @Param endpoint query string false "Override endpoint URL (used when provider matches the configured one)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 428 {object} map[string]string "API key missing for provider"
// @Failure 502 {object} map[string]string "Upstream provider error"
// @Failure 503 {object} map[string]string "AI manager not initialized"
// @Security BearerAuth
// @Router /ai/models [get]
func (api *API) handleAIModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mgr := globalAIManager
	if mgr == nil {
		writeJsonError(w, http.StatusServiceUnavailable, "ai manager not initialized")
		return
	}

	cfg := mgr.Config()
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" {
		provider = cfg.Provider
	}
	endpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	if endpoint == "" && provider == cfg.Provider {
		endpoint = cfg.Endpoint
	}
	apiKeyRef := ""
	if provider == cfg.Provider {
		apiKeyRef = cfg.APIKeyRef
	}

	p, err := mgr.ProviderFor(provider, endpoint, apiKeyRef)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ai.ErrMissingAPIKey) {
			status = http.StatusPreconditionRequired
		}
		writeJsonError(w, status, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	models, err := p.ListModels(ctx)
	if err != nil {
		writeJsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider": provider,
		"models":   models,
	})
}

type aiStatusResponse struct {
	Enabled            bool     `json:"enabled"`
	Provider           string   `json:"provider"`
	Model              string   `json:"model"`
	Endpoint           string   `json:"endpoint"`
	APIKeyRef          string   `json:"api_key_ref"`
	HasKey             bool     `json:"has_key"`
	Ready              bool     `json:"ready"`
	NotReadyReason     string   `json:"not_ready_reason,omitempty"`
	AvailableProviders []string `json:"available_providers"`
}

// @Summary Get AI assistant status
// @Description Returns whether the AI assistant is enabled, configured, and ready to serve requests.
// @Tags AI
// @Produce json
// @Success 200 {object} aiStatusResponse
// @Security BearerAuth
// @Router /ai/status [get]
func (api *API) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mgr := globalAIManager
	resp := aiStatusResponse{
		AvailableProviders: []string{ai.ProviderOpenAI, ai.ProviderAnthropic, ai.ProviderOllama, ai.ProviderOpenAICompatible},
	}
	if mgr == nil {
		resp.NotReadyReason = "manager not initialized"
		writeJSON(w, http.StatusOK, resp)
		return
	}
	cfg := mgr.Config()
	resp.Enabled = cfg.Enabled
	resp.Provider = cfg.Provider
	resp.Model = cfg.Model
	resp.Endpoint = cfg.Endpoint
	resp.APIKeyRef = cfg.APIKeyRef

	keyRef := cfg.APIKeyRef
	if keyRef == "" {
		keyRef = cfg.Provider
	}
	resp.HasKey = keyRef != "" && mgr.Secrets().Has(keyRef)

	if _, err := mgr.Provider(); err != nil {
		resp.NotReadyReason = err.Error()
	} else {
		resp.Ready = true
	}
	writeJSON(w, http.StatusOK, resp)
}

type aiSecretBody struct {
	Ref string `json:"ref"`
	Key string `json:"key"`
}

// @Summary List stored AI secret refs
// @Tags AI
// @Produce json
// @Success 200 {object} map[string][]string
// @Failure 503 {object} map[string]string "AI manager not initialized"
// @Security BearerAuth
// @Router /ai/secrets [get]
func (api *API) handleAISecrets(w http.ResponseWriter, r *http.Request) {
	mgr := globalAIManager
	if mgr == nil {
		writeJsonError(w, http.StatusServiceUnavailable, "ai manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		refs := mgr.Secrets().Refs()
		sort.Strings(refs)
		writeJSON(w, http.StatusOK, map[string][]string{"refs": refs})

	case http.MethodPut:
		api.putAISecret(w, r, mgr)

	case http.MethodDelete:
		api.deleteAISecret(w, r, mgr)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// @Summary Save an AI provider secret
// @Tags AI
// @Accept json
// @Produce json
// @Param body body aiSecretBody true "Secret ref and key"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /ai/secrets [put]
func (api *API) putAISecret(w http.ResponseWriter, r *http.Request, mgr *ai.Manager) {
	var body aiSecretBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Ref = strings.TrimSpace(body.Ref)
	if body.Ref == "" {
		writeJsonError(w, http.StatusBadRequest, "ref is required")
		return
	}
	if strings.TrimSpace(body.Key) == "" {
		writeJsonError(w, http.StatusBadRequest, "key is required (use DELETE to remove)")
		return
	}
	if err := mgr.Secrets().Set(body.Ref, body.Key); err != nil {
		log.Errorf("ai: failed to save secret: %v", err)
		writeJsonError(w, http.StatusInternalServerError, "failed to save secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "ref": body.Ref})
}

// @Summary Delete an AI provider secret
// @Tags AI
// @Produce json
// @Param ref query string true "Secret ref to remove"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /ai/secrets [delete]
func (api *API) deleteAISecret(w http.ResponseWriter, r *http.Request, mgr *ai.Manager) {
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		writeJsonError(w, http.StatusBadRequest, "ref query param is required")
		return
	}
	if err := mgr.Secrets().Delete(ref); err != nil {
		log.Errorf("ai: failed to delete secret: %v", err)
		writeJsonError(w, http.StatusInternalServerError, "failed to delete secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

type aiExplainRequest struct {
	Topic       string `json:"topic"`
	FieldLabel  string `json:"field_label,omitempty"`
	FieldDoc    string `json:"field_doc,omitempty"`
	Value       string `json:"value,omitempty"`
	ContextJSON string `json:"context_json,omitempty"`
	Question    string `json:"question,omitempty"`
	Language    string `json:"language,omitempty"`
}

// @Summary Stream an AI explanation for a config setting
// @Description Streams a Server-Sent Events response with delta/done/error events.
// @Tags AI
// @Accept json
// @Produce text/event-stream
// @Param body body aiExplainRequest true "Explain request"
// @Success 200 {string} string "SSE stream"
// @Failure 400 {object} map[string]string
// @Failure 503 {object} map[string]string "AI manager not initialized or provider unavailable"
// @Security BearerAuth
// @Router /ai/explain [post]
func (api *API) handleAIExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body aiExplainRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Topic = strings.TrimSpace(body.Topic)
	body.FieldLabel = strings.TrimSpace(body.FieldLabel)
	body.FieldDoc = strings.TrimSpace(body.FieldDoc)
	body.Language = strings.TrimSpace(body.Language)
	if body.Topic == "" {
		writeJsonError(w, http.StatusBadRequest, "topic is required")
		return
	}

	facts := ai.TopicFacts(body.Topic)
	system := buildExplainSystemPrompt(body.FieldDoc != "", facts != "", body.Language)
	user := buildExplainUserPrompt(body, facts)

	streamAI(w, r, ai.Request{
		System:   system,
		Messages: []ai.Message{{Role: ai.RoleUser, Content: user}},
	})
}

type aiChatRequest struct {
	System   string       `json:"system,omitempty"`
	Messages []ai.Message `json:"messages"`
}

// @Summary Stream an AI chat completion
// @Description Streams a Server-Sent Events response with delta/done/error events for a chat conversation.
// @Tags AI
// @Accept json
// @Produce text/event-stream
// @Param body body aiChatRequest true "Chat request"
// @Success 200 {string} string "SSE stream"
// @Failure 400 {object} map[string]string
// @Failure 503 {object} map[string]string "AI manager not initialized or provider unavailable"
// @Security BearerAuth
// @Router /ai/chat [post]
func (api *API) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body aiChatRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.Messages) == 0 {
		writeJsonError(w, http.StatusBadRequest, "messages must not be empty")
		return
	}
	for i, m := range body.Messages {
		if m.Role != ai.RoleUser && m.Role != ai.RoleAssistant && m.Role != ai.RoleSystem {
			writeJsonError(w, http.StatusBadRequest, fmt.Sprintf("messages[%d].role invalid", i))
			return
		}
	}

	sys := body.System
	if sys == "" {
		sys = buildChatSystemPrompt()
	}

	streamAI(w, r, ai.Request{
		System:   sys,
		Messages: body.Messages,
	})
}

func streamAI(w http.ResponseWriter, r *http.Request, req ai.Request) {
	mgr := globalAIManager
	if mgr == nil {
		writeJsonError(w, http.StatusServiceUnavailable, "ai manager not initialized")
		return
	}
	provider, err := mgr.Provider()
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, ai.ErrUnknownProvider) {
			status = http.StatusBadRequest
		}
		writeJsonError(w, status, err.Error())
		return
	}

	cfg := mgr.Config()
	if req.MaxTokens == 0 {
		req.MaxTokens = cfg.MaxTokens
	}
	if req.Temperature == 0 {
		req.Temperature = cfg.Temperature
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, _ := w.(http.Flusher)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	ch, err := provider.Stream(ctx, req)
	if err != nil {
		writeSSE(w, flusher, "error", map[string]string{"message": err.Error()})
		return
	}

	for chunk := range ch {
		if chunk.Err != nil {
			writeSSE(w, flusher, "error", map[string]string{"message": chunk.Err.Error()})
			return
		}
		if chunk.Delta != "" {
			writeSSE(w, flusher, "delta", map[string]string{"text": chunk.Delta})
		}
		if chunk.Done {
			payload := map[string]any{}
			if chunk.Usage != nil {
				payload["usage"] = chunk.Usage
			}
			writeSSE(w, flusher, "done", payload)
			return
		}
	}
	writeSSE(w, flusher, "done", map[string]any{})
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte(`{"error":"encode failed"}`)
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
	if flusher != nil {
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	setJsonHeader(w)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func buildExplainSystemPrompt(hasFieldDoc, hasFacts bool, language string) string {
	base := []string{
		"You are an in-app assistant for B4, a Linux DPI-bypass tool that uses netfilter queues.",
		"Audience: a non-expert end user configuring B4 through its web UI.",
		"Style: concise (under 200 words), plain language, no jargon without a one-line definition.",
		"When explaining a setting: cover (1) what it does in one sentence, (2) when to enable/change it, (3) common pitfalls.",
	}
	if hasFacts {
		base = append(base,
			"You will be given a 'B4-specific facts' block. Treat it as authoritative ground truth about how this setting behaves in b4 specifically — paraphrase from it, do not contradict it.",
			"If the user's question goes beyond what the facts block covers, say so explicitly rather than infer behavior from the field name or generic networking knowledge.",
		)
	}
	if hasFieldDoc {
		base = append(base,
			"You will be given an Authoritative description for this setting.",
			"Treat that description as the only source of truth about what the field does, its unit, and its default value.",
			"Do NOT infer the meaning, unit, or default from the field's JSON name — the name may be misleading (for example a field called *_bytes_limit may actually count packets).",
			"If the Authoritative description does not cover something the user asked about, say you do not have authoritative info on that point rather than guessing.",
			"Do not call the user's value 'unusual' or recommend changing it unless the Authoritative description gives you a basis to do so.",
		)
	} else if !hasFacts {
		base = append(base,
			"You were NOT given authoritative documentation for this setting. Be explicit about that uncertainty in your answer and avoid prescriptive recommendations.",
		)
	}
	base = append(base, "Do not invent settings, flags, or defaults that you are not told about.")
	if name := languageName(language); name != "" {
		base = append(base, fmt.Sprintf("Reply in %s. Keep technical identifiers (config field names, code, units like ms) untranslated.", name))
	}
	return strings.Join(base, " ")
}

func languageName(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return ""
	}
	if i := strings.IndexAny(code, "-_"); i > 0 {
		code = code[:i]
	}
	switch code {
	case "en":
		return "English"
	case "ru":
		return "Russian"
	case "uk":
		return "Ukrainian"
	case "de":
		return "German"
	case "fr":
		return "French"
	case "es":
		return "Spanish"
	case "pt":
		return "Portuguese"
	case "it":
		return "Italian"
	case "tr":
		return "Turkish"
	case "fa":
		return "Persian"
	case "zh":
		return "Chinese"
	case "ja":
		return "Japanese"
	case "ko":
		return "Korean"
	case "ar":
		return "Arabic"
	case "pl":
		return "Polish"
	default:
		return code
	}
}

func buildExplainUserPrompt(body aiExplainRequest, facts string) string {
	var sb strings.Builder
	sb.WriteString("Topic: ")
	sb.WriteString(body.Topic)
	sb.WriteString("\n")
	if body.FieldLabel != "" {
		sb.WriteString("UI label: ")
		sb.WriteString(body.FieldLabel)
		sb.WriteString("\n")
	}
	if body.FieldDoc != "" {
		sb.WriteString("Authoritative description: ")
		sb.WriteString(body.FieldDoc)
		sb.WriteString("\n")
	}
	if facts != "" {
		sb.WriteString("B4-specific facts (authoritative — trust over guesses):\n")
		sb.WriteString(facts)
		sb.WriteString("\n")
	}
	if body.Value != "" {
		sb.WriteString("Current value: ")
		sb.WriteString(body.Value)
		sb.WriteString("\n")
	}
	if body.ContextJSON != "" {
		sb.WriteString("Surrounding config (JSON):\n")
		sb.WriteString(body.ContextJSON)
		sb.WriteString("\n")
	}
	if body.Question != "" {
		sb.WriteString("User question: ")
		sb.WriteString(body.Question)
	} else {
		sb.WriteString("Explain this setting.")
	}
	return sb.String()
}

func buildChatSystemPrompt() string {
	return strings.Join([]string{
		"You are an in-app assistant for B4, a Linux DPI-bypass tool.",
		"Help the user diagnose and fix DPI/circumvention problems based on the context they share.",
		"Be concise. Suggest concrete config changes when appropriate, but never claim to have applied them.",
		"If you do not know something, say so rather than guess.",
	}, " ")
}
