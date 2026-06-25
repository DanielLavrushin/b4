package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

type openAIProvider struct {
	endpoint string
	apiKey   string
	model    string
	httpc    *http.Client
	req      config.AIConfig
}

func (p *openAIProvider) Name() string { return ProviderOpenAI }

type openAIModelList struct {
	Data []struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
	} `json:"data"`
}

func (p *openAIProvider) ListModels(ctx context.Context) ([]Model, error) {
	url := strings.TrimRight(p.endpoint, "/") + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpc.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, readErrorBody(resp, "openai")
	}
	var list openAIModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("openai: decode models: %w", err)
	}
	out := make([]Model, 0, len(list.Data))
	for _, m := range list.Data {
		if !isOpenAIChatModel(m.ID) {
			continue
		}
		out = append(out, Model{ID: m.ID, Created: m.Created})
	}
	return out, nil
}

func isOpenAIChatModel(id string) bool {
	if id == "" {
		return false
	}
	prefixes := []string{"gpt-", "chatgpt-", "o1", "o3", "o4"}
	for _, p := range prefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model         string          `json:"model"`
	Messages      []openAIMessage `json:"messages"`
	Stream        bool            `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (p *openAIProvider) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	msgs := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openAIMessage{Role: string(RoleSystem), Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openAIMessage{Role: string(m.Role), Content: m.Content})
	}

	url := strings.TrimRight(p.endpoint, "/") + "/chat/completions"
	makeReq := func(omitTemperature bool) (*http.Request, error) {
		body := openAIRequest{
			Model:    p.model,
			Messages: msgs,
			Stream:   true,
			StreamOptions: &struct {
				IncludeUsage bool `json:"include_usage"`
			}{IncludeUsage: true},
			MaxTokens: clampMaxTokens(req, 1024),
		}
		if !omitTemperature {
			t := clampTemperature(req)
			body.Temperature = &t
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		return httpReq, nil
	}

	resp, err := postWithTemperatureRetry(ctx, p.httpc, makeReq, "openai")
	if err != nil {
		return nil, err
	}

	out := make(chan Chunk, 16)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		parseSSE(ctx, resp.Body, out, parseOpenAIEvent)
	}()
	return out, nil
}

func parseOpenAIEvent(payload []byte) (Chunk, bool, bool) {
	if bytes.Equal(payload, []byte("[DONE]")) {
		return Chunk{Done: true}, true, true
	}
	var c openAIChunk
	if err := json.Unmarshal(payload, &c); err != nil {
		return Chunk{Err: fmt.Errorf("openai: bad chunk: %w", err)}, true, false
	}
	out := Chunk{}
	if len(c.Choices) > 0 {
		out.Delta = c.Choices[0].Delta.Content
		if c.Choices[0].FinishReason != nil && *c.Choices[0].FinishReason != "" {
			out.Done = true
		}
	}
	if c.Usage != nil {
		out.Usage = &Usage{InputTokens: c.Usage.PromptTokens, OutputTokens: c.Usage.CompletionTokens}
	}
	if out.Delta == "" && out.Usage == nil && !out.Done {
		return Chunk{}, false, false
	}
	return out, true, false
}

// sendChunk delivers a data chunk, blocking until the consumer reads it or ctx
// is canceled. The ctx escape is what prevents the producer goroutine (and the
// underlying HTTP body) from leaking when the consumer returns early and stops
// draining a full buffered channel. Returns false if the send was abandoned.
func sendChunk(ctx context.Context, out chan<- Chunk, c Chunk) bool {
	select {
	case out <- c:
		return true
	case <-ctx.Done():
		return false
	}
}

// trySend makes a best-effort, non-blocking delivery of a terminal chunk
// (error or cancellation notice). It never blocks: if the consumer has stopped
// reading and the buffer is full, the chunk is dropped rather than leaking the
// producer. A plain ctx-select is wrong here because on cancellation ctx.Done
// is already ready and would race the delivery, dropping the notice ~half the
// time even when the consumer is still draining.
func trySend(out chan<- Chunk, c Chunk) {
	select {
	case out <- c:
	default:
	}
}

func parseSSE(ctx context.Context, r io.Reader, out chan<- Chunk, parse func([]byte) (Chunk, bool, bool)) {
	br := bufio.NewReader(r)
	var dataBuf bytes.Buffer
	flush := func() bool {
		if dataBuf.Len() == 0 {
			return true
		}
		payload := bytes.TrimSpace(dataBuf.Bytes())
		dataBuf.Reset()
		c, emit, done := parse(payload)
		if emit && !sendChunk(ctx, out, c) {
			return false
		}
		return !done
	}

	for {
		select {
		case <-ctx.Done():
			trySend(out, Chunk{Err: ctx.Err()})
			return
		default:
		}

		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) == 0 {
				if !flush() {
					return
				}
				continue
			}
			if bytes.HasPrefix(trimmed, []byte(":")) {
				continue
			}
			if bytes.HasPrefix(trimmed, []byte("data:")) {
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.Write(bytes.TrimSpace(trimmed[len("data:"):]))
				continue
			}
		}
		if err != nil {
			flush()
			if err != io.EOF {
				trySend(out, Chunk{Err: err})
			}
			return
		}
	}
}

func readErrorBody(resp *http.Response, who string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("%s: http %d", who, resp.StatusCode)
	}
	return fmt.Errorf("%s: http %d: %s", who, resp.StatusCode, msg)
}
