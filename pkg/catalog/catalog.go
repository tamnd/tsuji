// Package catalog holds the model directory: which models exist, what they
// cost, and which upstream endpoint serves each of them.
// The seed ships in models.json; a live instance can resync from any
// OpenRouter-shaped /api/v1/models source later.
package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
)

//go:embed models.json
var seed []byte

// Model is one catalog entry, wire-shaped after OpenRouter's /api/v1/models.
type Model struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Created          int64         `json:"created"`
	Description      string        `json:"description"`
	ContextLength    int           `json:"context_length"`
	Architecture     Architecture  `json:"architecture"`
	Pricing          Pricing       `json:"pricing"`
	TopProvider      TopProvider   `json:"top_provider"`
	PerRequestLimits any           `json:"per_request_limits"`
	SupportedParams  []string      `json:"supported_parameters"`
	Endpoints        []EndpointDef `json:"-"`
}

// Architecture describes modality and tokenizer family.
type Architecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tokenizer        string   `json:"tokenizer"`
	InstructType     *string  `json:"instruct_type"`
}

// Pricing is dollars per token as decimal strings, OpenRouter style.
type Pricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
	Request    string `json:"request"`
	Image      string `json:"image"`
}

// TopProvider summarizes the primary endpoint.
type TopProvider struct {
	ContextLength       int  `json:"context_length"`
	MaxCompletionTokens *int `json:"max_completion_tokens"`
	IsModerated         bool `json:"is_moderated"`
}

// EndpointDef maps a model to an upstream: which provider slug serves it,
// under which upstream model name, in which API dialect.
type EndpointDef struct {
	Provider string `json:"provider"`
	Dialect  string `json:"dialect"`
	Model    string `json:"model"`
}

// seedModel is the on-disk JSON shape (Model plus endpoints).
type seedModel struct {
	Model
	Endpoints []EndpointDef `json:"endpoints"`
}

// Catalog is an in-memory view over the seed, safe for concurrent reads.
type Catalog struct {
	mu     sync.RWMutex
	models map[string]*Model
	order  []string
}

// Load parses the embedded seed.
func Load() (*Catalog, error) {
	var rows []seedModel
	if err := json.Unmarshal(seed, &rows); err != nil {
		return nil, fmt.Errorf("catalog seed: %w", err)
	}
	c := &Catalog{models: make(map[string]*Model, len(rows))}
	for i := range rows {
		m := rows[i].Model
		m.Endpoints = rows[i].Endpoints
		if _, dup := c.models[m.ID]; dup {
			return nil, fmt.Errorf("catalog seed: duplicate model %s", m.ID)
		}
		c.models[m.ID] = &m
		c.order = append(c.order, m.ID)
	}
	sort.Strings(c.order)
	return c, nil
}

// Get returns a model by id (author/slug), nil when unknown.
func (c *Catalog) Get(id string) *Model {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.models[id]
}

// List returns all models in id order.
func (c *Catalog) List() []*Model {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Model, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.models[id])
	}
	return out
}

// PromptPriceMicrocents converts the decimal dollar price to microcents/token.
func (m *Model) PromptPriceMicrocents() int64 { return toMicrocents(m.Pricing.Prompt) }

// CompletionPriceMicrocents converts the decimal dollar price to microcents/token.
func (m *Model) CompletionPriceMicrocents() int64 { return toMicrocents(m.Pricing.Completion) }

func toMicrocents(dollars string) int64 {
	f, err := strconv.ParseFloat(dollars, 64)
	if err != nil {
		return 0
	}
	// 1 dollar = 100 cents = 1e8 microcents.
	return int64(f*1e8 + 0.5)
}
