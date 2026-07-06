package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/store"
)

// stream relays upstream SSE chunks to the client, restamping identity and
// accumulating usage for the accounting row.
func (h *Handler) stream(w http.ResponseWriter, ctx context.Context, req *ChatRequest, model *catalog.Model, up *Upstream, gen *store.Generation, start time.Time) {
	fl, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "streaming unsupported", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Keep the socket warm until the first upstream frame arrives.
	fmt.Fprintf(w, "%s\n\n", keepAliveComment)
	fl.Flush()

	var lastUsage *Usage
	var finish string
	streamErr := up.Stream(ctx, req, func(chunk *ChatResponse, done bool) error {
		if done {
			return nil
		}
		chunk.ID = gen.ID
		chunk.Model = model.ID
		chunk.Provider = up.Provider
		if chunk.Object == "" {
			chunk.Object = "chat.completion.chunk"
		}
		if chunk.Usage != nil {
			lastUsage = chunk.Usage
			cost := usageCost(chunk.Usage, up)
			chunk.Usage.Cost = &cost
		}
		for _, c := range chunk.Choices {
			if c.FinishReason != nil {
				finish = *c.FinishReason
			}
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return err
		}
		fl.Flush()
		return nil
	})

	gen.LatencyMS = time.Since(start).Milliseconds()
	gen.FinishReason = finish
	if lastUsage != nil {
		gen.PromptTokens = lastUsage.PromptTokens
		gen.CompletionTokens = lastUsage.CompletionTokens
		gen.CostMicrocents = int64(lastUsage.PromptTokens)*up.PromptPrice +
			int64(lastUsage.CompletionTokens)*up.CompletionPrice
		if lastUsage.CompletionTokens == 0 && finish != "tool_calls" {
			gen.CostMicrocents = 0
		}
	}
	if up.CostOverride != nil {
		gen.CostMicrocents = up.CostOverride()
	}

	if streamErr != nil {
		gen.Error = streamErr.Error()
		_ = h.Store.InsertGeneration(gen)
		// The status line is gone; surface the failure as an SSE error frame.
		eb, _ := json.Marshal(errorBody{Error: APIError{Code: http.StatusBadGateway, Message: "provider error", Metadata: map[string]any{"provider_name": up.Provider}}})
		fmt.Fprintf(w, "data: %s\n\n", eb)
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
		return
	}

	_ = h.Store.InsertGeneration(gen)
	fmt.Fprint(w, "data: [DONE]\n\n")
	fl.Flush()
}

func usageCost(u *Usage, up *Upstream) float64 {
	if up.CostOverride != nil {
		return float64(up.CostOverride()) / 1e8
	}
	mc := int64(u.PromptTokens)*up.PromptPrice + int64(u.CompletionTokens)*up.CompletionPrice
	return float64(mc) / 1e8
}
