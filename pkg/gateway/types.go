// Package gateway implements the OpenAI-compatible wire surface of tsuji:
// request parsing, auth, dispatch to a provider adapter, response and SSE
// shaping, and generation accounting.
package gateway

import "encoding/json"

// ChatRequest is the body of POST /api/v1/chat/completions.
// It is the OpenAI shape plus the tsuji routing extensions.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`

	// Legacy completions surface routes through the same struct.
	Prompt string `json:"prompt,omitempty"`

	Stream            bool            `json:"stream,omitempty"`
	MaxTokens         *int            `json:"max_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	TopK              *int            `json:"top_k,omitempty"`
	MinP              *float64        `json:"min_p,omitempty"`
	TopA              *float64        `json:"top_a,omitempty"`
	Seed              *int            `json:"seed,omitempty"`
	FrequencyPenalty  *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty   *float64        `json:"presence_penalty,omitempty"`
	RepetitionPenalty *float64        `json:"repetition_penalty,omitempty"`
	LogitBias         map[string]int  `json:"logit_bias,omitempty"`
	Logprobs          *bool           `json:"logprobs,omitempty"`
	TopLogprobs       *int            `json:"top_logprobs,omitempty"`
	Stop              json.RawMessage `json:"stop,omitempty"`
	N                 *int            `json:"n,omitempty"`
	User              string          `json:"user,omitempty"`

	Tools             []Tool          `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	ResponseFormat    *ResponseFormat `json:"response_format,omitempty"`

	// tsuji extensions, same names OpenRouter uses.
	Models     []string       `json:"models,omitempty"`
	Route      string         `json:"route,omitempty"`
	Provider   *ProviderPrefs `json:"provider,omitempty"`
	Transforms []string       `json:"transforms,omitempty"`
	Reasoning  *Reasoning     `json:"reasoning,omitempty"`
	Usage      *UsageOpts     `json:"usage,omitempty"`
	Fusion     *FusionOpts    `json:"fusion,omitempty"`
}

// Message is one chat turn. Content is a string or an array of parts;
// it is kept raw and interpreted by the adapter.
type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// Tool is an OpenAI function tool definition.
type Tool struct {
	Type     string          `json:"type"`
	Function json.RawMessage `json:"function"`
}

// ToolCall is an assistant tool invocation.
type ToolCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the name/arguments pair inside a tool call.
type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ResponseFormat selects plain text, json_object, or strict json_schema output.
type ResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

// ProviderPrefs is the per-request routing preference block.
type ProviderPrefs struct {
	Order             []string        `json:"order,omitempty"`
	Only              []string        `json:"only,omitempty"`
	Ignore            []string        `json:"ignore,omitempty"`
	AllowFallbacks    *bool           `json:"allow_fallbacks,omitempty"`
	RequireParameters *bool           `json:"require_parameters,omitempty"`
	DataCollection    string          `json:"data_collection,omitempty"`
	Quantizations     []string        `json:"quantizations,omitempty"`
	Sort              json.RawMessage `json:"sort,omitempty"`
	MaxPrice          *MaxPrice       `json:"max_price,omitempty"`
}

// MaxPrice caps acceptable endpoint pricing in dollars per million tokens.
type MaxPrice struct {
	Prompt     *float64 `json:"prompt,omitempty"`
	Completion *float64 `json:"completion,omitempty"`
}

// Reasoning controls thinking-token behavior on reasoning models.
type Reasoning struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens *int   `json:"max_tokens,omitempty"`
	Exclude   bool   `json:"exclude,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

// UsageOpts asks for cost detail in the usage block.
type UsageOpts struct {
	Include bool `json:"include,omitempty"`
}

// FusionOpts overrides the fusion meta-model composition per request.
// An explicit panel wins over the preset; the preset defaults to the one
// implied by the model id (tsuji/fusion, -budget, -fast).
type FusionOpts struct {
	Preset string   `json:"preset,omitempty"`
	Panel  []string `json:"panel,omitempty"`
	Judge  string   `json:"judge,omitempty"`
	Writer string   `json:"writer,omitempty"`
}

// FusionDetail reports what the fusion pipeline did: every panel answer,
// the judge's comparison notes, and per-phase cost so clients can show a
// breakdown. It rides on the response as a tsuji extension field.
type FusionDetail struct {
	Preset string        `json:"preset"`
	Panel  []FusionPanel `json:"panel"`
	Judge  FusionPhase   `json:"judge"`
	Writer FusionPhase   `json:"writer"`
}

// FusionPanel is one panel member's answer (or its failure).
type FusionPanel struct {
	Model   string  `json:"model"`
	Content string  `json:"content,omitempty"`
	Error   string  `json:"error,omitempty"`
	Cost    float64 `json:"cost"`
}

// FusionPhase is the judge or writer leg of a fusion run.
type FusionPhase struct {
	Model string  `json:"model"`
	Notes string  `json:"notes,omitempty"`
	Cost  float64 `json:"cost"`
}

// ChatResponse is the blocking chat.completion object.
type ChatResponse struct {
	ID       string        `json:"id"`
	Object   string        `json:"object"`
	Created  int64         `json:"created"`
	Model    string        `json:"model"`
	Provider string        `json:"provider,omitempty"`
	Choices  []Choice      `json:"choices"`
	Usage    *Usage        `json:"usage,omitempty"`
	Fusion   *FusionDetail `json:"fusion,omitempty"`
}

// Choice is one completion choice.
type Choice struct {
	Index              int          `json:"index"`
	Message            *RespMessage `json:"message,omitempty"`
	Delta              *RespMessage `json:"delta,omitempty"`
	FinishReason       *string      `json:"finish_reason"`
	NativeFinishReason *string      `json:"native_finish_reason,omitempty"`
}

// RespMessage is the assistant message (or stream delta).
type RespMessage struct {
	Role      string     `json:"role,omitempty"`
	Content   *string    `json:"content,omitempty"`
	Reasoning *string    `json:"reasoning,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Usage is the token and cost accounting block.
type Usage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	Cost                    *float64                 `json:"cost,omitempty"`
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// PromptTokensDetails breaks out cached prompt tokens.
type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// CompletionTokensDetails breaks out reasoning tokens.
type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}
