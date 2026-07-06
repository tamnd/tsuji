package store

import "time"

// Generation is the accounting row every gateway request writes.
type Generation struct {
	ID               string
	KeyID            int64
	ModelRequested   string
	ModelServed      string
	Provider         string
	AppReferer       string
	AppTitle         string
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
	CostMicrocents   int64
	LatencyMS        int64
	Streamed         bool
	FinishReason     string
	Error            string
	CreatedAt        time.Time
}

// InsertGeneration writes one accounting row.
func (s *Store) InsertGeneration(g *Generation) error {
	_, err := s.db.Exec(`
INSERT INTO generations
	(id, key_id, model_requested, model_served, provider, app_referer, app_title,
	 prompt_tokens, completion_tokens, reasoning_tokens, cached_tokens,
	 cost_microcents, latency_ms, streamed, finish_reason, error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.KeyID, g.ModelRequested, g.ModelServed, g.Provider, g.AppReferer, g.AppTitle,
		g.PromptTokens, g.CompletionTokens, g.ReasoningTokens, g.CachedTokens,
		g.CostMicrocents, g.LatencyMS, boolInt(g.Streamed), g.FinishReason, g.Error,
		g.CreatedAt.UTC().Unix(),
	)
	return err
}

// GenerationByID fetches one accounting row.
func (s *Store) GenerationByID(id string) (*Generation, error) {
	var g Generation
	var created int64
	var streamed int
	err := s.db.QueryRow(`
SELECT id, key_id, model_requested, model_served, provider, app_referer, app_title,
	prompt_tokens, completion_tokens, reasoning_tokens, cached_tokens,
	cost_microcents, latency_ms, streamed, finish_reason, error, created_at
FROM generations WHERE id = ?`, id).Scan(
		&g.ID, &g.KeyID, &g.ModelRequested, &g.ModelServed, &g.Provider, &g.AppReferer, &g.AppTitle,
		&g.PromptTokens, &g.CompletionTokens, &g.ReasoningTokens, &g.CachedTokens,
		&g.CostMicrocents, &g.LatencyMS, &streamed, &g.FinishReason, &g.Error, &created,
	)
	if err != nil {
		return nil, err
	}
	g.Streamed = streamed != 0
	g.CreatedAt = time.Unix(created, 0).UTC()
	return &g, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
