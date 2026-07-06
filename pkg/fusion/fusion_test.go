package fusion_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/fusion"
	"github.com/tamnd/tsuji/pkg/gateway"
	"github.com/tamnd/tsuji/pkg/store"
)

// script is one fake model's behavior.
type script struct {
	content          string
	promptTokens     int
	completionTokens int
	promptPrice      int64 // microcents per token
	completionPrice  int64
	fail             error
}

// fakeDialer serves scripted answers and records every leg's messages.
type fakeDialer struct {
	mu      sync.Mutex
	scripts map[string]script
	calls   []recordedCall
}

type recordedCall struct {
	model    string
	messages []gateway.Message
	stream   bool
}

func (f *fakeDialer) Dial(_ context.Context, model *catalog.Model, req *gateway.ChatRequest) (*gateway.Upstream, error) {
	sc, ok := f.scripts[model.ID]
	if !ok {
		return nil, fmt.Errorf("no script for %s", model.ID)
	}
	record := func(stream bool) {
		f.mu.Lock()
		f.calls = append(f.calls, recordedCall{model: model.ID, messages: req.Messages, stream: stream})
		f.mu.Unlock()
	}
	resp := func() *gateway.ChatResponse {
		content := sc.content
		stop := "stop"
		return &gateway.ChatResponse{
			Object:  "chat.completion",
			Choices: []gateway.Choice{{Index: 0, Message: &gateway.RespMessage{Role: "assistant", Content: &content}, FinishReason: &stop}},
			Usage: &gateway.Usage{
				PromptTokens:     sc.promptTokens,
				CompletionTokens: sc.completionTokens,
				TotalTokens:      sc.promptTokens + sc.completionTokens,
			},
		}
	}
	return &gateway.Upstream{
		Provider:        "fake",
		PromptPrice:     sc.promptPrice,
		CompletionPrice: sc.completionPrice,
		Complete: func(context.Context, *gateway.ChatRequest) (*gateway.ChatResponse, error) {
			record(false)
			if sc.fail != nil {
				return nil, sc.fail
			}
			return resp(), nil
		},
		Stream: func(_ context.Context, _ *gateway.ChatRequest, fn func(*gateway.ChatResponse, bool) error) error {
			record(true)
			if sc.fail != nil {
				return sc.fail
			}
			// One delta per word, then a finish frame, then a usage frame.
			for word := range strings.FieldsSeq(sc.content) {
				w := word + " "
				if err := fn(&gateway.ChatResponse{
					Object:  "chat.completion.chunk",
					Choices: []gateway.Choice{{Index: 0, Delta: &gateway.RespMessage{Content: &w}, FinishReason: nil}},
				}, false); err != nil {
					return err
				}
			}
			stop := "stop"
			if err := fn(&gateway.ChatResponse{
				Object:  "chat.completion.chunk",
				Choices: []gateway.Choice{{Index: 0, Delta: &gateway.RespMessage{}, FinishReason: &stop}},
			}, false); err != nil {
				return err
			}
			full := resp()
			return fn(&gateway.ChatResponse{Object: "chat.completion.chunk", Choices: []gateway.Choice{}, Usage: full.Usage}, false)
		},
	}, nil
}

// testEngine builds an engine over three fake panel models plus judge and
// writer, using real catalog ids so plan validation passes.
func testEngine(t *testing.T) (*fusion.Engine, *fakeDialer, *store.Store) {
	t.Helper()
	dialer := &fakeDialer{scripts: map[string]script{
		// 10 prompt + 20 completion at 1/2 microcents = 10*1 + 20*2 = 50 each.
		"google/gemini-3.5-flash":    {content: "Paris is the capital.", promptTokens: 10, completionTokens: 20, promptPrice: 1, completionPrice: 2},
		"openai/gpt-5.4-mini":        {content: "The capital is Paris.", promptTokens: 10, completionTokens: 20, promptPrice: 1, completionPrice: 2},
		"anthropic/claude-haiku-4.5": {content: "Paris.", promptTokens: 10, completionTokens: 20, promptPrice: 1, completionPrice: 2},
		// Judge: 30 prompt + 10 completion at 2/4 = 30*2 + 10*4 = 100.
		"openai/gpt-5.4": {content: "All three answers agree: Paris. No contradictions.", promptTokens: 30, completionTokens: 10, promptPrice: 2, completionPrice: 4},
		// Writer: 40 prompt + 15 completion at 2/4 = 40*2 + 15*4 = 140.
		"anthropic/claude-sonnet-5": {content: "The capital of France is Paris.", promptTokens: 40, completionTokens: 15, promptPrice: 2, completionPrice: 4},
	}}

	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	eng := &fusion.Engine{
		Catalog: cat,
		Store:   st,
		Next:    dialer,
		Presets: map[string]fusion.Preset{
			"general-high": {
				Panel:  []string{"google/gemini-3.5-flash", "openai/gpt-5.4-mini", "anthropic/claude-haiku-4.5"},
				Judge:  "openai/gpt-5.4",
				Writer: "anthropic/claude-sonnet-5",
			},
		},
	}
	return eng, dialer, st
}

func dialFusion(t *testing.T, eng *fusion.Engine, req *gateway.ChatRequest) *gateway.Upstream {
	t.Helper()
	m := eng.Catalog.Get("tsuji/fusion")
	if m == nil {
		t.Fatal("tsuji/fusion missing from catalog seed")
	}
	up, err := eng.Dial(context.Background(), m, req)
	if err != nil {
		t.Fatal(err)
	}
	return up
}

func userReq(prompt string) *gateway.ChatRequest {
	return &gateway.ChatRequest{
		Model:    "tsuji/fusion",
		Messages: []gateway.Message{{Role: "user", Content: fmt.Appendf(nil, "%q", prompt)}},
	}
}

func TestFusionCompleteFlow(t *testing.T) {
	eng, dialer, st := testEngine(t)
	req := userReq("What is the capital of France?")
	up := dialFusion(t, eng, req)

	ctx := gateway.WithParent(context.Background(), gateway.Parent{GenID: "gen-parent", KeyID: mintKey(t, st)})
	resp, err := up.Complete(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	// The writer's answer is the response content.
	if got := *resp.Choices[0].Message.Content; got != "The capital of France is Paris." {
		t.Errorf("content = %q", got)
	}

	// Cost is the sum of every leg: 3*50 + 100 + 140 = 390 microcents.
	if got := up.CostOverride(); got != 390 {
		t.Errorf("total cost = %d microcents, want 390", got)
	}

	// Usage sums tokens across all five legs.
	if resp.Usage.PromptTokens != 3*10+30+40 || resp.Usage.CompletionTokens != 3*20+10+15 {
		t.Errorf("usage = %d/%d", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}

	// The fusion detail carries the panel answers and judge notes.
	d := resp.Fusion
	if d == nil {
		t.Fatal("fusion detail missing")
	}
	if d.Preset != "general-high" || len(d.Panel) != 3 {
		t.Fatalf("detail = preset %q, %d panel entries", d.Preset, len(d.Panel))
	}
	if !strings.Contains(d.Judge.Notes, "agree") {
		t.Errorf("judge notes = %q", d.Judge.Notes)
	}
	if d.Judge.Cost != 100.0/1e8 || d.Writer.Cost != 140.0/1e8 {
		t.Errorf("phase costs = %v / %v", d.Judge.Cost, d.Writer.Cost)
	}

	// The judge saw the panel answers; the writer saw the judge notes.
	var judgeSaw, writerSaw string
	for _, c := range dialer.calls {
		switch c.model {
		case "openai/gpt-5.4":
			judgeSaw = flatten(c.messages)
		case "anthropic/claude-sonnet-5":
			writerSaw = flatten(c.messages)
		}
	}
	if !strings.Contains(judgeSaw, "Paris is the capital.") {
		t.Error("judge prompt is missing a panel answer")
	}
	if !strings.Contains(writerSaw, "No contradictions") {
		t.Error("writer prompt is missing the judge notes")
	}
	if !strings.Contains(writerSaw, "What is the capital of France?") {
		t.Error("writer prompt is missing the original conversation")
	}

	// Five child rows link back to the parent generation.
	children, err := st.ChildGenerations("gen-parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 5 {
		t.Fatalf("child rows = %d, want 5", len(children))
	}
	var childSum int64
	for _, c := range children {
		childSum += c.CostMicrocents
	}
	if childSum != 390 {
		t.Errorf("child cost sum = %d, want 390", childSum)
	}
}

func TestFusionPanelFailureTolerated(t *testing.T) {
	eng, dialer, _ := testEngine(t)
	sc := dialer.scripts["openai/gpt-5.4-mini"]
	sc.fail = errors.New("upstream 500")
	dialer.scripts["openai/gpt-5.4-mini"] = sc

	req := userReq("hello")
	up := dialFusion(t, eng, req)
	resp, err := up.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var failed int
	for _, p := range resp.Fusion.Panel {
		if p.Error != "" {
			failed++
			if p.Model != "openai/gpt-5.4-mini" {
				t.Errorf("wrong panel member failed: %s", p.Model)
			}
		}
	}
	if failed != 1 {
		t.Errorf("failed panel entries = %d, want 1", failed)
	}
	// 2*50 + 100 + 140: the dead leg costs nothing.
	if got := up.CostOverride(); got != 340 {
		t.Errorf("total cost = %d, want 340", got)
	}
}

func TestFusionTooFewSurvivors(t *testing.T) {
	eng, dialer, _ := testEngine(t)
	for _, id := range []string{"openai/gpt-5.4-mini", "google/gemini-3.5-flash"} {
		sc := dialer.scripts[id]
		sc.fail = errors.New("upstream 500")
		dialer.scripts[id] = sc
	}
	req := userReq("hello")
	up := dialFusion(t, eng, req)
	if _, err := up.Complete(context.Background(), req); err == nil {
		t.Fatal("want error with only one survivor")
	}
}

func TestFusionJudgeFailureFailsRequest(t *testing.T) {
	eng, dialer, _ := testEngine(t)
	sc := dialer.scripts["openai/gpt-5.4"]
	sc.fail = errors.New("judge down")
	dialer.scripts["openai/gpt-5.4"] = sc

	req := userReq("hello")
	up := dialFusion(t, eng, req)
	if _, err := up.Complete(context.Background(), req); err == nil || !strings.Contains(err.Error(), "judge") {
		t.Fatalf("err = %v, want judge failure", err)
	}
}

func TestFusionStream(t *testing.T) {
	eng, _, _ := testEngine(t)
	req := userReq("What is the capital of France?")
	req.Stream = true
	up := dialFusion(t, eng, req)

	var reasoning, content string
	var final *gateway.ChatResponse
	err := up.Stream(context.Background(), req, func(chunk *gateway.ChatResponse, _ bool) error {
		if chunk.Fusion != nil {
			final = chunk
			return nil
		}
		for _, c := range chunk.Choices {
			if c.Delta == nil {
				continue
			}
			if c.Delta.Reasoning != nil {
				reasoning += *c.Delta.Reasoning
			}
			if c.Delta.Content != nil {
				content += *c.Delta.Content
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reasoning, "agree") {
		t.Errorf("judge notes not streamed as reasoning: %q", reasoning)
	}
	if strings.TrimSpace(content) != "The capital of France is Paris." {
		t.Errorf("streamed content = %q", content)
	}
	if final == nil {
		t.Fatal("no final frame with fusion detail")
	}
	if final.Usage == nil || final.Usage.PromptTokens != 3*10+30+40 {
		t.Errorf("final usage = %+v", final.Usage)
	}
	if got := up.CostOverride(); got != 390 {
		t.Errorf("total cost = %d, want 390", got)
	}
}

func TestFusionRequestOverrides(t *testing.T) {
	eng, dialer, _ := testEngine(t)
	req := userReq("hello")
	req.Fusion = &gateway.FusionOpts{
		Panel:  []string{"google/gemini-3.5-flash", "anthropic/claude-haiku-4.5"},
		Judge:  "openai/gpt-5.4",
		Writer: "anthropic/claude-sonnet-5",
	}
	up := dialFusion(t, eng, req)
	resp, err := up.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Fusion.Panel) != 2 {
		t.Errorf("panel size = %d, want 2", len(resp.Fusion.Panel))
	}
	for _, c := range dialer.calls {
		if c.model == "openai/gpt-5.4-mini" {
			t.Error("excluded model was still called")
		}
	}
}

func TestFusionUnknownModelRejected(t *testing.T) {
	eng, _, _ := testEngine(t)
	req := userReq("hello")
	req.Fusion = &gateway.FusionOpts{Panel: []string{"nope/nothing", "openai/gpt-5.4-mini"}}
	m := eng.Catalog.Get("tsuji/fusion")
	if _, err := eng.Dial(context.Background(), m, req); err == nil {
		t.Fatal("want error for unknown panel model")
	}
}

func TestFusionPassthrough(t *testing.T) {
	eng, dialer, _ := testEngine(t)
	m := eng.Catalog.Get("openai/gpt-5.4-mini")
	req := &gateway.ChatRequest{Model: "openai/gpt-5.4-mini"}
	up, err := eng.Dial(context.Background(), m, req)
	if err != nil {
		t.Fatal(err)
	}
	if up.Provider != "fake" {
		t.Errorf("passthrough went to %q, not the next dialer", up.Provider)
	}
	_ = dialer
}

func mintKey(t *testing.T, st *store.Store) int64 {
	t.Helper()
	_, k, err := st.CreateKey("test")
	if err != nil {
		t.Fatal(err)
	}
	return k.ID
}

func flatten(msgs []gateway.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.Write(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}
