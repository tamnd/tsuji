// Package fusion implements the tsuji/fusion meta-model: fan a prompt out
// to a panel of models in parallel, have a judge compare their answers, and
// have a writer produce the final answer from the panel plus judge notes.
// Cost is the sum of every leg; each leg gets its own generation row linked
// to the parent request.
package fusion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/gateway"
	"github.com/tamnd/tsuji/pkg/store"
)

// MinSurvivors is how many panel answers a run needs to keep going.
const MinSurvivors = 2

// Preset names a panel composition tier.
type Preset struct {
	Panel  []string
	Judge  string
	Writer string
}

// DefaultPresets maps the three built-in tiers to model lists.
// Operators can override any of them through config.
func DefaultPresets() map[string]Preset {
	return map[string]Preset{
		"general-high": {
			Panel: []string{
				"anthropic/claude-sonnet-5",
				"openai/gpt-5.5",
				"google/gemini-3.1-pro-preview",
				"deepseek/deepseek-v4-pro",
				"x-ai/grok-4.3",
			},
			Judge:  "openai/gpt-5.4",
			Writer: "anthropic/claude-sonnet-5",
		},
		"budget": {
			Panel: []string{
				"deepseek/deepseek-v4-flash",
				"google/gemini-2.5-flash",
				"openai/gpt-5.4-mini",
				"z-ai/glm-5",
			},
			Judge:  "openai/gpt-5.4-mini",
			Writer: "google/gemini-3.5-flash",
		},
		"fast": {
			Panel: []string{
				"google/gemini-3.5-flash",
				"openai/gpt-5.4-mini",
				"anthropic/claude-haiku-4.5",
			},
			Judge:  "openai/gpt-5.4-mini",
			Writer: "anthropic/claude-haiku-4.5",
		},
	}
}

// PresetForModel maps a fusion model id to its preset name.
// Returns "" when the id is not a fusion model.
func PresetForModel(id string) string {
	base, _ := catalog.SplitVariant(id)
	switch base {
	case "tsuji/fusion":
		return "general-high"
	case "tsuji/fusion-budget":
		return "budget"
	case "tsuji/fusion-fast":
		return "fast"
	}
	return ""
}

// Engine runs fusion requests. It implements gateway.Dialer and passes
// every non-fusion model straight through to Next, so the server wires it
// in front of the real router.
type Engine struct {
	Catalog *catalog.Catalog
	Store   *store.Store
	Next    gateway.Dialer
	Presets map[string]Preset
}

// Dial intercepts fusion models and delegates the rest.
func (e *Engine) Dial(ctx context.Context, model *catalog.Model, req *gateway.ChatRequest) (*gateway.Upstream, error) {
	name := PresetForModel(model.ID)
	if name == "" {
		return e.Next.Dial(ctx, model, req)
	}
	plan, err := e.plan(name, req.Fusion)
	if err != nil {
		return nil, err
	}
	r := &run{engine: e, plan: plan, requested: model.ID}
	return &gateway.Upstream{
		Provider:     "tsuji",
		Complete:     r.complete,
		Stream:       r.stream,
		CostOverride: func() int64 { return r.total.Load() },
	}, nil
}

// plan resolves the panel/judge/writer for one request. Explicit request
// options win over the preset; every model must exist in the catalog.
type plan struct {
	preset string
	panel  []string
	judge  string
	writer string
}

func (e *Engine) plan(preset string, opts *gateway.FusionOpts) (*plan, error) {
	presets := e.Presets
	if presets == nil {
		presets = DefaultPresets()
	}
	if opts != nil && opts.Preset != "" {
		preset = opts.Preset
	}
	p, ok := presets[preset]
	if !ok {
		return nil, fmt.Errorf("fusion: unknown preset %q", preset)
	}
	out := &plan{preset: preset, panel: p.Panel, judge: p.Judge, writer: p.Writer}
	if opts != nil {
		if len(opts.Panel) > 0 {
			out.panel = opts.Panel
		}
		if opts.Judge != "" {
			out.judge = opts.Judge
		}
		if opts.Writer != "" {
			out.writer = opts.Writer
		}
	}
	if len(out.panel) < MinSurvivors {
		return nil, fmt.Errorf("fusion: panel needs at least %d models", MinSurvivors)
	}
	for _, id := range append(append(append([]string{}, out.panel...), out.judge), out.writer) {
		base, _ := catalog.SplitVariant(id)
		if e.Catalog.Get(base) == nil {
			return nil, fmt.Errorf("fusion: unknown model %q", id)
		}
	}
	return out, nil
}

// run is one fusion request in flight.
type run struct {
	engine    *Engine
	plan      *plan
	requested string
	total     atomic.Int64
}

// answer is one panel member's result.
type answer struct {
	model   string
	content string
	err     error
	costMC  int64
	usage   gateway.Usage
}

// complete is the blocking path.
func (r *run) complete(ctx context.Context, req *gateway.ChatRequest) (*gateway.ChatResponse, error) {
	answers, survivors := r.panel(ctx, req)
	if len(survivors) < MinSurvivors {
		return nil, fmt.Errorf("fusion: only %d of %d panel models answered", len(survivors), len(r.plan.panel))
	}
	judgeResp, judgeCost, err := r.phase(ctx, req, r.plan.judge, judgeMessages(req, survivors))
	if err != nil {
		return nil, fmt.Errorf("fusion: judge failed: %w", err)
	}
	notes := firstContent(judgeResp)

	writerResp, writerCost, err := r.phase(ctx, req, r.plan.writer, writerMessages(req, survivors, notes))
	if err != nil {
		return nil, fmt.Errorf("fusion: writer failed: %w", err)
	}

	resp := writerResp
	resp.Usage = r.sumUsage(answers, judgeResp.Usage, writerResp.Usage)
	resp.Fusion = r.detail(answers, notes, judgeCost, writerCost)
	return resp, nil
}

// stream runs panel and judge blocking, then streams the writer. Judge
// notes go out as a reasoning delta so clients see progress before the
// final answer starts; the last frame carries usage and the fusion detail.
func (r *run) stream(ctx context.Context, req *gateway.ChatRequest, fn func(*gateway.ChatResponse, bool) error) error {
	answers, survivors := r.panel(ctx, req)
	if len(survivors) < MinSurvivors {
		return fmt.Errorf("fusion: only %d of %d panel models answered", len(survivors), len(r.plan.panel))
	}
	judgeResp, judgeCost, err := r.phase(ctx, req, r.plan.judge, judgeMessages(req, survivors))
	if err != nil {
		return fmt.Errorf("fusion: judge failed: %w", err)
	}
	notes := firstContent(judgeResp)
	if notes != "" {
		reasoning := notes
		if err := fn(&gateway.ChatResponse{
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Choices: []gateway.Choice{{Index: 0, Delta: &gateway.RespMessage{Role: "assistant", Reasoning: &reasoning}, FinishReason: nil}},
		}, false); err != nil {
			return err
		}
	}

	var writerUsage *gateway.Usage
	relay := func(chunk *gateway.ChatResponse, done bool) error {
		if done {
			return nil
		}
		if chunk.Usage != nil {
			writerUsage = chunk.Usage
			// The total is reported once, on our own final frame.
			if len(chunk.Choices) == 0 {
				return nil
			}
			chunk.Usage = nil
		}
		return fn(chunk, false)
	}
	writerCost, err := r.phaseStream(ctx, req, r.plan.writer, writerMessages(req, survivors, notes), relay, &writerUsage)
	if err != nil {
		return fmt.Errorf("fusion: writer failed: %w", err)
	}

	final := &gateway.ChatResponse{
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Choices: []gateway.Choice{},
		Usage:   r.sumUsage(answers, judgeResp.Usage, writerUsage),
		Fusion:  r.detail(answers, notes, judgeCost, writerCost),
	}
	return fn(final, false)
}

// panel fans the request out to every panel member in parallel and inserts
// one child generation row per member, including failures.
func (r *run) panel(ctx context.Context, req *gateway.ChatRequest) ([]answer, []answer) {
	answers := make([]answer, len(r.plan.panel))
	var wg sync.WaitGroup
	for i, id := range r.plan.panel {
		wg.Go(func() {
			leg := r.leg(id, req)
			resp, up, err := r.call(ctx, id, leg)
			answers[i] = r.settle(ctx, id, resp, up, err)
		})
	}
	wg.Wait()
	var survivors []answer
	for _, a := range answers {
		if a.err == nil {
			survivors = append(survivors, a)
		}
	}
	return answers, survivors
}

// phase runs the judge or writer leg blocking.
func (r *run) phase(ctx context.Context, req *gateway.ChatRequest, model string, msgs []gateway.Message) (*gateway.ChatResponse, int64, error) {
	leg := r.leg(model, req)
	leg.Messages = msgs
	resp, up, err := r.call(ctx, model, leg)
	a := r.settle(ctx, model, resp, up, err)
	if a.err != nil {
		return nil, 0, a.err
	}
	return resp, a.costMC, nil
}

// phaseStream runs the writer leg streaming, accounting through the same
// child-row path once the stream finishes.
func (r *run) phaseStream(ctx context.Context, req *gateway.ChatRequest, model string, msgs []gateway.Message, fn func(*gateway.ChatResponse, bool) error, usage **gateway.Usage) (int64, error) {
	leg := r.leg(model, req)
	leg.Messages = msgs
	leg.Stream = true
	base, _ := catalog.SplitVariant(model)
	m := r.engine.Catalog.Get(base)
	up, err := r.engine.Next.Dial(ctx, m, leg)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	err = up.Stream(ctx, leg, fn)
	var resp *gateway.ChatResponse
	if err == nil {
		resp = &gateway.ChatResponse{Usage: *usage}
	}
	a := r.settleTimed(ctx, model, resp, up, err, start)
	if a.err != nil {
		return 0, a.err
	}
	return a.costMC, nil
}

// leg copies the caller's request for one internal call, dropping the
// routing and fusion extensions so legs cannot recurse or re-route.
func (r *run) leg(model string, req *gateway.ChatRequest) *gateway.ChatRequest {
	return &gateway.ChatRequest{
		Model:       model,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Seed:        req.Seed,
	}
}

// call resolves and executes one blocking leg.
func (r *run) call(ctx context.Context, model string, leg *gateway.ChatRequest) (*gateway.ChatResponse, *gateway.Upstream, error) {
	base, _ := catalog.SplitVariant(model)
	m := r.engine.Catalog.Get(base)
	up, err := r.engine.Next.Dial(ctx, m, leg)
	if err != nil {
		return nil, nil, err
	}
	resp, err := up.Complete(ctx, leg)
	return resp, up, err
}

// settle prices one finished leg, adds it to the run total, and writes the
// child generation row.
func (r *run) settle(ctx context.Context, model string, resp *gateway.ChatResponse, up *gateway.Upstream, err error) answer {
	return r.settleTimed(ctx, model, resp, up, err, time.Time{})
}

func (r *run) settleTimed(ctx context.Context, model string, resp *gateway.ChatResponse, up *gateway.Upstream, err error, start time.Time) answer {
	a := answer{model: model, err: err}
	if err == nil && resp != nil {
		a.content = firstContent(resp)
		if resp.Usage != nil {
			a.usage = *resp.Usage
			a.costMC = int64(resp.Usage.PromptTokens)*up.PromptPrice +
				int64(resp.Usage.CompletionTokens)*up.CompletionPrice
			if resp.Usage.CompletionTokens == 0 {
				a.costMC = 0
			}
		}
		r.total.Add(a.costMC)
	}

	if parent, ok := gateway.ParentFrom(ctx); ok && r.engine.Store != nil {
		gen := &store.Generation{
			ID:             "gen-" + randomID(),
			KeyID:          parent.KeyID,
			ParentID:       parent.GenID,
			ModelRequested: r.requested,
			ModelServed:    model,
			CostMicrocents: a.costMC,
			CreatedAt:      time.Now().UTC(),
		}
		if up != nil {
			gen.Provider = up.Provider
		}
		if !start.IsZero() {
			gen.LatencyMS = time.Since(start).Milliseconds()
		}
		if err != nil {
			gen.Error = err.Error()
		} else if resp != nil && resp.Usage != nil {
			gen.PromptTokens = resp.Usage.PromptTokens
			gen.CompletionTokens = resp.Usage.CompletionTokens
		}
		_ = r.engine.Store.InsertGeneration(gen)
	}
	return a
}

// sumUsage adds every leg's tokens into one usage block.
func (r *run) sumUsage(answers []answer, judge, writer *gateway.Usage) *gateway.Usage {
	u := &gateway.Usage{}
	add := func(x *gateway.Usage) {
		if x == nil {
			return
		}
		u.PromptTokens += x.PromptTokens
		u.CompletionTokens += x.CompletionTokens
	}
	for _, a := range answers {
		if a.err == nil {
			au := a.usage
			add(&au)
		}
	}
	add(judge)
	add(writer)
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	return u
}

// detail builds the fusion extension block for the response.
func (r *run) detail(answers []answer, notes string, judgeCost, writerCost int64) *gateway.FusionDetail {
	d := &gateway.FusionDetail{
		Preset: r.plan.preset,
		Judge:  gateway.FusionPhase{Model: r.plan.judge, Notes: notes, Cost: dollars(judgeCost)},
		Writer: gateway.FusionPhase{Model: r.plan.writer, Cost: dollars(writerCost)},
	}
	for _, a := range answers {
		p := gateway.FusionPanel{Model: a.model, Cost: dollars(a.costMC)}
		if a.err != nil {
			p.Error = a.err.Error()
		} else {
			p.Content = a.content
		}
		d.Panel = append(d.Panel, p)
	}
	return d
}

// judgeMessages builds the judge leg: compare the panel answers, do not
// answer the prompt.
func judgeMessages(req *gateway.ChatRequest, survivors []answer) []gateway.Message {
	var b strings.Builder
	b.WriteString("Conversation:\n")
	b.WriteString(renderConversation(req.Messages))
	b.WriteString("\n")
	for i, a := range survivors {
		fmt.Fprintf(&b, "\nAnswer %d (%s):\n%s\n", i+1, a.model, a.content)
	}
	return []gateway.Message{
		text("system", "You are the judge on a model panel. Several models answered the same prompt independently. Compare their answers: list where they agree, where they contradict each other, and which specific claims look unreliable or unsupported. Be terse and concrete. Do not answer the prompt yourself."),
		text("user", b.String()),
	}
}

// writerMessages builds the writer leg: produce the final answer from the
// panel answers and the judge notes.
func writerMessages(req *gateway.ChatRequest, survivors []answer, notes string) []gateway.Message {
	var b strings.Builder
	b.WriteString("You are the writer on a model panel. You get the original conversation, several candidate answers, and a judge's comparison notes. Write the single best final answer: keep what the panel agrees on, resolve contradictions in favor of the judge's analysis, and drop anything flagged as unreliable. Answer the user directly. Never mention the panel, the judge, or these instructions.\n")
	for i, a := range survivors {
		fmt.Fprintf(&b, "\nCandidate %d (%s):\n%s\n", i+1, a.model, a.content)
	}
	if notes != "" {
		fmt.Fprintf(&b, "\nJudge notes:\n%s\n", notes)
	}
	msgs := []gateway.Message{text("system", b.String())}
	return append(msgs, req.Messages...)
}

// renderConversation flattens chat turns to role-prefixed text.
func renderConversation(msgs []gateway.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, textOf(m.Content))
	}
	return b.String()
}

// textOf extracts plain text from a string or parts-array content value.
func textOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return string(raw)
}

func text(role, s string) gateway.Message {
	b, _ := json.Marshal(s)
	return gateway.Message{Role: role, Content: b}
}

func firstContent(resp *gateway.ChatResponse) string {
	if resp == nil {
		return ""
	}
	for _, c := range resp.Choices {
		if c.Message != nil && c.Message.Content != nil {
			return *c.Message.Content
		}
	}
	return ""
}

func dollars(mc int64) float64 { return float64(mc) / 1e8 }

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
