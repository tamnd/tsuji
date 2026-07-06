package gateway_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/config"
	"github.com/tamnd/tsuji/pkg/gateway"
	"github.com/tamnd/tsuji/pkg/route"
	"github.com/tamnd/tsuji/pkg/store"
)

// fakeUpstream is a minimal openai-compatible server for tests.
func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, tok := range []string{"Hel", "lo"} {
				fmt.Fprintf(w, `data: {"id":"up-1","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`+"\n\n", body.Model, tok)
			}
			fmt.Fprintf(w, `data: {"id":"up-1","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`+"\n\n", body.Model)
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"up-1","object":"chat.completion","created":1,"model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`, body.Model)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestServer(t *testing.T) (baseURL, apiKey string) {
	t.Helper()
	up := fakeUpstream(t)

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	plaintext, _, err := st.CreateKey("test")
	if err != nil {
		t.Fatal(err)
	}

	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Providers: map[string]config.Provider{
		"openai": {APIKey: "upstream-secret", BaseURL: up.URL + "/v1"},
	}}

	h := &gateway.Handler{Store: st, Catalog: cat, Dialer: route.New(cfg)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/chat/completions", h.ChatCompletions)
	mux.HandleFunc("GET /api/v1/models", h.Models)
	mux.HandleFunc("GET /api/v1/generation", h.Generation)
	mux.HandleFunc("GET /api/v1/key", h.KeyInfo)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, plaintext
}

func postChat(t *testing.T, base, key, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestChatCompletionsBlocking(t *testing.T) {
	base, key := newTestServer(t)
	resp := postChat(t, base, key, `{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out gateway.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.ID, "gen-") {
		t.Errorf("id = %q, want gen- prefix", out.ID)
	}
	if out.Model != "openai/gpt-4o-mini" {
		t.Errorf("model = %q", out.Model)
	}
	if out.Provider != "openai" {
		t.Errorf("provider = %q", out.Provider)
	}
	if out.Usage == nil || out.Usage.Cost == nil {
		t.Fatal("usage.cost missing")
	}
	// 5 prompt tokens at $0.15/M + 2 completion at $0.6/M.
	want := 5*0.00000015 + 2*0.0000006
	if diff := *out.Usage.Cost - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("cost = %v, want %v", *out.Usage.Cost, want)
	}
}

func TestChatCompletionsStreaming(t *testing.T) {
	base, key := newTestServer(t)
	resp := postChat(t, base, key, `{"model":"openai/gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	var frames []string
	var sawKeepAlive, sawDone bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, ":") {
			sawKeepAlive = true
		}
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				sawDone = true
				break
			}
			frames = append(frames, payload)
		}
	}
	if !sawKeepAlive {
		t.Error("no keep-alive comment before first frame")
	}
	if !sawDone {
		t.Error("no [DONE] terminator")
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}

	var content strings.Builder
	for _, f := range frames {
		var chunk gateway.ChatResponse
		if err := json.Unmarshal([]byte(f), &chunk); err != nil {
			t.Fatalf("bad frame %q: %v", f, err)
		}
		if chunk.Model != "openai/gpt-4o-mini" {
			t.Errorf("chunk model = %q", chunk.Model)
		}
		for _, c := range chunk.Choices {
			if c.Delta != nil && c.Delta.Content != nil {
				content.WriteString(*c.Delta.Content)
			}
		}
	}
	if content.String() != "Hello" {
		t.Errorf("streamed content = %q", content.String())
	}
}

func TestGenerationLookup(t *testing.T) {
	base, key := newTestServer(t)
	resp := postChat(t, base, key, `{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	var out gateway.ChatResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/generation?id="+out.ID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	gresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer gresp.Body.Close()
	if gresp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", gresp.StatusCode)
	}
	var wrap struct {
		Data struct {
			ID               string  `json:"id"`
			TokensPrompt     int     `json:"tokens_prompt"`
			TokensCompletion int     `json:"tokens_completion"`
			TotalCost        float64 `json:"total_cost"`
		} `json:"data"`
	}
	if err := json.NewDecoder(gresp.Body).Decode(&wrap); err != nil {
		t.Fatal(err)
	}
	if wrap.Data.ID != out.ID || wrap.Data.TokensPrompt != 5 || wrap.Data.TokensCompletion != 2 {
		t.Errorf("generation row = %+v", wrap.Data)
	}
}

func TestErrors(t *testing.T) {
	base, key := newTestServer(t)

	cases := []struct {
		name, auth, body string
		want             int
	}{
		{"no auth", "", `{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"x"}]}`, http.StatusUnauthorized},
		{"bad key", "sk-tsuji-v1-bogus", `{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"x"}]}`, http.StatusUnauthorized},
		{"unknown model", key, `{"model":"nope/nope","messages":[{"role":"user","content":"x"}]}`, http.StatusNotFound},
		{"missing model", key, `{"messages":[{"role":"user","content":"x"}]}`, http.StatusBadRequest},
		{"missing messages", key, `{"model":"openai/gpt-4o-mini"}`, http.StatusBadRequest},
		{"unconfigured provider", key, `{"model":"deepseek/deepseek-chat-v3.2","messages":[{"role":"user","content":"x"}]}`, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/chat/completions", strings.NewReader(tc.body))
			if tc.auth != "" {
				req.Header.Set("Authorization", "Bearer "+tc.auth)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			var eb struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
				t.Fatalf("error body not OpenAI-shaped: %v", err)
			}
			if eb.Error.Code != tc.want {
				t.Errorf("error.code = %d, want %d", eb.Error.Code, tc.want)
			}
		})
	}
}

func TestModelsEndpoint(t *testing.T) {
	base, _ := newTestServer(t)
	resp, err := http.Get(base + "/api/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var wrap struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt string `json:"prompt"`
			} `json:"pricing"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrap); err != nil {
		t.Fatal(err)
	}
	if len(wrap.Data) < 3 {
		t.Fatalf("got %d models", len(wrap.Data))
	}
	found := false
	for _, m := range wrap.Data {
		if m.ID == "anthropic/claude-sonnet-5" {
			found = true
			if m.ContextLength != 1000000 {
				t.Errorf("context = %d", m.ContextLength)
			}
			if m.Pricing.Prompt != "0.000003" {
				t.Errorf("prompt price = %q", m.Pricing.Prompt)
			}
		}
	}
	if !found {
		t.Error("claude-sonnet-5 missing from catalog")
	}
}
