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

// Anthropic speaks the native messages API.
// The generic openai adapter would work against nothing here; Anthropic has
// its own request shape, auth header, and streaming event vocabulary.
type Anthropic struct {
	Client  *http.Client
	Version string
}

// NewAnthropic returns the adapter with defaults.
func NewAnthropic() *Anthropic {
	return &Anthropic{
		Client:  &http.Client{Timeout: 10 * time.Minute},
		Version: "2023-06-01",
	}
}

// Name implements Adapter.
func (a *Anthropic) Name() string { return "anthropic" }

type anthMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthRequest struct {
	Model         string        `json:"model"`
	System        string        `json:"system,omitempty"`
	Messages      []anthMessage `json:"messages"`
	MaxTokens     int           `json:"max_tokens"`
	Temperature   *float64      `json:"temperature,omitempty"`
	TopP          *float64      `json:"top_p,omitempty"`
	TopK          *int          `json:"top_k,omitempty"`
	StopSequences []string      `json:"stop_sequences,omitempty"`
	Stream        bool          `json:"stream,omitempty"`
	Tools         []anthTool    `json:"tools,omitempty"`
	ToolChoice    any           `json:"tool_choice,omitempty"`
}

type anthContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// Thinking blocks.
	Thinking string `json:"thinking,omitempty"`
}

type anthResponse struct {
	ID         string             `json:"id"`
	Model      string             `json:"model"`
	Content    []anthContentBlock `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      anthUsage          `json:"usage"`
}

type anthUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// translateRequest maps the OpenAI shape onto the messages API.
func (a *Anthropic) translateRequest(ep Endpoint, req *gateway.ChatRequest, stream bool) (*anthRequest, error) {
	out := &anthRequest{
		Model:       ep.Model,
		MaxTokens:   4096,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
		Stream:      stream,
	}
	if req.MaxTokens != nil {
		out.MaxTokens = *req.MaxTokens
	}
	if len(req.Stop) > 0 {
		var one string
		var many []string
		if err := json.Unmarshal(req.Stop, &one); err == nil {
			out.StopSequences = []string{one}
		} else if err := json.Unmarshal(req.Stop, &many); err == nil {
			out.StopSequences = many
		}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			out.System = joinSystem(out.System, contentText(m.Content))
		case "tool":
			out.Messages = append(out.Messages, anthMessage{
				Role: "user",
				Content: []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     contentText(m.Content),
				}},
			})
		case "assistant":
			if len(m.ToolCalls) > 0 {
				blocks := []map[string]any{}
				if txt := contentText(m.Content); txt != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": txt})
				}
				for _, tc := range m.ToolCalls {
					var input any = map[string]any{}
					if tc.Function.Arguments != "" {
						_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
					}
					blocks = append(blocks, map[string]any{
						"type": "tool_use", "id": tc.ID, "name": tc.Function.Name, "input": input,
					})
				}
				out.Messages = append(out.Messages, anthMessage{Role: "assistant", Content: blocks})
			} else {
				out.Messages = append(out.Messages, anthMessage{Role: "assistant", Content: contentText(m.Content)})
			}
		default: // user
			out.Messages = append(out.Messages, anthMessage{Role: "user", Content: translateUserContent(m.Content)})
		}
	}

	for _, t := range req.Tools {
		var fn struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		}
		if err := json.Unmarshal(t.Function, &fn); err != nil {
			continue
		}
		params := fn.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out.Tools = append(out.Tools, anthTool{Name: fn.Name, Description: fn.Description, InputSchema: params})
	}
	if len(req.ToolChoice) > 0 {
		var s string
		if err := json.Unmarshal(req.ToolChoice, &s); err == nil {
			switch s {
			case "auto":
				out.ToolChoice = map[string]any{"type": "auto"}
			case "required":
				out.ToolChoice = map[string]any{"type": "any"}
			case "none":
				out.Tools = nil
			}
		} else {
			var obj struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if err := json.Unmarshal(req.ToolChoice, &obj); err == nil && obj.Function.Name != "" {
				out.ToolChoice = map[string]any{"type": "tool", "name": obj.Function.Name}
			}
		}
	}
	return out, nil
}

// contentText flattens string-or-parts content into plain text.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

// translateUserContent keeps text and converts image_url parts to native blocks.
func translateUserContent(raw json.RawMessage) any {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		return string(raw)
	}
	blocks := []map[string]any{}
	for _, p := range parts {
		switch p["type"] {
		case "text":
			blocks = append(blocks, map[string]any{"type": "text", "text": p["text"]})
		case "image_url":
			iu, _ := p["image_url"].(map[string]any)
			url, _ := iu["url"].(string)
			if media, data, ok := parseDataURL(url); ok {
				blocks = append(blocks, map[string]any{
					"type":   "image",
					"source": map[string]any{"type": "base64", "media_type": media, "data": data},
				})
			} else if url != "" {
				blocks = append(blocks, map[string]any{
					"type":   "image",
					"source": map[string]any{"type": "url", "url": url},
				})
			}
		}
	}
	return blocks
}

func parseDataURL(u string) (mediaType, data string, ok bool) {
	rest, found := strings.CutPrefix(u, "data:")
	if !found {
		return "", "", false
	}
	meta, payload, found := strings.Cut(rest, ",")
	if !found {
		return "", "", false
	}
	mediaType = strings.TrimSuffix(meta, ";base64")
	return mediaType, payload, true
}

func joinSystem(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n\n" + b
}

func mapStopReason(s string) string {
	switch s {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return s
	}
}

func (a *Anthropic) do(ctx context.Context, ep Endpoint, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimSuffix(ep.BaseURL, "/") + "/messages"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("x-api-key", ep.APIKey)
	hreq.Header.Set("anthropic-version", a.Version)
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
func (a *Anthropic) Complete(ctx context.Context, ep Endpoint, req *gateway.ChatRequest) (*gateway.ChatResponse, error) {
	areq, err := a.translateRequest(ep, req, false)
	if err != nil {
		return nil, err
	}
	resp, err := a.do(ctx, ep, areq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var ar anthResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, &UpstreamError{Provider: ep.Provider, Status: http.StatusBadGateway, Body: "invalid upstream response: " + err.Error()}
	}

	msg := &gateway.RespMessage{Role: "assistant"}
	var text, thinking strings.Builder
	for _, blk := range ar.Content {
		switch blk.Type {
		case "text":
			text.WriteString(blk.Text)
		case "thinking":
			thinking.WriteString(blk.Thinking)
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, gateway.ToolCall{
				ID:   blk.ID,
				Type: "function",
				Function: gateway.FunctionCall{
					Name:      blk.Name,
					Arguments: string(blk.Input),
				},
			})
		}
	}
	t := text.String()
	msg.Content = &t
	if th := thinking.String(); th != "" && (req.Reasoning == nil || !req.Reasoning.Exclude) {
		msg.Reasoning = &th
	}

	finish := mapStopReason(ar.StopReason)
	native := ar.StopReason
	return &gateway.ChatResponse{
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Choices: []gateway.Choice{{
			Index:              0,
			Message:            msg,
			FinishReason:       &finish,
			NativeFinishReason: &native,
		}},
		Usage: &gateway.Usage{
			PromptTokens:     ar.Usage.InputTokens + ar.Usage.CacheReadInputTokens + ar.Usage.CacheCreationInputTokens,
			CompletionTokens: ar.Usage.OutputTokens,
			TotalTokens:      ar.Usage.InputTokens + ar.Usage.CacheReadInputTokens + ar.Usage.CacheCreationInputTokens + ar.Usage.OutputTokens,
			PromptTokensDetails: &gateway.PromptTokensDetails{
				CachedTokens: ar.Usage.CacheReadInputTokens,
			},
		},
	}, nil
}

// anthEvent is the union of streaming event payloads we care about.
type anthEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Message struct {
		Usage anthUsage `json:"usage"`
	} `json:"message"`
	Usage anthUsage `json:"usage"`
}

// Stream implements Adapter, translating native events to OpenAI chunks.
func (a *Anthropic) Stream(ctx context.Context, ep Endpoint, req *gateway.ChatRequest, fn func(StreamChunk) error) error {
	areq, err := a.translateRequest(ep, req, true)
	if err != nil {
		return err
	}
	resp, err := a.do(ctx, ep, areq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	exclude := req.Reasoning != nil && req.Reasoning.Exclude
	var usage anthUsage
	var stopReason string
	toolIdx := -1 // running OpenAI tool_calls index
	blockType := map[int]string{}
	sentRole := false

	emit := func(delta *gateway.RespMessage, finish *string, u *gateway.Usage) error {
		if !sentRole && delta != nil {
			delta.Role = "assistant"
			sentRole = true
		}
		var native *string
		if finish != nil && stopReason != "" {
			native = &stopReason
		}
		return fn(StreamChunk{Response: &gateway.ChatResponse{
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Choices: []gateway.Choice{{Index: 0, Delta: delta, FinishReason: finish, NativeFinishReason: native}},
			Usage:   u,
		}})
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		payload, found := strings.CutPrefix(line, "data:")
		if !found {
			continue
		}
		var ev anthEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			usage = ev.Message.Usage
		case "content_block_start":
			blockType[ev.Index] = ev.ContentBlock.Type
			if ev.ContentBlock.Type == "tool_use" {
				toolIdx++
				i := toolIdx
				if err := emit(&gateway.RespMessage{ToolCalls: []gateway.ToolCall{{
					Index: &i, ID: ev.ContentBlock.ID, Type: "function",
					Function: gateway.FunctionCall{Name: ev.ContentBlock.Name},
				}}}, nil, nil); err != nil {
					return err
				}
			}
		case "content_block_delta":
			switch ev.Delta.Type {
			case "text_delta":
				txt := ev.Delta.Text
				if err := emit(&gateway.RespMessage{Content: &txt}, nil, nil); err != nil {
					return err
				}
			case "thinking_delta":
				if !exclude {
					th := ev.Delta.Thinking
					if err := emit(&gateway.RespMessage{Reasoning: &th}, nil, nil); err != nil {
						return err
					}
				}
			case "input_json_delta":
				i := toolIdx
				if err := emit(&gateway.RespMessage{ToolCalls: []gateway.ToolCall{{
					Index: &i, Function: gateway.FunctionCall{Arguments: ev.Delta.PartialJSON},
				}}}, nil, nil); err != nil {
					return err
				}
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage.OutputTokens > 0 {
				usage.OutputTokens = ev.Usage.OutputTokens
			}
		case "message_stop":
			finish := mapStopReason(stopReason)
			prompt := usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens
			if err := emit(&gateway.RespMessage{}, &finish, &gateway.Usage{
				PromptTokens:     prompt,
				CompletionTokens: usage.OutputTokens,
				TotalTokens:      prompt + usage.OutputTokens,
				PromptTokensDetails: &gateway.PromptTokensDetails{
					CachedTokens: usage.CacheReadInputTokens,
				},
			}); err != nil {
				return err
			}
			return fn(StreamChunk{Done: true})
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return fn(StreamChunk{Done: true})
}
