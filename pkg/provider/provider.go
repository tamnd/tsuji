// Package provider holds the upstream adapters.
// An Adapter turns a gateway request into one upstream call and normalizes
// the answer back into the OpenAI shape the gateway serves.
package provider

import (
	"context"
	"strconv"

	"github.com/tamnd/tsuji/pkg/gateway"
)

// Endpoint is one concrete upstream a request can be sent to.
type Endpoint struct {
	// Provider is the slug shown to users (openai, anthropic, groq, ...).
	Provider string
	// Model is the upstream's own model identifier.
	Model string
	// BaseURL is the upstream API root.
	BaseURL string
	// APIKey authenticates against the upstream.
	APIKey string
	// PromptPrice and CompletionPrice are microcents per token.
	PromptPrice     int64
	CompletionPrice int64
}

// StreamChunk is one SSE frame from the upstream, already normalized.
type StreamChunk struct {
	Response *gateway.ChatResponse
	Done     bool
}

// Adapter speaks one upstream dialect.
type Adapter interface {
	// Name is the dialect name (openai, anthropic, google).
	Name() string
	// Complete performs a blocking completion.
	Complete(ctx context.Context, ep Endpoint, req *gateway.ChatRequest) (*gateway.ChatResponse, error)
	// Stream performs a streaming completion, sending chunks to the callback
	// in order. The callback must not be called after an error return.
	Stream(ctx context.Context, ep Endpoint, req *gateway.ChatRequest, fn func(StreamChunk) error) error
}

// UpstreamError carries the upstream HTTP status so the gateway can map it.
type UpstreamError struct {
	Provider string
	Status   int
	Body     string
}

func (e *UpstreamError) Error() string {
	return "upstream " + e.Provider + " returned " + strconv.Itoa(e.Status)
}

// UpstreamStatus lets the gateway map the upstream HTTP status without
// importing this package's concrete type.
func (e *UpstreamError) UpstreamStatus() int { return e.Status }
