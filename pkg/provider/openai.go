package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tamnd/tsuji/pkg/gateway"
)

// OpenAI is the generic openai-compatible dialect.
// It covers openai itself and every upstream that mirrors the API
// (deepseek, mistral, groq, together, fireworks, local runners).
type OpenAI struct {
	Client *http.Client
}

// NewOpenAI returns the adapter with a sane default client.
func NewOpenAI() *OpenAI {
	return &OpenAI{Client: &http.Client{Timeout: 10 * time.Minute}}
}

// Name implements Adapter.
func (a *OpenAI) Name() string { return "openai" }

// upstreamBody strips tsuji-only fields and rewrites the model to the
// upstream's own identifier before forwarding.
func upstreamBody(ep Endpoint, req *gateway.ChatRequest) ([]byte, error) {
	m := map[string]any{}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	m["model"] = ep.Model
	for _, k := range []string{"models", "route", "provider", "transforms", "reasoning", "usage", "prompt"} {
		delete(m, k)
	}
	return json.Marshal(m)
}

func (a *OpenAI) do(ctx context.Context, ep Endpoint, req *gateway.ChatRequest, stream bool) (*http.Response, error) {
	body, err := upstreamBody(ep, req)
	if err != nil {
		return nil, err
	}
	if stream {
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		m["stream"] = true
		body, _ = json.Marshal(m)
	}
	url := strings.TrimSuffix(ep.BaseURL, "/") + "/chat/completions"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+ep.APIKey)
	resp, err := a.Client.Do(hreq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &UpstreamError{Provider: ep.Provider, Status: resp.StatusCode, Body: string(raw)}
	}
	return resp, nil
}

// Complete implements Adapter.
func (a *OpenAI) Complete(ctx context.Context, ep Endpoint, req *gateway.ChatRequest) (*gateway.ChatResponse, error) {
	resp, err := a.do(ctx, ep, req, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out gateway.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, &UpstreamError{Provider: ep.Provider, Status: http.StatusBadGateway, Body: "invalid upstream response: " + err.Error()}
	}
	return &out, nil
}

// Stream implements Adapter.
func (a *OpenAI) Stream(ctx context.Context, ep Endpoint, req *gateway.ChatRequest, fn func(StreamChunk) error) error {
	resp, err := a.do(ctx, ep, req, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return fn(StreamChunk{Done: true})
		}
		var chunk gateway.ChatResponse
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if err := fn(StreamChunk{Response: &chunk}); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	// Upstream closed without [DONE]; treat as done so the client gets a clean end.
	return fn(StreamChunk{Done: true})
}
