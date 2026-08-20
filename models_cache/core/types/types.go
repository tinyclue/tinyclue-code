package types

// ── Pipeline context ──────────────────────────────────────────────────────────

// PipelineContext holds shared state pre-fetched by the pipeline
// for use across provider filters.
type PipelineContext struct {
	ModelsDevData  *ModelsDevResponse
	NvidiaModelIDs map[string]string
	AiGatewayData  *AiGatewayResponse
	OpenRouterData *OpenRouterResponse
}

// ── Registry types ────────────────────────────────────────────────────────────

// FilterFunc converts raw API data into models.
type FilterFunc func(ctx *PipelineContext) []Model

// PatchFunc modifies or supplements existing models for a provider.
type PatchFunc func(models []Model) []Model

// InjectFunc returns hardcoded model definitions for non-API providers.
type InjectFunc func() []Model

// Entry registers a provider's three-phase pipeline.
type Entry struct {
	Provider string
	Filter   FilterFunc
	Patch    PatchFunc
	Inject   InjectFunc
}

// ── models.dev API ──────────────────────────────────────────────────────────

// ModelsDevResponse is the top-level response from https://models.dev/api.json.
type ModelsDevResponse struct {
	AmazonBedrock       *ProviderBlock `json:"amazon-bedrock,omitempty"`
	Anthropic           *ProviderBlock `json:"anthropic,omitempty"`
	Google              *ProviderBlock `json:"google,omitempty"`
	OpenAI              *ProviderBlock `json:"openai,omitempty"`
	Groq                *ProviderBlock `json:"groq,omitempty"`
	Cerebras            *ProviderBlock `json:"cerebras,omitempty"`
	CloudflareWorkersAI *ProviderBlock `json:"cloudflare-workers-ai,omitempty"`
	CloudflareAIGateway *ProviderBlock `json:"cloudflare-ai-gateway,omitempty"`
	XAI                 *ProviderBlock `json:"xai,omitempty"`
	ZAICodingPlan       *ProviderBlock `json:"zai-coding-plan,omitempty"`
	Mistral             *ProviderBlock `json:"mistral,omitempty"`
	HuggingFace         *ProviderBlock `json:"huggingface,omitempty"`
	FireworksAI         *ProviderBlock `json:"fireworks-ai,omitempty"`
	Nvidia              *ProviderBlock `json:"nvidia,omitempty"`
	Together            *ProviderBlock `json:"together,omitempty"`
	Togetherai          *ProviderBlock `json:"togetherai,omitempty"`
	TogetherAI          *ProviderBlock `json:"together-ai,omitempty"`
	OpenCode            *ProviderBlock `json:"opencode,omitempty"`
	OpenCodeGo          *ProviderBlock `json:"opencode-go,omitempty"`
	GitHubCopilot       *ProviderBlock `json:"github-copilot,omitempty"`
	MiniMax             *ProviderBlock `json:"minimax,omitempty"`
	MiniMaxCN           *ProviderBlock `json:"minimax-cn,omitempty"`
	KimiForCoding       *ProviderBlock `json:"kimi-for-coding,omitempty"`
	MoonshotAI          *ProviderBlock `json:"moonshotai,omitempty"`
	MoonshotAICN        *ProviderBlock `json:"moonshotai-cn,omitempty"`
	Xiaomi              *ProviderBlock `json:"xiaomi,omitempty"`
}

type ProviderBlock struct {
	Models map[string]ModelsDevModel `json:"models"`
}

type ModelsDevModel struct {
	ID         string           `json:"id,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCall   *bool            `json:"tool_call,omitempty"`
	Reasoning  *bool            `json:"reasoning,omitempty"`
	Limit      *ModelLimit      `json:"limit,omitempty"`
	Cost       *ModelsDevCost   `json:"cost,omitempty"`
	Modalities *ModelModalities `json:"modalities,omitempty"`
	Provider   *ModelProvider   `json:"provider,omitempty"`
	Status     *string          `json:"status,omitempty"`
}

// ModelsDevCost is the raw models.dev cost data with optional/pointer fields.
type ModelsDevCost struct {
	Input      *float64 `json:"input,omitempty"`
	Output     *float64 `json:"output,omitempty"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

type ModelLimit struct {
	Context *int `json:"context,omitempty"`
	Output  *int `json:"output,omitempty"`
}

type ModelModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

type ModelProvider struct {
	NPM *string `json:"npm,omitempty"`
}

// ── OpenRouter API ──────────────────────────────────────────────────────────

type OpenRouterResponse struct {
	Data []OpenRouterModel `json:"data"`
}

type OpenRouterModel struct {
	ID                  string                  `json:"id"`
	Name                string                  `json:"name"`
	Created             int                     `json:"created,omitempty"`
	Description         string                  `json:"description,omitempty"`
	ContextLength       int                     `json:"context_length,omitempty"`
	Architecture        *OpenRouterArchitecture `json:"architecture,omitempty"`
	Pricing             *OpenRouterPricing      `json:"pricing,omitempty"`
	TopProvider         *OpenRouterTopProvider  `json:"top_provider,omitempty"`
	SupportedParameters []string                `json:"supported_parameters,omitempty"`
}

type OpenRouterArchitecture struct {
	Modality     string `json:"modality,omitempty"`
	Tokenizer    string `json:"tokenizer,omitempty"`
	InstructType string `json:"instruct_type,omitempty"`
}

type OpenRouterPricing struct {
	Prompt          string `json:"prompt,omitempty"`
	Completion      string `json:"completion,omitempty"`
	Image           string `json:"image,omitempty"`
	Request         string `json:"request,omitempty"`
	InputCacheRead  string `json:"input_cache_read,omitempty"`
	InputCacheWrite string `json:"input_cache_write,omitempty"`
	WebSearch       string `json:"web_search,omitempty"`
}

type OpenRouterTopProvider struct {
	MaxCompletionTokens int  `json:"max_completion_tokens,omitempty"`
	IsModerated         bool `json:"is_moderated,omitempty"`
}

// ── NVIDIA NIM API ──────────────────────────────────────────────────────────

type NvidiaResponse struct {
	Data []NvidiaModelListItem `json:"data"`
}

type NvidiaModelListItem struct {
	ID string `json:"id"`
}

// ── Vercel AI Gateway API ───────────────────────────────────────────────────

type AiGatewayResponse struct {
	Data []AiGatewayModel `json:"data"`
}

type AiGatewayModel struct {
	ID            string            `json:"id"`
	Name          string            `json:"name,omitempty"`
	ContextWindow int               `json:"context_window,omitempty"`
	MaxTokens     int               `json:"max_tokens,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Pricing       *AiGatewayPricing `json:"pricing,omitempty"`
}

type AiGatewayPricing struct {
	Input           any `json:"input,omitempty"`
	Output          any `json:"output,omitempty"`
	InputCacheRead  any `json:"input_cache_read,omitempty"`
	InputCacheWrite any `json:"input_cache_write,omitempty"`
}
