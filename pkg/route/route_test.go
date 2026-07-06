package route

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/config"
	"github.com/tamnd/tsuji/pkg/gateway"
)

// upstream returns a fake openai-compatible server that can be told to fail.
func upstream(t *testing.T, name string, status *atomic.Int64, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if s := status.Load(); s != 200 {
			http.Error(w, `{"error":{"message":"boom"}}`, int(s))
			return
		}
		fmt.Fprintf(w, `{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, name)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testModel() *catalog.Model {
	return &catalog.Model{
		ID:              "test/model",
		Pricing:         catalog.Pricing{Prompt: "0.000002", Completion: "0.000006"},
		SupportedParams: []string{"tools"},
		Endpoints: []catalog.EndpointDef{
			{Provider: "alpha", Dialect: "openai", Model: "m-alpha"},
			{Provider: "beta", Dialect: "openai", Model: "m-beta", PromptPrice: "0.000001", CompletionPrice: "0.000002", SupportedParams: []string{"tools", "structured_outputs"}},
		},
	}
}

func newRouter(cfg *config.Config) *Router {
	r := New(cfg)
	r.SetSeed(42)
	return r
}

func chatReq(model string, prefs *gateway.ProviderPrefs) *gateway.ChatRequest {
	return &gateway.ChatRequest{Model: model, Provider: prefs}
}

func TestOnlyIgnoreFilters(t *testing.T) {
	var st, hits atomic.Int64
	st.Store(200)
	up := upstream(t, "alpha", &st, &hits)
	cfg := &config.Config{Providers: map[string]config.Provider{
		"alpha": {APIKey: "k", BaseURL: up.URL},
		"beta":  {APIKey: "k", BaseURL: up.URL},
	}}
	r := newRouter(cfg)

	cands, err := r.candidates(testModel(), chatReq("test/model", &gateway.ProviderPrefs{Only: []string{"beta"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].ep.Provider != "beta" {
		t.Fatalf("only filter: got %d cands, first %s", len(cands), cands[0].ep.Provider)
	}

	cands, err = r.candidates(testModel(), chatReq("test/model", &gateway.ProviderPrefs{Ignore: []string{"beta"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].ep.Provider != "alpha" {
		t.Fatalf("ignore filter: got %v", cands)
	}

	if _, err := r.candidates(testModel(), chatReq("test/model", &gateway.ProviderPrefs{Only: []string{"nope"}})); err == nil {
		t.Fatal("expected ErrNoEndpoint")
	}
}

func TestOrderAndFloor(t *testing.T) {
	var st, hits atomic.Int64
	st.Store(200)
	up := upstream(t, "x", &st, &hits)
	cfg := &config.Config{Providers: map[string]config.Provider{
		"alpha": {APIKey: "k", BaseURL: up.URL},
		"beta":  {APIKey: "k", BaseURL: up.URL},
	}}
	r := newRouter(cfg)

	cands, _ := r.candidates(testModel(), chatReq("test/model", &gateway.ProviderPrefs{Order: []string{"beta", "alpha"}}))
	if cands[0].ep.Provider != "beta" {
		t.Errorf("order[]: first = %s", cands[0].ep.Provider)
	}

	// :floor sorts by price; beta has the endpoint-level cheaper override.
	cands, _ = r.candidates(testModel(), chatReq("test/model:floor", nil))
	if cands[0].ep.Provider != "beta" {
		t.Errorf(":floor first = %s, want beta (cheaper)", cands[0].ep.Provider)
	}
	if cands[0].ep.PromptPrice != 100 {
		t.Errorf("endpoint price override = %d microcents, want 100", cands[0].ep.PromptPrice)
	}
}

func TestMaxPriceFilter(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{
		"alpha": {APIKey: "k", BaseURL: "http://x"},
		"beta":  {APIKey: "k", BaseURL: "http://x"},
	}}
	r := newRouter(cfg)
	// Prompt cap of $1.5/M keeps only beta ($1/M override); alpha is $2/M.
	p := 1.5
	cands, err := r.candidates(testModel(), chatReq("test/model", &gateway.ProviderPrefs{MaxPrice: &gateway.MaxPrice{Prompt: &p}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].ep.Provider != "beta" {
		t.Fatalf("max_price: got %d cands", len(cands))
	}
}

func TestRequireParameters(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{
		"alpha": {APIKey: "k", BaseURL: "http://x"},
		"beta":  {APIKey: "k", BaseURL: "http://x"},
	}}
	r := newRouter(cfg)
	yes := true
	req := chatReq("test/model", &gateway.ProviderPrefs{RequireParameters: &yes})
	req.ResponseFormat = &gateway.ResponseFormat{Type: "json_schema"}
	cands, err := r.candidates(testModel(), req)
	if err != nil {
		t.Fatal(err)
	}
	// Only beta declares structured_outputs at the endpoint level.
	if len(cands) != 1 || cands[0].ep.Provider != "beta" {
		t.Fatalf("require_parameters: got %v", len(cands))
	}
}

func TestFallbackWalk(t *testing.T) {
	var stA, hitsA, stB, hitsB atomic.Int64
	stA.Store(500) // alpha always fails
	stB.Store(200)
	upA := upstream(t, "alpha", &stA, &hitsA)
	upB := upstream(t, "beta", &stB, &hitsB)
	cfg := &config.Config{Providers: map[string]config.Provider{
		"alpha": {APIKey: "k", BaseURL: upA.URL},
		"beta":  {APIKey: "k", BaseURL: upB.URL},
	}}
	r := newRouter(cfg)

	req := chatReq("test/model", &gateway.ProviderPrefs{Order: []string{"alpha", "beta"}})
	up, err := r.Dial(context.Background(), testModel(), req)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := up.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("fallback did not save the request: %v", err)
	}
	var got string
	_ = json.Unmarshal([]byte(*resp.Choices[0].Message.Content), &got)
	if hitsA.Load() != 1 || hitsB.Load() != 1 {
		t.Errorf("hits alpha=%d beta=%d, want 1/1", hitsA.Load(), hitsB.Load())
	}
}

func TestNoFallbackOnTerminalError(t *testing.T) {
	var stA, hitsA, stB, hitsB atomic.Int64
	stA.Store(400) // bad request: terminal
	stB.Store(200)
	upA := upstream(t, "alpha", &stA, &hitsA)
	upB := upstream(t, "beta", &stB, &hitsB)
	cfg := &config.Config{Providers: map[string]config.Provider{
		"alpha": {APIKey: "k", BaseURL: upA.URL},
		"beta":  {APIKey: "k", BaseURL: upB.URL},
	}}
	r := newRouter(cfg)

	req := chatReq("test/model", &gateway.ProviderPrefs{Order: []string{"alpha", "beta"}})
	up, _ := r.Dial(context.Background(), testModel(), req)
	if _, err := up.Complete(context.Background(), req); err == nil {
		t.Fatal("expected terminal error")
	}
	if hitsB.Load() != 0 {
		t.Errorf("beta was tried after a terminal 400")
	}
}

func TestAllowFallbacksFalse(t *testing.T) {
	var stA, hitsA, stB, hitsB atomic.Int64
	stA.Store(500)
	stB.Store(200)
	upA := upstream(t, "alpha", &stA, &hitsA)
	upB := upstream(t, "beta", &stB, &hitsB)
	cfg := &config.Config{Providers: map[string]config.Provider{
		"alpha": {APIKey: "k", BaseURL: upA.URL},
		"beta":  {APIKey: "k", BaseURL: upB.URL},
	}}
	r := newRouter(cfg)

	no := false
	req := chatReq("test/model", &gateway.ProviderPrefs{Order: []string{"alpha", "beta"}, AllowFallbacks: &no})
	up, _ := r.Dial(context.Background(), testModel(), req)
	if _, err := up.Complete(context.Background(), req); err == nil {
		t.Fatal("expected failure with fallbacks disabled")
	}
	if hitsB.Load() != 0 {
		t.Errorf("beta was tried despite allow_fallbacks=false")
	}
}

func TestFreeVariantFiltersPaid(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{
		"alpha": {APIKey: "k", BaseURL: "http://x"},
		"beta":  {APIKey: "k", BaseURL: "http://x"},
	}}
	r := newRouter(cfg)
	if _, err := r.candidates(testModel(), chatReq("test/model:free", nil)); err == nil {
		t.Fatal("paid endpoints matched :free")
	}
}
