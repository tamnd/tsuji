package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tamnd/tsuji/pkg/gateway"
)

func anthEndpoint(url string) Endpoint {
	return Endpoint{Provider: "anthropic", Model: "claude-test", BaseURL: url, APIKey: "sk-ant-test"}
}

func TestAnthropicCompleteTranslation(t *testing.T) {
	var captured anthRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-ant-test" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		fmt.Fprint(w, `{
			"id":"msg_1","model":"claude-test","stop_reason":"tool_use",
			"content":[
				{"type":"text","text":"Let me check the weather."},
				{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Hanoi"}}
			],
			"usage":{"input_tokens":10,"output_tokens":7,"cache_read_input_tokens":5,"cache_creation_input_tokens":0}
		}`)
	}))
	defer srv.Close()

	a := NewAnthropic()
	req := &gateway.ChatRequest{
		Model: "anthropic/claude-test",
		Messages: []gateway.Message{
			{Role: "system", Content: json.RawMessage(`"Be brief."`)},
			{Role: "user", Content: json.RawMessage(`"Weather in Hanoi?"`)},
		},
		Tools: []gateway.Tool{{
			Type:     "function",
			Function: json.RawMessage(`{"name":"get_weather","description":"look up weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}`),
		}},
		ToolChoice: json.RawMessage(`"auto"`),
	}
	resp, err := a.Complete(context.Background(), anthEndpoint(srv.URL), req)
	if err != nil {
		t.Fatal(err)
	}

	// Request translation.
	if captured.System != "Be brief." {
		t.Errorf("system not hoisted: %q", captured.System)
	}
	if captured.MaxTokens != 4096 {
		t.Errorf("max_tokens default = %d", captured.MaxTokens)
	}
	if len(captured.Messages) != 1 || captured.Messages[0].Role != "user" {
		t.Errorf("messages = %+v", captured.Messages)
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Name != "get_weather" {
		t.Errorf("tools = %+v", captured.Tools)
	}

	// Response translation.
	msg := resp.Choices[0].Message
	if msg.Content == nil || *msg.Content != "Let me check the weather." {
		t.Errorf("content = %v", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Function.Name != "get_weather" || !strings.Contains(tc.Function.Arguments, "Hanoi") {
		t.Errorf("tool call = %+v", tc)
	}
	if *resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish = %s", *resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 15 {
		t.Errorf("prompt tokens = %d, want input+cache_read = 15", resp.Usage.PromptTokens)
	}
	if resp.Usage.PromptTokensDetails.CachedTokens != 5 {
		t.Errorf("cached tokens = %d", resp.Usage.PromptTokensDetails.CachedTokens)
	}
}

func TestAnthropicToolResultAndAssistantToolUse(t *testing.T) {
	var captured anthRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		fmt.Fprint(w, `{"id":"m","content":[{"type":"text","text":"22C"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	a := NewAnthropic()
	req := &gateway.ChatRequest{
		Model: "anthropic/claude-test",
		Messages: []gateway.Message{
			{Role: "user", Content: json.RawMessage(`"Weather?"`)},
			{Role: "assistant", ToolCalls: []gateway.ToolCall{{
				ID: "toolu_1", Type: "function",
				Function: gateway.FunctionCall{Name: "get_weather", Arguments: `{"city":"Hanoi"}`},
			}}},
			{Role: "tool", ToolCallID: "toolu_1", Content: json.RawMessage(`"22C sunny"`)},
		},
	}
	if _, err := a.Complete(context.Background(), anthEndpoint(srv.URL), req); err != nil {
		t.Fatal(err)
	}
	if len(captured.Messages) != 3 {
		t.Fatalf("messages = %d", len(captured.Messages))
	}
	// Assistant turn carries a tool_use block.
	asst, _ := json.Marshal(captured.Messages[1].Content)
	if !strings.Contains(string(asst), `"tool_use"`) || !strings.Contains(string(asst), "toolu_1") {
		t.Errorf("assistant blocks = %s", asst)
	}
	// The tool turn becomes a user tool_result block.
	if captured.Messages[2].Role != "user" {
		t.Errorf("tool turn role = %s", captured.Messages[2].Role)
	}
	res, _ := json.Marshal(captured.Messages[2].Content)
	if !strings.Contains(string(res), `"tool_result"`) || !strings.Contains(string(res), "22C sunny") {
		t.Errorf("tool_result blocks = %s", res)
	}
}

func TestAnthropicStreamTranslation(t *testing.T) {
	events := []string{
		`event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":9,"output_tokens":0,"cache_read_input_tokens":3}}}`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			fmt.Fprint(w, ev+"\n\n")
		}
	}))
	defer srv.Close()

	a := NewAnthropic()
	req := &gateway.ChatRequest{
		Model:    "anthropic/claude-test",
		Stream:   true,
		Messages: []gateway.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}

	var text strings.Builder
	var finish string
	var usage *gateway.Usage
	sawDone := false
	sawRole := false
	err := a.Stream(context.Background(), anthEndpoint(srv.URL), req, func(ch StreamChunk) error {
		if ch.Done {
			sawDone = true
			return nil
		}
		c := ch.Response.Choices[0]
		if c.Delta != nil {
			if c.Delta.Role == "assistant" {
				sawRole = true
			}
			if c.Delta.Content != nil {
				text.WriteString(*c.Delta.Content)
			}
		}
		if c.FinishReason != nil {
			finish = *c.FinishReason
		}
		if ch.Response.Usage != nil {
			usage = ch.Response.Usage
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawRole {
		t.Error("first delta missing assistant role")
	}
	if text.String() != "Hello world" {
		t.Errorf("text = %q", text.String())
	}
	if finish != "stop" {
		t.Errorf("finish = %q", finish)
	}
	if usage == nil || usage.PromptTokens != 12 || usage.CompletionTokens != 4 {
		t.Errorf("usage = %+v, want prompt 12 (9+3 cached) completion 4", usage)
	}
	if !sawDone {
		t.Error("no Done chunk")
	}
}

func TestAnthropicStreamToolUse(t *testing.T) {
	events := []string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_9","name":"lookup"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"go\"}"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`data: {"type":"message_stop"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			fmt.Fprint(w, ev+"\n\n")
		}
	}))
	defer srv.Close()

	a := NewAnthropic()
	req := &gateway.ChatRequest{
		Model:    "anthropic/claude-test",
		Stream:   true,
		Messages: []gateway.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}

	var name, args, finish string
	err := a.Stream(context.Background(), anthEndpoint(srv.URL), req, func(ch StreamChunk) error {
		if ch.Done {
			return nil
		}
		c := ch.Response.Choices[0]
		if c.Delta != nil {
			for _, tc := range c.Delta.ToolCalls {
				if tc.Function.Name != "" {
					name = tc.Function.Name
				}
				args += tc.Function.Arguments
			}
		}
		if c.FinishReason != nil {
			finish = *c.FinishReason
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "lookup" {
		t.Errorf("tool name = %q", name)
	}
	if args != `{"q":"go"}` {
		t.Errorf("args = %q", args)
	}
	if finish != "tool_calls" {
		t.Errorf("finish = %q", finish)
	}
}

func TestAnthropicUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"type":"overloaded_error"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	a := NewAnthropic()
	req := &gateway.ChatRequest{
		Model:    "anthropic/claude-test",
		Messages: []gateway.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}
	_, err := a.Complete(context.Background(), anthEndpoint(srv.URL), req)
	ue, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("err = %T %v", err, err)
	}
	if ue.Status != http.StatusTooManyRequests || ue.Provider != "anthropic" {
		t.Errorf("upstream error = %+v", ue)
	}
}
