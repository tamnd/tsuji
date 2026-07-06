package catalog

// ProviderInfo is the public metadata for one upstream provider,
// served by GET /api/v1/providers.
type ProviderInfo struct {
	Name              string  `json:"name"`
	Slug              string  `json:"slug"`
	PrivacyPolicyURL  *string `json:"privacy_policy_url"`
	TermsOfServiceURL *string `json:"terms_of_service_url"`
	StatusPageURL     *string `json:"status_page_url"`
}

// Providers lists every provider slug the router knows how to reach.
var Providers = []ProviderInfo{
	{Name: "OpenAI", Slug: "openai", PrivacyPolicyURL: new("https://openai.com/policies/privacy-policy"), TermsOfServiceURL: new("https://openai.com/policies/terms-of-use"), StatusPageURL: new("https://status.openai.com")},
	{Name: "Anthropic", Slug: "anthropic", PrivacyPolicyURL: new("https://www.anthropic.com/legal/privacy"), TermsOfServiceURL: new("https://www.anthropic.com/legal/consumer-terms"), StatusPageURL: new("https://status.anthropic.com")},
	{Name: "Google AI Studio", Slug: "google", PrivacyPolicyURL: new("https://policies.google.com/privacy"), TermsOfServiceURL: new("https://policies.google.com/terms")},
	{Name: "DeepSeek", Slug: "deepseek", PrivacyPolicyURL: new("https://platform.deepseek.com/privacy"), TermsOfServiceURL: new("https://platform.deepseek.com/terms")},
	{Name: "Groq", Slug: "groq", PrivacyPolicyURL: new("https://groq.com/privacy-policy"), StatusPageURL: new("https://groqstatus.com")},
	{Name: "Mistral", Slug: "mistral", PrivacyPolicyURL: new("https://mistral.ai/terms#privacy-policy"), TermsOfServiceURL: new("https://mistral.ai/terms")},
	{Name: "Together", Slug: "together", PrivacyPolicyURL: new("https://www.together.ai/privacy"), TermsOfServiceURL: new("https://www.together.ai/terms-of-service"), StatusPageURL: new("https://status.together.ai")},
	{Name: "Fireworks", Slug: "fireworks", PrivacyPolicyURL: new("https://fireworks.ai/privacy-policy"), TermsOfServiceURL: new("https://fireworks.ai/terms-of-service")},
	{Name: "DeepInfra", Slug: "deepinfra", PrivacyPolicyURL: new("https://deepinfra.com/privacy"), TermsOfServiceURL: new("https://deepinfra.com/terms")},
	{Name: "xAI", Slug: "xai", PrivacyPolicyURL: new("https://x.ai/legal/privacy-policy"), TermsOfServiceURL: new("https://x.ai/legal/terms-of-service")},
	{Name: "Moonshot AI", Slug: "moonshot", PrivacyPolicyURL: new("https://platform.moonshot.ai/docs/agreement/privacy-policy")},
	{Name: "Alibaba Model Studio", Slug: "alibaba", PrivacyPolicyURL: new("https://www.alibabacloud.com/help/en/legal/latest/alibaba-cloud-international-website-privacy-policy")},
	{Name: "Z.ai", Slug: "zai", PrivacyPolicyURL: new("https://z.ai/model-api/terms-of-service")},
	{Name: "MiniMax", Slug: "minimax", PrivacyPolicyURL: new("https://www.minimax.io/platform/protocol/privacy-policy")},
}
