package catalog

import (
	"slices"
	"testing"
)

func TestSeedLoads(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	models := c.List()
	if len(models) < 35 {
		t.Fatalf("seed has %d models, want at least 35", len(models))
	}

	knownDialects := []string{"openai", "anthropic"}
	providerSlugs := map[string]bool{}
	for _, p := range Providers {
		providerSlugs[p.Slug] = true
	}

	for _, m := range models {
		if m.Description == "" {
			t.Errorf("%s: empty description", m.ID)
		}
		if m.ContextLength == 0 {
			t.Errorf("%s: zero context length", m.ID)
		}
		if len(m.Endpoints) == 0 {
			t.Errorf("%s: no endpoints", m.ID)
		}
		if m.PromptPriceMicrocents() == 0 && m.Pricing.Prompt != "0" {
			t.Errorf("%s: prompt price %q converts to 0", m.ID, m.Pricing.Prompt)
		}
		for _, e := range m.Endpoints {
			if !slices.Contains(knownDialects, e.Dialect) {
				t.Errorf("%s: unknown dialect %q", m.ID, e.Dialect)
			}
			if !providerSlugs[e.Provider] {
				t.Errorf("%s: endpoint provider %q not in the provider registry", m.ID, e.Provider)
			}
		}
	}

	// Anthropic models must use the native dialect.
	for _, m := range models {
		if len(m.ID) > 10 && m.ID[:10] == "anthropic/" {
			for _, e := range m.Endpoints {
				if e.Provider == "anthropic" && e.Dialect != "anthropic" {
					t.Errorf("%s: anthropic endpoint uses dialect %q", m.ID, e.Dialect)
				}
			}
		}
	}
}

func TestSplitVariant(t *testing.T) {
	cases := []struct{ in, base, variant string }{
		{"openai/gpt-4o-mini", "openai/gpt-4o-mini", ""},
		{"deepseek/deepseek-r1-0528:free", "deepseek/deepseek-r1-0528", "free"},
		{"openai/gpt-5.5:nitro", "openai/gpt-5.5", "nitro"},
		{"z-ai/glm-5:floor", "z-ai/glm-5", "floor"},
		{"anthropic/claude-sonnet-5:thinking", "anthropic/claude-sonnet-5", "thinking"},
		{"x-ai/grok-4.20:online", "x-ai/grok-4.20", "online"},
		{"some/model:v2", "some/model:v2", ""},
	}
	for _, tc := range cases {
		base, variant := SplitVariant(tc.in)
		if base != tc.base || variant != tc.variant {
			t.Errorf("SplitVariant(%q) = %q, %q", tc.in, base, variant)
		}
	}
}

func TestEndpointPriceOverride(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	m := c.Get("meta-llama/llama-3.3-70b-instruct")
	if m == nil {
		t.Fatal("model missing from seed")
	}
	var together *EndpointDef
	for i := range m.Endpoints {
		if m.Endpoints[i].Provider == "together" {
			together = &m.Endpoints[i]
		}
	}
	if together == nil {
		t.Fatal("together endpoint missing")
	}
	if together.PromptPriceMicrocents(m) == m.PromptPriceMicrocents() {
		t.Error("together price override not applied")
	}
}
