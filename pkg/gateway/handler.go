package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/store"
)

// Dialer resolves an endpoint and performs the upstream call.
// It is satisfied by the routing layer; M1 wires a single-endpoint resolver.
type Dialer interface {
	// Resolve maps a catalog model to a concrete endpoint plus adapter,
	// or an error when no provider can serve it.
	Dial(ctx context.Context, model *catalog.Model, req *ChatRequest) (*Upstream, error)
}

// Upstream is a resolved endpoint bound to its adapter functions.
type Upstream struct {
	Provider        string
	PromptPrice     int64
	CompletionPrice int64
	Complete        func(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	Stream          func(ctx context.Context, req *ChatRequest, fn func(*ChatResponse, bool) error) error
}

// Handler serves the /api/v1 inference surface.
type Handler struct {
	Store   *store.Store
	Catalog *catalog.Catalog
	Dialer  Dialer
	MaxBody int64
}

// keepAliveComment is written while the upstream is still thinking.
const keepAliveComment = ": TSUJI PROCESSING"

// ChatCompletions handles POST /api/v1/chat/completions.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	key := h.authenticate(w, r)
	if key == nil {
		return
	}

	maxBody := h.MaxBody
	if maxBody == 0 {
		maxBody = 32 << 20
	}
	var req ChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error(), nil)
		return
	}
	if req.Model == "" {
		WriteError(w, http.StatusBadRequest, "model is required", nil)
		return
	}
	if len(req.Messages) == 0 && req.Prompt == "" {
		WriteError(w, http.StatusBadRequest, "messages is required", nil)
		return
	}

	model := h.Catalog.Get(strings.TrimSuffix(req.Model, ":free"))
	if model == nil {
		WriteError(w, http.StatusNotFound, fmt.Sprintf("no model named %q", req.Model), nil)
		return
	}

	up, err := h.Dialer.Dial(r.Context(), model, &req)
	if err != nil {
		WriteError(w, http.StatusServiceUnavailable, "no provider available for "+model.ID+": "+err.Error(), nil)
		return
	}

	gen := &store.Generation{
		ID:             "gen-" + randomID(),
		KeyID:          key.ID,
		ModelRequested: req.Model,
		ModelServed:    model.ID,
		Provider:       up.Provider,
		AppReferer:     r.Header.Get("HTTP-Referer"),
		AppTitle:       r.Header.Get("X-Title"),
		Streamed:       req.Stream,
		CreatedAt:      time.Now().UTC(),
	}
	start := time.Now()

	if req.Stream {
		h.stream(w, r, &req, model, up, gen, start)
		return
	}

	resp, err := up.Complete(r.Context(), &req)
	gen.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		gen.Error = err.Error()
		_ = h.Store.InsertGeneration(gen)
		status, meta := mapUpstreamError(err, up.Provider)
		WriteError(w, status, "provider error", meta)
		return
	}

	h.finishResponse(resp, model, up, gen)
	_ = h.Store.InsertGeneration(gen)
	writeJSON(w, http.StatusOK, resp)
}

// finishResponse stamps tsuji identity and cost onto an upstream response.
func (h *Handler) finishResponse(resp *ChatResponse, model *catalog.Model, up *Upstream, gen *store.Generation) {
	resp.ID = gen.ID
	resp.Model = model.ID
	resp.Provider = up.Provider
	if resp.Object == "" {
		resp.Object = "chat.completion"
	}
	if resp.Usage != nil {
		gen.PromptTokens = resp.Usage.PromptTokens
		gen.CompletionTokens = resp.Usage.CompletionTokens
		gen.CostMicrocents = int64(resp.Usage.PromptTokens)*up.PromptPrice +
			int64(resp.Usage.CompletionTokens)*up.CompletionPrice
		// Zero completion insurance: nothing generated, nothing charged.
		if resp.Usage.CompletionTokens == 0 && !hasToolCalls(resp) {
			gen.CostMicrocents = 0
		}
		cost := float64(gen.CostMicrocents) / 1e8
		resp.Usage.Cost = &cost
	}
	if len(resp.Choices) > 0 {
		gen.FinishReason = deref(resp.Choices[0].FinishReason)
	}
}

func hasToolCalls(resp *ChatResponse) bool {
	for _, c := range resp.Choices {
		if c.Message != nil && len(c.Message.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// Models handles GET /api/v1/models.
func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.Catalog.List()})
}

// Generation handles GET /api/v1/generation?id=.
func (h *Handler) Generation(w http.ResponseWriter, r *http.Request) {
	key := h.authenticate(w, r)
	if key == nil {
		return
	}
	id := r.URL.Query().Get("id")
	g, err := h.Store.GenerationByID(id)
	if err != nil || g.KeyID != key.ID {
		WriteError(w, http.StatusNotFound, "generation not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"id":                      g.ID,
		"model":                   g.ModelServed,
		"provider_name":           g.Provider,
		"streamed":                g.Streamed,
		"tokens_prompt":           g.PromptTokens,
		"tokens_completion":       g.CompletionTokens,
		"native_tokens_reasoning": g.ReasoningTokens,
		"total_cost":              float64(g.CostMicrocents) / 1e8,
		"cache_discount":          nil,
		"latency":                 g.LatencyMS,
		"finish_reason":           g.FinishReason,
		"created_at":              g.CreatedAt.Format(time.RFC3339),
	}})
}

// KeyInfo handles GET /api/v1/key.
func (h *Handler) KeyInfo(w http.ResponseWriter, r *http.Request) {
	key := h.authenticate(w, r)
	if key == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"label":           key.Label,
		"name":            key.Name,
		"limit":           nil,
		"limit_remaining": nil,
		"usage":           0,
		"is_free_tier":    false,
	}})
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) *store.Key {
	auth := r.Header.Get("Authorization")
	secret, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || secret == "" {
		WriteError(w, http.StatusUnauthorized, "missing Authorization header", nil)
		return nil
	}
	key, err := h.Store.KeyBySecret(secret)
	if err != nil {
		if errors.Is(err, store.ErrKeyNotFound) {
			WriteError(w, http.StatusUnauthorized, "invalid API key", nil)
		} else {
			WriteError(w, http.StatusInternalServerError, "key lookup failed", nil)
		}
		return nil
	}
	return key
}

func mapUpstreamError(err error, providerName string) (int, map[string]any) {
	meta := map[string]any{"provider_name": providerName}
	var ue interface{ UpstreamStatus() int }
	if errors.As(err, &ue) {
		switch s := ue.UpstreamStatus(); {
		case s == http.StatusTooManyRequests:
			return http.StatusTooManyRequests, meta
		case s == http.StatusRequestTimeout:
			return http.StatusRequestTimeout, meta
		case s >= 500:
			return http.StatusBadGateway, meta
		default:
			return http.StatusBadGateway, meta
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusRequestTimeout, meta
	}
	return http.StatusBadGateway, meta
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
