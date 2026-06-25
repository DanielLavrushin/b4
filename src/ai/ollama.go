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
	"time"

	"github.com/daniellavrushin/b4/config"
)

type ollamaProvider struct {
	endpoint string
	model    string
	httpc    *http.Client
	req      config.AIConfig
}

func (p *ollamaProvider) Name() string { return ProviderOllama }

type ollamaTagsResponse struct {
	Models []struct {
		Name       string `json:"name"`
		Model      string `json:"model"`
		ModifiedAt string `json:"modified_at"`
	} `json:"models"`
}

func (p *ollamaProvider) ListModels(ctx context.Context) ([]Model, error) {
	url := strings.TrimRight(p.endpoint, "/") + "/api/tags"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpc.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, readErrorBody(resp, "ollama")
	}
	var list ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("ollama: decode tags: %w", err)
	}
	out := make([]Model, 0, len(list.Models))
	for _, m := range list.Models {
		id := m.Name
		if id == "" {
			id = m.Model
		}
		if id == "" {
			continue
		}
		var created int64
		if m.ModifiedAt != "" {
			if t, err := time.Parse(time.RFC3339, m.ModifiedAt); err == nil {
				created = t.Unix()
			}
		}
		out = append(out, Model{ID: id, Created: created})
	}
	return out, nil
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	NumPredict  int      `json:"num_predict,omitempty"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	Error           string `json:"error"`
}

func (p *ollamaProvider) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	msgs := make([]ollamaMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, ollamaMessage{Role: string(RoleSystem), Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, ollamaMessage{Role: string(m.Role), Content: m.Content})
	}

	temp := clampTemperature(req)
	body := ollamaRequest{
		Model:    p.model,
		Messages: msgs,
		Stream:   true,
		Options: &ollamaOptions{
			Temperature: &temp,
			NumPredict:  clampMaxTokens(req, 1024),
		},
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(p.endpoint, "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpc.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		return nil, readErrorBody(resp, "ollama")
	}

	out := make(chan Chunk, 16)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		br := bufio.NewReader(resp.Body)
		for {
			select {
			case <-ctx.Done():
				trySend(out, Chunk{Err: ctx.Err()})
				return
			default:
			}
			line, err := br.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				var c ollamaChunk
				if jerr := json.Unmarshal(bytes.TrimSpace(line), &c); jerr != nil {
					trySend(out, Chunk{Err: fmt.Errorf("ollama: bad chunk: %w", jerr)})
					return
				}
				if c.Error != "" {
					trySend(out, Chunk{Err: fmt.Errorf("ollama: %s", c.Error)})
					return
				}
				ch := Chunk{Delta: c.Message.Content, Done: c.Done}
				if c.Done {
					ch.Usage = &Usage{InputTokens: c.PromptEvalCount, OutputTokens: c.EvalCount}
				}
				if (ch.Delta != "" || ch.Done) && !sendChunk(ctx, out, ch) {
					return
				}
				if c.Done {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					trySend(out, Chunk{Err: err})
				}
				return
			}
		}
	}()
	return out, nil
}
