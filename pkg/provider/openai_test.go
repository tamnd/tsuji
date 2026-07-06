package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tamnd/tsuji/pkg/gateway"
)

func openaiEndpoint(baseURL string) Endpoint {
	return Endpoint{
		Provider: "openai",
		Model:    "gpt-test",
		BaseURL:  baseURL,
		APIKey:   "sk-test",
	}
}

// The stream request must rewrite the model, drop tsuji-only fields, and ask
// the upstream for the final usage frame via stream_options.
func TestOpenAIStreamRequestShape(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	a := NewOpenAI()
	req := &gateway.ChatRequest{
		Model:    "openai/gpt-test",
		Messages: []gateway.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
		Models:   []string{"other/model"},
	}
	var usage *gateway.Usage
	err := a.Stream(context.Background(), openaiEndpoint(srv.URL), req, func(ch StreamChunk) error {
		if !ch.Done && ch.Response.Usage != nil {
			usage = ch.Response.Usage
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	if got["model"] != "gpt-test" {
		t.Errorf("model = %v, want upstream name gpt-test", got["model"])
	}
	if got["stream"] != true {
		t.Errorf("stream flag missing")
	}
	so, ok := got["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Errorf("stream_options = %v, want include_usage true", got["stream_options"])
	}
	if _, leaked := got["models"]; leaked {
		t.Errorf("tsuji-only field models leaked upstream")
	}
	if usage == nil || usage.PromptTokens != 7 || usage.CompletionTokens != 2 {
		t.Errorf("usage frame not relayed: %+v", usage)
	}
}

// Tools go up verbatim and tool_calls come back parsed.
func TestOpenAIToolCallRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		tools, ok := got["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Errorf("tools not forwarded: %v", got["tools"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"id": "up-1", "object": "chat.completion",
			"choices": [{"index": 0, "message": {"role": "assistant", "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"}}
			]}, "finish_reason": "tool_calls"}],
			"usage": {"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}
		}`))
	}))
	defer srv.Close()

	a := NewOpenAI()
	req := &gateway.ChatRequest{
		Model:    "openai/gpt-test",
		Messages: []gateway.Message{{Role: "user", Content: json.RawMessage(`"weather in tokyo?"`)}},
		Tools: []gateway.Tool{{
			Type:     "function",
			Function: json.RawMessage(`{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}`),
		}},
	}
	resp, err := a.Complete(context.Background(), openaiEndpoint(srv.URL), req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	msg := resp.Choices[0].Message
	if msg == nil || len(msg.ToolCalls) != 1 {
		t.Fatalf("tool_calls missing: %+v", msg)
	}
	tc := msg.ToolCalls[0]
	if tc.Function.Name != "get_weather" || tc.ID != "call_1" {
		t.Errorf("tool call = %+v", tc)
	}
	if fr := resp.Choices[0].FinishReason; fr == nil || *fr != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", fr)
	}
}
