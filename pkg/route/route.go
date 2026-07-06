// Package route picks the upstream endpoint for a request.
// M1 ships the simplest resolver: the first configured endpoint of the model.
// The full engine (price-weighted balancing, fallbacks, variants, prefs)
// lands in M2 behind the same Dialer interface.
package route

import (
	"context"
	"errors"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/config"
	"github.com/tamnd/tsuji/pkg/gateway"
	"github.com/tamnd/tsuji/pkg/provider"
)

// Router resolves models to endpoints using operator provider config.
type Router struct {
	cfg      *config.Config
	adapters map[string]provider.Adapter
}

// New builds a Router with the built-in adapters registered.
func New(cfg *config.Config) *Router {
	oa := provider.NewOpenAI()
	return &Router{
		cfg:      cfg,
		adapters: map[string]provider.Adapter{"openai": oa},
	}
}

// defaultBaseURLs maps provider slugs to their public openai-compatible roots.
var defaultBaseURLs = map[string]string{
	"openai":    "https://api.openai.com/v1",
	"deepseek":  "https://api.deepseek.com/v1",
	"anthropic": "https://api.anthropic.com/v1",
	"groq":      "https://api.groq.com/openai/v1",
	"mistral":   "https://api.mistral.ai/v1",
	"together":  "https://api.together.xyz/v1",
	"fireworks": "https://api.fireworks.ai/inference/v1",
}

// ErrNoEndpoint means the model has no endpoint the operator has keys for.
var ErrNoEndpoint = errors.New("no configured endpoint")

// Dial implements gateway.Dialer.
func (r *Router) Dial(ctx context.Context, model *catalog.Model, req *gateway.ChatRequest) (*gateway.Upstream, error) {
	for _, def := range model.Endpoints {
		pc, ok := r.cfg.Providers[def.Provider]
		if !ok || pc.APIKey == "" {
			continue
		}
		adapter, ok := r.adapters[def.Dialect]
		if !ok {
			continue
		}
		base := pc.BaseURL
		if base == "" {
			base = defaultBaseURLs[def.Provider]
		}
		if base == "" {
			continue
		}
		ep := provider.Endpoint{
			Provider:        def.Provider,
			Model:           def.Model,
			BaseURL:         base,
			APIKey:          pc.APIKey,
			PromptPrice:     model.PromptPriceMicrocents(),
			CompletionPrice: model.CompletionPriceMicrocents(),
		}
		return &gateway.Upstream{
			Provider:        ep.Provider,
			PromptPrice:     ep.PromptPrice,
			CompletionPrice: ep.CompletionPrice,
			Complete: func(ctx context.Context, req *gateway.ChatRequest) (*gateway.ChatResponse, error) {
				return adapter.Complete(ctx, ep, req)
			},
			Stream: func(ctx context.Context, req *gateway.ChatRequest, fn func(*gateway.ChatResponse, bool) error) error {
				return adapter.Stream(ctx, ep, req, func(c provider.StreamChunk) error {
					return fn(c.Response, c.Done)
				})
			},
		}, nil
	}
	return nil, ErrNoEndpoint
}
