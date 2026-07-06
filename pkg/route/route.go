// Package route picks upstream endpoints for a request and walks fallbacks.
// Selection order: filter candidates by provider prefs and variant, sort by
// the requested strategy (default price-weighted), then try each in turn on
// retryable failures. When every endpoint of a model fails, the models[]
// fallback list moves to the next model.
package route

import (
	"context"
	"errors"
	"math/rand"
	"slices"
	"strings"
	"sync"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/config"
	"github.com/tamnd/tsuji/pkg/gateway"
	"github.com/tamnd/tsuji/pkg/provider"
)

// Router resolves models to endpoints using operator provider config.
type Router struct {
	cfg      *config.Config
	adapters map[string]provider.Adapter

	mu  sync.Mutex
	rng *rand.Rand
}

// New builds a Router with the built-in adapters registered.
// seed fixes the weighted-choice RNG; pass 0 for a time-free default source.
func New(cfg *config.Config) *Router {
	return &Router{
		cfg: cfg,
		adapters: map[string]provider.Adapter{
			"openai":    provider.NewOpenAI(),
			"anthropic": provider.NewAnthropic(),
		},
		rng: rand.New(rand.NewSource(1)),
	}
}

// SetSeed re-seeds the weighted-choice RNG (tests).
func (r *Router) SetSeed(seed int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rng = rand.New(rand.NewSource(seed))
}

// defaultBaseURLs maps provider slugs to their public API roots.
var defaultBaseURLs = map[string]string{
	"openai":    "https://api.openai.com/v1",
	"deepseek":  "https://api.deepseek.com/v1",
	"anthropic": "https://api.anthropic.com/v1",
	"google":    "https://generativelanguage.googleapis.com/v1beta/openai",
	"groq":      "https://api.groq.com/openai/v1",
	"mistral":   "https://api.mistral.ai/v1",
	"together":  "https://api.together.xyz/v1",
	"fireworks": "https://api.fireworks.ai/inference/v1",
	"deepinfra": "https://api.deepinfra.com/v1/openai",
	"xai":       "https://api.x.ai/v1",
	"moonshot":  "https://api.moonshot.ai/v1",
	"alibaba":   "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
	"zai":       "https://api.z.ai/api/paas/v4",
	"minimax":   "https://api.minimax.io/v1",
}

// ErrNoEndpoint means no endpoint survived filtering and configuration.
var ErrNoEndpoint = errors.New("no matching endpoint")

// candidate is one dialable endpoint bound to its adapter.
type candidate struct {
	def     catalog.EndpointDef
	ep      provider.Endpoint
	adapter provider.Adapter
}

// Dial implements gateway.Dialer.
func (r *Router) Dial(ctx context.Context, model *catalog.Model, req *gateway.ChatRequest) (*gateway.Upstream, error) {
	cands, err := r.candidates(model, req)
	if err != nil {
		return nil, err
	}
	primary := cands[0]

	// The Upstream closures walk the candidate list on retryable errors.
	return &gateway.Upstream{
		Provider:        primary.ep.Provider,
		PromptPrice:     primary.ep.PromptPrice,
		CompletionPrice: primary.ep.CompletionPrice,
		Complete: func(ctx context.Context, req *gateway.ChatRequest) (*gateway.ChatResponse, error) {
			var lastErr error
			for _, c := range cands {
				resp, err := c.adapter.Complete(ctx, c.ep, req)
				if err == nil {
					return resp, nil
				}
				lastErr = err
				if !retryable(err) {
					break
				}
			}
			return nil, lastErr
		},
		Stream: func(ctx context.Context, req *gateway.ChatRequest, fn func(*gateway.ChatResponse, bool) error) error {
			var lastErr error
			for _, c := range cands {
				delivered := false
				err := c.adapter.Stream(ctx, c.ep, req, func(ch provider.StreamChunk) error {
					delivered = true
					return fn(ch.Response, ch.Done)
				})
				if err == nil {
					return nil
				}
				lastErr = err
				// After bytes reach the client we cannot switch upstreams.
				if delivered || !retryable(err) {
					break
				}
			}
			return lastErr
		},
	}, nil
}

// candidates builds, filters, and orders the endpoint list for one model.
func (r *Router) candidates(model *catalog.Model, req *gateway.ChatRequest) ([]candidate, error) {
	_, variant := catalog.SplitVariant(req.Model)
	prefs := req.Provider

	var cands []candidate
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
		c := candidate{
			def:     def,
			adapter: adapter,
			ep: provider.Endpoint{
				Provider:        def.Provider,
				Model:           def.Model,
				BaseURL:         base,
				APIKey:          pc.APIKey,
				PromptPrice:     def.PromptPriceMicrocents(model),
				CompletionPrice: def.CompletionPriceMicrocents(model),
			},
		}
		if !r.match(c, model, req, prefs, variant) {
			continue
		}
		cands = append(cands, c)
	}
	if len(cands) == 0 {
		return nil, ErrNoEndpoint
	}

	r.order(cands, prefs, variant)

	if prefs != nil && prefs.AllowFallbacks != nil && !*prefs.AllowFallbacks {
		cands = cands[:1]
	}
	return cands, nil
}

// match applies the filtering rules from provider prefs and variant.
func (r *Router) match(c candidate, model *catalog.Model, req *gateway.ChatRequest, prefs *gateway.ProviderPrefs, variant string) bool {
	if variant == "free" && (c.ep.PromptPrice > 0 || c.ep.CompletionPrice > 0) {
		return false
	}
	if prefs == nil {
		return true
	}
	if len(prefs.Only) > 0 && !slices.Contains(prefs.Only, c.ep.Provider) {
		return false
	}
	if slices.Contains(prefs.Ignore, c.ep.Provider) {
		return false
	}
	if len(prefs.Quantizations) > 0 && c.def.Quantization != "" &&
		!slices.Contains(prefs.Quantizations, c.def.Quantization) {
		return false
	}
	if prefs.MaxPrice != nil {
		// MaxPrice is dollars per million tokens; prices are microcents/token.
		// $X/M tokens = X microcents/token exactly (1e8 microcents per dollar / 1e6 tokens = 1e2... ).
		if prefs.MaxPrice.Prompt != nil && float64(c.ep.PromptPrice) > *prefs.MaxPrice.Prompt*100 {
			return false
		}
		if prefs.MaxPrice.Completion != nil && float64(c.ep.CompletionPrice) > *prefs.MaxPrice.Completion*100 {
			return false
		}
	}
	if prefs.RequireParameters != nil && *prefs.RequireParameters {
		supported := c.def.SupportedParams
		if len(supported) == 0 {
			supported = model.SupportedParams
		}
		for _, p := range requestedParams(req) {
			if !slices.Contains(supported, p) {
				return false
			}
		}
	}
	return true
}

// requestedParams lists the optional capabilities this request actually uses.
func requestedParams(req *gateway.ChatRequest) []string {
	var out []string
	if len(req.Tools) > 0 {
		out = append(out, "tools")
	}
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_schema" {
		out = append(out, "structured_outputs")
	} else if req.ResponseFormat != nil {
		out = append(out, "response_format")
	}
	if req.Logprobs != nil && *req.Logprobs {
		out = append(out, "logprobs")
	}
	if req.Reasoning != nil {
		out = append(out, "reasoning")
	}
	return out
}

// order sorts candidates: explicit order[] first, then the sort strategy.
func (r *Router) order(cands []candidate, prefs *gateway.ProviderPrefs, variant string) {
	if prefs != nil && len(prefs.Order) > 0 {
		pos := func(p string) int {
			if i := slices.Index(prefs.Order, p); i >= 0 {
				return i
			}
			return len(prefs.Order) + 1
		}
		slices.SortStableFunc(cands, func(a, b candidate) int {
			return pos(a.ep.Provider) - pos(b.ep.Provider)
		})
		return
	}

	sortBy := ""
	if prefs != nil && len(prefs.Sort) > 0 {
		s := strings.Trim(string(prefs.Sort), `"`)
		sortBy = s
	}
	if variant == "floor" {
		sortBy = "price"
	}
	if variant == "nitro" {
		sortBy = "throughput"
	}

	switch sortBy {
	case "price":
		slices.SortStableFunc(cands, func(a, b candidate) int {
			return int(totalPrice(a) - totalPrice(b))
		})
	case "throughput", "latency":
		// Health samples land with the M3 data plane; until then this
		// keeps catalog order, which lists the primary endpoint first.
	default:
		r.priceWeightedShuffle(cands)
	}
}

func totalPrice(c candidate) int64 {
	return c.ep.PromptPrice + c.ep.CompletionPrice
}

// priceWeightedShuffle orders candidates by repeated weighted draws where
// weight is the inverse square of price, so cheap endpoints lead most of the
// time without starving the rest.
func (r *Router) priceWeightedShuffle(cands []candidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	weights := make([]float64, len(cands))
	for i, c := range cands {
		p := float64(totalPrice(c))
		if p < 1 {
			p = 1
		}
		weights[i] = 1 / (p * p)
	}
	for i := 0; i < len(cands)-1; i++ {
		var sum float64
		for j := i; j < len(cands); j++ {
			sum += weights[j]
		}
		pick := i
		x := r.rng.Float64() * sum
		for j := i; j < len(cands); j++ {
			x -= weights[j]
			if x <= 0 {
				pick = j
				break
			}
		}
		cands[i], cands[pick] = cands[pick], cands[i]
		weights[i], weights[pick] = weights[pick], weights[i]
	}
}

// retryable reports whether the next endpoint should be tried.
func retryable(err error) bool {
	var ue *provider.UpstreamError
	if errors.As(err, &ue) {
		switch {
		case ue.Status == 429, ue.Status == 408:
			return true
		case ue.Status >= 500:
			return true
		default:
			return false
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Network-level failures (connection refused, reset) are retryable.
	return true
}
